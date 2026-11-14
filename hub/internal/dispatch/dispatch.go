// Package dispatch connects event sources (Tuya data-point changes or manual
// triggers) to logical actions on devices, without binding a rule to a specific
// transport. A rule names a target device and the change to apply; the hub
// decides how to reach that device (LAN HTTP) when it executes.
package dispatch

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"reflect"
	"strings"
	"sync"
	"time"

	"smart_home/hub/internal/db"
	"smart_home/hub/internal/eventbus"
	"smart_home/hub/internal/model"
	"smart_home/hub/internal/tuya/service"
)

// Source types.
const (
	SourceTuyaDP     = "tuya_dp"
	SourceMideaState = "midea_state"
	SourceThingState = "thing_state"
	SourceManual     = "manual"
	// SourceTime is a volatile, one-shot trigger: the rule fires at a single
	// instant (a wall-clock time today or a relative delay) instead of reacting
	// to a device event. It is used for "desliga às 14" or "desliga daqui a
	// 10 minutos".
	SourceTime = "time"
)

// Action kinds.
const (
	ActionFan   = "fan"
	ActionTuya  = "tuya"
	ActionMidea = "midea"
)

// Schedule limits when an automatic trigger may fire. Empty Start/End means
// "always". Days uses time.Weekday values (0 = Sunday); empty means every day.
// A window whose start is after its end crosses midnight, and Days refers to
// the day the window starts.
type Schedule struct {
	Start string `json:"start,omitempty"`
	End   string `json:"end,omitempty"`
	Days  []int  `json:"days,omitempty"`
}

// Delay pauses before each action of an automation. It is opt-in per rule; the
// hub does not impose a global interval. It is mainly used between fan RF
// frames, but it applies to the ordered action list as a whole.
type Delay struct {
	Enabled bool `json:"enabled"`
	Ms      int  `json:"ms"`
}

// Active reports whether an instant falls inside the schedule. The hub's local
// timezone is used because the operator writes the window in wall-clock time.
func (w Schedule) Active(t time.Time) bool {
	if w.Start == "" || w.End == "" {
		return true
	}
	start, okStart := parseClock(w.Start)
	end, okEnd := parseClock(w.End)
	if !okStart || !okEnd {
		// Invalid schedules are rejected on save. If an old row somehow has
		// one, do not make the automation silently stop working.
		return true
	}
	t = t.In(time.Local)
	at := t.Hour()*60 + t.Minute()
	day := int(t.Weekday())
	if start == end {
		return dayAllowed(w.Days, day)
	}
	if start < end {
		return dayAllowed(w.Days, day) && at >= start && at < end
	}
	// The window crosses midnight. An early-morning instant belongs to the
	// previous day's occurrence.
	if at >= start {
		return dayAllowed(w.Days, day)
	}
	if at < end {
		return dayAllowed(w.Days, (day+6)%7)
	}
	return false
}

// parseClock converts "HH:MM" into minutes since midnight.
func parseClock(v string) (int, bool) {
	t, err := time.Parse("15:04", strings.TrimSpace(v))
	if err != nil {
		return 0, false
	}
	return t.Hour()*60 + t.Minute(), true
}

func dayAllowed(days []int, day int) bool {
	if len(days) == 0 {
		return true
	}
	for _, d := range days {
		if d == day {
			return true
		}
	}
	return false
}

// ValidateSchedule checks a schedule coming from the API.
func ValidateSchedule(w Schedule) error {
	if w.Start == "" && w.End == "" {
		if len(w.Days) > 0 {
			return errors.New("schedule days require a start and end time")
		}
		return nil
	}
	if w.Start == "" || w.End == "" {
		return errors.New("schedule needs both start and end")
	}
	if _, ok := parseClock(w.Start); !ok {
		return fmt.Errorf("invalid schedule start %q (use HH:MM)", w.Start)
	}
	if _, ok := parseClock(w.End); !ok {
		return fmt.Errorf("invalid schedule end %q (use HH:MM)", w.End)
	}
	for _, d := range w.Days {
		if d < 0 || d > 6 {
			return fmt.Errorf("invalid schedule day %d (use 0..6)", d)
		}
	}
	return nil
}

// Source describes what triggers an automation. Both device sources name a
// key the same way: a Tuya data point (DP) and a Midea logical state key
// ("power", "mode", "target_temp", ...) arrive in DP.
//
// A SourceTime trigger ignores Device/DP and uses the instant fields instead.
type Source struct {
	Type   string   `json:"type"`
	Device string   `json:"device,omitempty"`
	DP     string   `json:"dp,omitempty"`
	DPs    []string `json:"dps,omitempty"`
	Value  any      `json:"value,omitempty"`

	// Time trigger (Type == SourceTime). Exactly one of At or InSecs is
	// required. The hub resolves either into FireAt, an absolute RFC3339
	// instant in the hub's local timezone, which is what makes the pending
	// instant survive a restart.
	At     string `json:"at,omitempty"`         // "HH:MM" wall-clock, local
	Date   string `json:"date,omitempty"`       // "YYYY-MM-DD", optional
	InSecs int    `json:"in_seconds,omitempty"` // relative delay, resolved at save
	FireAt string `json:"fire_at,omitempty"`    // RFC3339, resolved by the hub
	// Days turns a clock trigger into a weekly one: it fires at At on each of
	// these weekdays (0=Sunday..6=Saturday) and re-arms itself. Empty means a
	// one-shot trigger.
	Days []int `json:"days,omitempty"`
}

// Action is a logical operation on a device. For a fan it sets a state on a
// sub-device of a gateway (Thing + DeviceID); for a Tuya or Midea device it
// writes a data point or a state (Device + DP/State). Which one applies depends
// on Kind, so both addressings live side by side.
type Action struct {
	Kind     string         `json:"kind"`
	Thing    string         `json:"thing,omitempty"`
	DeviceID string         `json:"device_id,omitempty"`
	State    map[string]any `json:"state,omitempty"`
	Device   string         `json:"device,omitempty"`
	DP       string         `json:"dp,omitempty"`
	Value    any            `json:"value,omitempty"`
}

// Automation is one stored rule.
type Automation struct {
	ID       string   `json:"id"`
	Name     string   `json:"name"`
	Enabled  bool     `json:"enabled"`
	Once     bool     `json:"once,omitempty"`
	Source   Source   `json:"source"`
	Schedule Schedule `json:"schedule,omitempty"`
	Delay    Delay    `json:"delay,omitempty"`
	Actions  []Action `json:"actions"`
}

// FanSetter sets an absolute state on a fan. It is implemented by the hub.
type FanSetter interface {
	SetState(ctx context.Context, thing, deviceID string, state map[string]any, force bool) (map[string]any, error)
}

// TuyaController writes one data point on a Tuya device. It is implemented by
// the Tuya service.
type TuyaController interface {
	Control(ctx context.Context, id, dp string, value any) (map[string]any, error)
}

// MideaController applies a partial logical state on a Midea appliance, reports
// the last persisted state and pushes state changes to the dispatcher. It is
// implemented by the Midea service.
type MideaController interface {
	Apply(ctx context.Context, id string, state map[string]any) (map[string]any, error)
	StateSourceSetter(source any)
	RefreshStates(ctx context.Context)
	States(ctx context.Context, id string) (map[string]any, error)
}

// MideaStateSource receives the logical state of an appliance when it changes.
type MideaStateSource interface {
	HandleMideaState(deviceID string, state map[string]any)
}

// EventSource streams Tuya events and lets the dispatcher ensure its source
// devices are being listened to. It is implemented by the Tuya service.
type EventSource interface {
	SetEventHandler(fn func(deviceID string, e service.EventRecord))
	StartListen(ctx context.Context, id string) error
}

// ThingStateSource exposes the current snapshot of gateways and their
// sub-devices. It is implemented by the hub and is used to seed the "last
// value" map so a restart does not replay fan states into automations.
type ThingStateSource interface {
	Snapshot() []*model.Thing
}

// Dispatcher evaluates automations and fires their actions.
type Dispatcher struct {
	store *db.Store
	svc   EventSource
	fans  FanSetter
	tuya  TuyaController
	midea MideaController

	commandTimeout time.Duration
	// grace is how long after its instant a scheduled (SourceTime) automation
	// may still run. A rule overdue by more than this is skipped.
	grace time.Duration

	mu          sync.Mutex
	rules       []Automation
	last        map[string]any // deviceID + "\x00" + dp -> last seen value
	now         func() time.Time
	bus         *eventbus.Bus
	busCancel   func()
	thingStates ThingStateSource
}

// New builds a dispatcher.
func New(store *db.Store, svc EventSource, fans FanSetter, tuya TuyaController) *Dispatcher {
	return &Dispatcher{
		store:          store,
		svc:            svc,
		fans:           fans,
		tuya:           tuya,
		commandTimeout: 15 * time.Second,
		grace:          2 * time.Minute,
		last:           map[string]any{},
		now:            time.Now,
	}
}

// SetGrace sets the tolerance after a scheduled automation's instant, so a hub
// that boots slightly late still runs it. A rule overdue by more is skipped.
func (d *Dispatcher) SetGrace(grace time.Duration) {
	if grace > 0 {
		d.grace = grace
	}
}

// SetMidea attaches the Midea controller so automations can set an appliance
// state (power, mode, temperature, ...) and react to its changes.
func (d *Dispatcher) SetMidea(m MideaController) {
	d.midea = m
	if m != nil {
		m.StateSourceSetter(d)
	}
	// A rule may already be loaded when the controller is attached, so start
	// the source refresh for it now (Reload is idempotent).
	d.mu.Lock()
	loaded := d.rules
	d.mu.Unlock()
	if len(loaded) > 0 {
		d.Reload(context.Background())
	}
}

// SetEventBus attaches the hub event bus. The dispatcher subscribes to "state"
// events so fan/gateway sub-device changes can trigger automations too.
func (d *Dispatcher) SetEventBus(bus *eventbus.Bus) { d.bus = bus }

// SetThingStates attaches the hub snapshot used to seed fan states at startup.
func (d *Dispatcher) SetThingStates(source ThingStateSource) { d.thingStates = source }

// Start installs the event handler, loads the rules and listens to every source
// device. It also subscribes to gateway state events for thing_state triggers.
func (d *Dispatcher) Start(ctx context.Context) {
	if d.svc != nil {
		d.svc.SetEventHandler(d.onEvent)
	}
	d.seedThingStates()
	if d.bus != nil {
		ch, cancel := d.bus.Subscribe()
		d.mu.Lock()
		d.busCancel = cancel
		d.mu.Unlock()
		go d.consumeThingStates(ctx, ch)
	}
	d.Reload(ctx)
}

// Close detaches the event handler and the bus subscription.
func (d *Dispatcher) Close() {
	if d.svc != nil {
		d.svc.SetEventHandler(nil)
	}
	d.mu.Lock()
	cancel := d.busCancel
	d.busCancel = nil
	d.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

// StartClock runs the scheduled (SourceTime) automations. It checks every
// second and also once immediately, so a rule that came due while the hub was
// down (but is still inside the grace window) fires on boot. It returns
// immediately; the loop stops when ctx is done.
func (d *Dispatcher) StartClock(ctx context.Context) {
	go func() {
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				d.runDue(ctx)
			}
		}
	}()
	d.runDue(ctx)
}

// runDue fires the scheduled automations whose instant has passed. A rule whose
// instant is older than the grace window is skipped. Either way a one-shot rule
// is then finished: a volatile (Once) rule is deleted, any other is disabled.
func (d *Dispatcher) runDue(ctx context.Context) {
	now := d.now()
	d.mu.Lock()
	rules := append([]Automation(nil), d.rules...)
	d.mu.Unlock()

	for _, r := range rules {
		if !r.Enabled || r.Source.Type != SourceTime {
			continue
		}
		fire, ok := parseInstant(r.Source.FireAt)
		if !ok {
			// Without a resolvable instant the rule can never fire; finish it
			// so it does not linger forever.
			log.Printf("[dispatch] %q (%s) has no valid fire_at, finishing", r.Name, r.ID)
			d.finishTime(r)
			continue
		}
		if fire.After(now) {
			continue
		}
		if now.Sub(fire) > d.grace {
			log.Printf("[dispatch] %q (%s) missed by %s, skipping", r.Name, r.ID, now.Sub(fire).Truncate(time.Second))
			d.finishTime(r)
			continue
		}
		d.fire(r, "time "+fire.In(time.Local).Format("15:04"))
		d.finishTime(r)
	}
}

// finishTime settles a scheduled rule after it came due (or was skipped): a
// weekly rule is re-armed for its next occurrence and stays loaded, while a
// one-shot rule is removed—deleted when volatile, disabled otherwise.
func (d *Dispatcher) finishTime(r Automation) {
	if len(r.Source.Days) > 0 && r.Enabled {
		d.rearm(r)
		return
	}
	d.finishOneShot(r)
}

// rearm rewrites a weekly rule to its next occurrence, both in the loaded set
// and in storage, so the same rule fires again on the chosen weekdays.
func (d *Dispatcher) rearm(r Automation) {
	next := r
	next.Source.FireAt = ""
	if err := resolveInstant(&next.Source, d.now()); err != nil {
		log.Printf("[dispatch] rearm %s: %v", r.ID, err)
		d.finishOneShot(r)
		return
	}

	d.mu.Lock()
	for i := range d.rules {
		if d.rules[i].ID == r.ID {
			d.rules[i] = next
			break
		}
	}
	d.mu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := Save(ctx, d.store, next); err != nil {
		log.Printf("[dispatch] rearm save %s: %v", r.ID, err)
	}
	if d.bus != nil {
		d.bus.PublishData("automations", map[string]any{"id": r.ID})
	}
}

// finishOneShot removes a fired (or skipped) scheduled rule from the loaded set
// and from storage: a volatile rule is deleted, any other is disabled. It then
// tells the UI so the list refreshes without waiting for the poll.
func (d *Dispatcher) finishOneShot(r Automation) {
	d.mu.Lock()
	for i := range d.rules {
		if d.rules[i].ID == r.ID {
			d.rules = append(d.rules[:i], d.rules[i+1:]...)
			break
		}
	}
	d.mu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if r.Once {
		if err := d.store.DeleteAutomation(ctx, r.ID); err != nil {
			log.Printf("[dispatch] remove volatile %s: %v", r.ID, err)
		}
	} else {
		r.Enabled = false
		if err := Save(ctx, d.store, r); err != nil {
			log.Printf("[dispatch] disable scheduled automation %s: %v", r.ID, err)
		}
	}
	if d.bus != nil {
		d.bus.PublishData("automations", map[string]any{"id": r.ID})
	}
}

// seedThingStates copies the last reported state of every gateway sub-device
// into the dedup map, so the first state event after a restart does not fire a
// rule for a value that was already there.
func (d *Dispatcher) seedThingStates() {
	if d.thingStates == nil {
		return
	}
	for _, thing := range d.thingStates.Snapshot() {
		if thing == nil {
			continue
		}
		for deviceID, state := range thing.States {
			if state == nil {
				continue
			}
			key := "thing:" + thing.ID + "/" + deviceID
			for name, value := range state.Reported {
				d.remember(key, name, normalize(value))
			}
		}
	}
}

func (d *Dispatcher) consumeThingStates(ctx context.Context, ch <-chan eventbus.Event) {
	for {
		select {
		case <-ctx.Done():
			return
		case ev, ok := <-ch:
			if !ok {
				return
			}
			if ev.Type != "state" {
				continue
			}
			thing, _ := ev.Data["thing"].(string)
			deviceID, _ := ev.Data["id"].(string)
			reported, _ := ev.Data["reported"].(map[string]any)
			d.HandleThingState(thing, deviceID, reported, ev.At)
		}
	}
}

// HandleThingState evaluates gateway sub-device state changes (fans, lights and
// other devices behind an ESP32). Only changed keys fire.
func (d *Dispatcher) HandleThingState(thing, deviceID string, reported map[string]any, at time.Time) {
	if thing == "" || deviceID == "" || len(reported) == 0 {
		return
	}
	if at.IsZero() {
		at = d.now()
	}
	key := "thing:" + thing + "/" + deviceID
	device := thing + "/" + deviceID
	for name, value := range reported {
		val := normalize(value)
		if !d.changed(key, name, val) {
			continue
		}
		d.match(device, name, val, at)
	}
}

// Reload re-reads the rules and makes sure every source device is being
// listened to.
func (d *Dispatcher) Reload(ctx context.Context) {
	rules, err := Load(ctx, d.store)
	if err != nil {
		log.Printf("[dispatch] load automations: %v", err)
		return
	}
	d.mu.Lock()
	d.rules = rules
	d.mu.Unlock()

	devices := map[string]bool{}
	mideaSources := map[string]bool{}
	for _, r := range rules {
		if !r.Enabled {
			continue
		}
		switch r.Source.Type {
		case SourceTuyaDP:
			if r.Source.Device == "" || devices[r.Source.Device] {
				continue
			}
			devices[r.Source.Device] = true
			if d.svc != nil {
				if err := d.svc.StartListen(ctx, r.Source.Device); err != nil {
					log.Printf("[dispatch] auto-listen %s failed: %v", r.Source.Device, err)
				}
			}
		case SourceMideaState:
			if r.Source.Device != "" {
				mideaSources[r.Source.Device] = true
			}
		}
	}
	if len(mideaSources) > 0 && d.midea != nil {
		// Seed the "last value" map from the stored state so a restart does not
		// replay a change, then read the appliances once so the first trigger
		// does not wait for the polling interval.
		for id := range mideaSources {
			state, err := d.midea.States(ctx, id)
			if err != nil {
				continue
			}
			for key, value := range state {
				d.remember(id, key, normalize(value))
			}
		}
		go d.midea.RefreshStates(ctx)
	}
	log.Printf("[dispatch] %d automation(s) over %d tuya and %d midea source device(s)", len(rules), len(devices), len(mideaSources))
}

// HandleMideaState evaluates the Midea triggers when an appliance state
// changes. Only the changed keys are passed in.
func (d *Dispatcher) HandleMideaState(deviceID string, state map[string]any) {
	at := d.now()
	for key, value := range state {
		val := normalize(value)
		if !d.changed(deviceID, key, val) {
			continue
		}
		d.match(deviceID, key, val, at)
	}
}

func (d *Dispatcher) onEvent(deviceID string, e service.EventRecord) {
	at := e.At
	if at.IsZero() {
		at = d.now()
	}
	switch e.Kind {
	case "status":
		for dp, v := range e.DPs {
			d.remember(deviceID, dp, normalize(v))
		}
		return
	case "report":
		// A report is an event (an unlock happened, a battery dropped), not a
		// state snapshot: fire it even when the value repeats. Remember it so a
		// later status push does not re-trigger the same rule.
		for dp, v := range e.DPs {
			val := normalize(v)
			d.remember(deviceID, dp, val)
			d.match(deviceID, dp, val, at)
		}
		return
	case "dp":
	default:
		return
	}
	for dp, v := range e.DPs {
		val := normalize(v)
		if !d.changed(deviceID, dp, val) {
			continue
		}
		d.match(deviceID, dp, val, at)
	}
}

func (d *Dispatcher) match(deviceID, dp string, val any, at time.Time) {
	d.mu.Lock()
	rules := append([]Automation(nil), d.rules...)
	d.mu.Unlock()

	for _, r := range rules {
		if !r.Enabled ||
			(r.Source.Type != SourceTuyaDP && r.Source.Type != SourceMideaState && r.Source.Type != SourceThingState) {
			continue
		}
		if r.Source.Device != "" && r.Source.Device != deviceID {
			continue
		}
		if !r.Source.matchesDP(dp) {
			continue
		}
		if r.Source.Value != nil && !equal(normalize(r.Source.Value), val) {
			continue
		}
		if !r.Schedule.Active(at) {
			continue
		}
		d.fire(r, fmt.Sprintf("dp %s=%v on %s", dp, val, deviceID))
	}
}

// matchesDP reports whether the trigger listens to dp. Both DP (legacy single
// data point) and DPs (a group, for example every unlock method) are accepted.
func (s Source) matchesDP(dp string) bool {
	if s.DP != "" && s.DP == dp {
		return true
	}
	for _, candidate := range s.DPs {
		if candidate == dp {
			return true
		}
	}
	return false
}

// Run fires a manual automation by id.
func (d *Dispatcher) Run(ctx context.Context, id string) (int, error) {
	rules, err := Load(ctx, d.store)
	if err != nil {
		return 0, err
	}
	for _, r := range rules {
		if r.ID != id {
			continue
		}
		if !r.Enabled {
			return 0, fmt.Errorf("dispatch: automation %s is disabled", id)
		}
		d.fire(r, "manual "+id)
		return len(r.Actions), nil
	}
	return 0, db.ErrNotFound
}

func (d *Dispatcher) fire(r Automation, reason string) {
	if r.Delay.Enabled && r.Delay.Ms > 0 {
		go func() {
			for _, a := range r.Actions {
				time.Sleep(time.Duration(r.Delay.Ms) * time.Millisecond)
				d.runOne(r, a, reason)
			}
		}()
		return
	}
	for _, a := range r.Actions {
		a := a
		go d.runOne(r, a, reason)
	}
}

func (d *Dispatcher) runOne(r Automation, a Action, reason string) {
	ctx, cancel := context.WithTimeout(context.Background(), d.commandTimeout)
	defer cancel()
	if err := d.runAction(ctx, a, r.ID); err != nil {
		log.Printf("[dispatch] %q (%s) action %s failed: %v", r.Name, reason, a.Kind, err)
		return
	}
	log.Printf("[dispatch] %q (%s) action %s ok", r.Name, reason, a.Kind)
}

func (d *Dispatcher) runAction(ctx context.Context, a Action, automationID string) error {
	switch a.Kind {
	case ActionFan:
		if a.Thing == "" || a.DeviceID == "" {
			return fmt.Errorf("dispatch: fan action needs a thing and a device id (automation %s)", automationID)
		}
		// Hub.SetState serializa todos os comandos de ventilador e aplica o
		// intervalo entre frames RF.
		state := fanState(a.State)
		_, err := d.fans.SetState(ctx, a.Thing, a.DeviceID, state, false)
		return err
	case ActionTuya:
		if a.Device == "" || a.DP == "" {
			return fmt.Errorf("dispatch: tuya action needs device and dp (automation %s)", automationID)
		}
		_, err := d.tuya.Control(ctx, a.Device, a.DP, a.Value)
		return err
	case ActionMidea:
		if a.Device == "" {
			return fmt.Errorf("dispatch: midea action needs a device (automation %s)", automationID)
		}
		if d.midea == nil {
			return fmt.Errorf("dispatch: midea control is not available (automation %s)", automationID)
		}
		_, err := d.midea.Apply(ctx, a.Device, a.State)
		return err
	default:
		return fmt.Errorf("dispatch: unknown action kind %q", a.Kind)
	}
}

func (d *Dispatcher) changed(deviceID, dp string, val any) bool {
	key := deviceID + "\x00" + dp
	d.mu.Lock()
	defer d.mu.Unlock()
	prev, seen := d.last[key]
	d.last[key] = val
	return !seen || !equal(prev, val)
}

func (d *Dispatcher) remember(deviceID, dp string, val any) {
	key := deviceID + "\x00" + dp
	d.mu.Lock()
	d.last[key] = val
	d.mu.Unlock()
}

// Load reads the automations from the database.
func Load(ctx context.Context, store *db.Store) ([]Automation, error) {
	list, err := store.ListAutomations(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]Automation, 0, len(list))
	for _, raw := range list {
		a := Automation{ID: raw.ID, Name: raw.Name, Enabled: raw.Enabled, Once: raw.Once}
		_ = json.Unmarshal(raw.Source, &a.Source)
		_ = json.Unmarshal(raw.Schedule, &a.Schedule)
		_ = json.Unmarshal(raw.Delay, &a.Delay)
		_ = json.Unmarshal(raw.Actions, &a.Actions)
		out = append(out, a)
	}
	return out, nil
}

// Save writes an automation to the database. A time trigger's relative delay is
// resolved to an absolute FireAt here, so the pending instant is durable.
func Save(ctx context.Context, store *db.Store, a Automation) error {
	if err := resolveInstant(&a.Source, time.Now()); err != nil {
		return err
	}
	source, err := json.Marshal(a.Source)
	if err != nil {
		return err
	}
	schedule, err := json.Marshal(a.Schedule)
	if err != nil {
		return err
	}
	delay, err := json.Marshal(a.Delay)
	if err != nil {
		return err
	}
	actions, err := json.Marshal(a.Actions)
	if err != nil {
		return err
	}
	return store.UpsertAutomation(ctx, db.Automation{
		ID:       a.ID,
		Name:     a.Name,
		Enabled:  a.Enabled,
		Once:     a.Once,
		Source:   source,
		Schedule: schedule,
		Delay:    delay,
		Actions:  actions,
	})
}

// Resolve computes a time trigger's absolute instant on a copy the caller can
// return to the UI. Save does the same, so calling it twice is harmless.
func Resolve(a *Automation, now time.Time) error {
	return resolveInstant(&a.Source, now)
}

// resolveInstant turns a time trigger's At/Date or InSecs into the absolute
// FireAt (RFC3339, in the hub's local timezone) and clears InSecs. An already
// resolved FireAt is kept. Instants are wall-clock in time.Local, like the
// schedule window, because the operator writes them in local time.
func resolveInstant(src *Source, now time.Time) error {
	if src == nil || src.Type != SourceTime {
		return nil
	}
	if src.FireAt != "" {
		if _, err := time.Parse(time.RFC3339, src.FireAt); err == nil {
			src.InSecs = 0
			return nil
		}
	}
	if src.InSecs > 0 {
		src.FireAt = now.Add(time.Duration(src.InSecs) * time.Second).Format(time.RFC3339)
		src.InSecs = 0
		return nil
	}
	clock, ok := parseClock(src.At)
	if !ok {
		return fmt.Errorf("invalid time at %q (use HH:MM)", src.At)
	}
	local := now.In(time.Local)
	day := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, time.Local)
	if src.Date != "" {
		d, err := time.ParseInLocation("2006-01-02", src.Date, time.Local)
		if err != nil {
			return fmt.Errorf("invalid time date %q (use YYYY-MM-DD)", src.Date)
		}
		day = d
	}
	if src.Date == "" && len(src.Days) > 0 {
		// Weekly trigger: the next day whose weekday is selected.
		src.FireAt = nextWeekly(local, clock, src.Days).Format(time.RFC3339)
		return nil
	}
	fire := day.Add(time.Duration(clock) * time.Minute)
	if src.Date == "" && !fire.After(local) {
		// No explicit date: the next occurrence is tomorrow.
		fire = fire.AddDate(0, 0, 1)
	}
	src.FireAt = fire.Format(time.RFC3339)
	return nil
}

// nextWeekly returns the first instant at `clock` minutes after midnight, on or
// after `from`, whose weekday is in days (0=Sunday..6=Saturday). It looks at the
// next seven days; an empty or all-invalid list falls back to `from`.
func nextWeekly(from time.Time, clock int, days []int) time.Time {
	set := make(map[time.Weekday]bool, len(days))
	for _, d := range days {
		set[time.Weekday(d)] = true
	}
	local := from.In(time.Local)
	day := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, time.Local)
	for i := 0; i < 7; i++ {
		candidate := day.AddDate(0, 0, i).Add(time.Duration(clock) * time.Minute)
		if candidate.After(local) && set[candidate.Weekday()] {
			return candidate
		}
	}
	// No weekday matched in a full week (should not happen after validation):
	// fall back to a week from now so the rule keeps a sane instant.
	return day.AddDate(0, 0, 7).Add(time.Duration(clock) * time.Minute)
}

// parseInstant parses a resolved FireAt instant.
func parseInstant(v string) (time.Time, bool) {
	if strings.TrimSpace(v) == "" {
		return time.Time{}, false
	}
	t, err := time.Parse(time.RFC3339, v)
	if err != nil {
		return time.Time{}, false
	}
	return t, true
}

// Validate checks the fields the UI and the API must agree on.
func Validate(a Automation) error {
	if strings.TrimSpace(a.Name) == "" {
		return errors.New("name is required")
	}
	switch a.Source.Type {
	case "", SourceManual:
	case SourceTime:
		if strings.TrimSpace(a.Source.At) == "" && a.Source.InSecs <= 0 {
			return errors.New("a scheduled automation needs at (HH:MM) or in_seconds")
		}
		if strings.TrimSpace(a.Source.At) != "" && a.Source.InSecs > 0 {
			return errors.New("a scheduled automation takes at or in_seconds, not both")
		}
		if at := strings.TrimSpace(a.Source.At); at != "" {
			if _, ok := parseClock(at); !ok {
				return fmt.Errorf("invalid at %q (use HH:MM)", at)
			}
		}
		if d := strings.TrimSpace(a.Source.Date); d != "" {
			if _, err := time.ParseInLocation("2006-01-02", d, time.Local); err != nil {
				return fmt.Errorf("invalid date %q (use YYYY-MM-DD)", d)
			}
		}
		seenDays := make(map[int]bool, len(a.Source.Days))
		for _, d := range a.Source.Days {
			if d < 0 || d > 6 {
				return fmt.Errorf("invalid time day %d (use 0..6)", d)
			}
			if seenDays[d] {
				return fmt.Errorf("duplicate time day %d", d)
			}
			seenDays[d] = true
		}
		if len(a.Source.Days) > 0 {
			if strings.TrimSpace(a.Source.At) == "" {
				return errors.New("time days require at (HH:MM)")
			}
			if strings.TrimSpace(a.Source.Date) != "" {
				return errors.New("time days cannot be combined with a date")
			}
			if a.Once {
				return errors.New("a weekly time rule cannot be volatile (once)")
			}
		}
		if a.Schedule.Start != "" || a.Schedule.End != "" {
			return errors.New("a scheduled automation cannot also have a schedule window")
		}
	case SourceTuyaDP, SourceMideaState, SourceThingState:
		if strings.TrimSpace(a.Source.Device) == "" {
			return errors.New("source device is required")
		}
		hasEvent := strings.TrimSpace(a.Source.DP) != ""
		for _, dp := range a.Source.DPs {
			if strings.TrimSpace(dp) != "" {
				hasEvent = true
				break
			}
		}
		if !hasEvent {
			return errors.New("source event is required")
		}
	default:
		return fmt.Errorf("unknown source type %q", a.Source.Type)
	}
	if len(a.Actions) == 0 {
		return errors.New("at least one action is required")
	}
	fans := map[string]bool{}
	for i, action := range a.Actions {
		if action.Kind != ActionFan {
			continue
		}
		if action.Thing == "" || action.DeviceID == "" {
			return fmt.Errorf("action %d: fan action needs a thing and a device id", i+1)
		}
		if len(action.State) != 1 {
			return fmt.Errorf("action %d: a fan accepts one command at a time", i+1)
		}
		target := action.Thing + "/" + action.DeviceID
		if fans[target] {
			return fmt.Errorf("action %d: only one command per fan is allowed", i+1)
		}
		fans[target] = true
	}
	if a.Delay.Enabled {
		if a.Delay.Ms <= 0 {
			return errors.New("delay must be greater than zero when enabled")
		}
		if a.Delay.Ms > 60000 {
			return errors.New("delay must be at most 60000 ms")
		}
	}
	return ValidateSchedule(a.Schedule)
}

// fanState maps a legacy power-on action to speed 1: the RF remote has no
// "power on" button. Everything else is a single command and passes through.
func fanState(state map[string]any) map[string]any {
	if len(state) == 1 {
		if p, ok := state["power"]; ok {
			if on, ok := p.(bool); ok && on {
				return map[string]any{"speed": int64(1)}
			}
		}
	}
	return state
}

// normalize turns JSON numbers into float64 so a rule value and a decoded DP
// value compare consistently.
func normalize(v any) any {
	switch t := v.(type) {
	case json.Number:
		if f, err := t.Float64(); err == nil {
			return f
		}
		return t
	case float32:
		return float64(t)
	case int:
		return float64(t)
	case int64:
		return float64(t)
	default:
		return v
	}
}

func equal(a, b any) bool {
	if af, ok := asFloat(a); ok {
		bf, ok := asFloat(b)
		return ok && af == bf
	}
	return reflect.DeepEqual(a, b)
}

func asFloat(v any) (float64, bool) {
	switch t := v.(type) {
	case float64:
		return t, true
	case int:
		return float64(t), true
	case int64:
		return float64(t), true
	default:
		return 0, false
	}
}
