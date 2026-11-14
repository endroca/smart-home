package dispatch

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"smart_home/hub/internal/db"
	"smart_home/hub/internal/eventbus"
	"smart_home/hub/internal/tuya/service"
)

type fanCall struct {
	thing, deviceID string
	state           map[string]any
}

type fakeFans struct{ calls chan fanCall }

func (f *fakeFans) SetState(_ context.Context, thing, deviceID string, state map[string]any, _ bool) (map[string]any, error) {
	f.calls <- fanCall{thing: thing, deviceID: deviceID, state: state}
	return map[string]any{"mode": "lan"}, nil
}

type tuyaCall struct {
	id, dp string
	value  any
}

type fakeTuya struct{ calls chan tuyaCall }

func (f *fakeTuya) Control(_ context.Context, id, dp string, value any) (map[string]any, error) {
	f.calls <- tuyaCall{id: id, dp: dp, value: value}
	return map[string]any{"mode": "lan"}, nil
}

type fakeSvc struct {
	fn       func(string, service.EventRecord)
	listened []string
}

func (f *fakeSvc) SetEventHandler(fn func(string, service.EventRecord)) { f.fn = fn }
func (f *fakeSvc) StartListen(_ context.Context, id string) error {
	f.listened = append(f.listened, id)
	return nil
}
func (f *fakeSvc) emit(id string, e service.EventRecord) {
	if f.fn != nil {
		f.fn(id, e)
	}
}

func newTestStore(t *testing.T) *db.Store {
	t.Helper()
	s, err := db.Open(filepath.Join(t.TempDir(), "hub.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func TestDispatchTuyaDPToFan(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	if err := Save(ctx, store, Automation{
		ID: "a1", Name: "Luz", Enabled: true,
		Source:  Source{Type: SourceTuyaDP, Device: "sw1", DP: "1", Value: true},
		Actions: []Action{{Kind: ActionFan, Thing: "rf01", DeviceID: "fan0", State: map[string]any{"light": true}}},
	}); err != nil {
		t.Fatal(err)
	}

	fans := &fakeFans{calls: make(chan fanCall, 4)}
	tuya := &fakeTuya{calls: make(chan tuyaCall, 4)}
	svc := &fakeSvc{}
	d := New(store, svc, fans, tuya)
	d.Start(ctx)

	if len(svc.listened) != 1 || svc.listened[0] != "sw1" {
		t.Fatalf("source device not listened: %v", svc.listened)
	}

	// Baseline snapshot: the switch is off, no action.
	svc.emit("sw1", service.EventRecord{Kind: "status", DPs: map[string]any{"1": false}})
	select {
	case c := <-fans.calls:
		t.Fatalf("action fired on baseline: %+v", c)
	case <-time.After(150 * time.Millisecond):
	}

	// The switch turns on: the fan light turns on.
	svc.emit("sw1", service.EventRecord{Kind: "dp", DPs: map[string]any{"1": true}})
	select {
	case c := <-fans.calls:
		if c.thing != "rf01" || c.deviceID != "fan0" || c.state["light"] != true {
			t.Fatalf("unexpected action: %+v", c)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("action did not fire")
	}

	// The same value again is not a change.
	svc.emit("sw1", service.EventRecord{Kind: "dp", DPs: map[string]any{"1": true}})
	select {
	case c := <-fans.calls:
		t.Fatalf("repeated value fired: %+v", c)
	case <-time.After(150 * time.Millisecond):
	}

	// The switch turns off: the value rule does not match.
	svc.emit("sw1", service.EventRecord{Kind: "dp", DPs: map[string]any{"1": false}})
	select {
	case c := <-fans.calls:
		t.Fatalf("non-matching value fired: %+v", c)
	case <-time.After(150 * time.Millisecond):
	}
}

func TestDispatchTuyaToTuyaAndManual(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	if err := Save(ctx, store, Automation{
		ID: "m1", Name: "Cena", Enabled: true,
		Source:  Source{Type: SourceManual},
		Actions: []Action{{Kind: ActionTuya, Device: "lamp1", DP: "1", Value: true}},
	}); err != nil {
		t.Fatal(err)
	}

	fans := &fakeFans{calls: make(chan fanCall, 4)}
	tuya := &fakeTuya{calls: make(chan tuyaCall, 4)}
	d := New(store, &fakeSvc{}, fans, tuya)

	n, err := d.Run(ctx, "m1")
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("actions=%d", n)
	}
	select {
	case c := <-tuya.calls:
		if c.id != "lamp1" || c.dp != "1" || c.value != true {
			t.Fatalf("unexpected tuya action: %+v", c)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("manual action did not fire")
	}
}

type fakeMidea struct{ calls chan mideaCall }

type mideaCall struct {
	id    string
	state map[string]any
}

func (f *fakeMidea) Apply(_ context.Context, id string, state map[string]any) (map[string]any, error) {
	f.calls <- mideaCall{id: id, state: state}
	return map[string]any{"mode": "lan"}, nil
}

func (f *fakeMidea) StateSourceSetter(source any)  {}
func (f *fakeMidea) RefreshStates(context.Context) {}
func (f *fakeMidea) States(context.Context, string) (map[string]any, error) {
	return map[string]any{}, nil
}

func TestDispatchMideaPowerToFan(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	if err := Save(ctx, store, Automation{
		ID: "ac1", Name: "Ar ligou", Enabled: true,
		Source:  Source{Type: SourceMideaState, Device: "1720000001", DP: "power", Value: true},
		Actions: []Action{{Kind: ActionFan, Thing: "rf01", DeviceID: "fan0", State: map[string]any{"speed": int64(1)}}},
	}); err != nil {
		t.Fatal(err)
	}

	fans := &fakeFans{calls: make(chan fanCall, 4)}
	d := New(store, &fakeSvc{}, fans, &fakeTuya{calls: make(chan tuyaCall, 4)})
	d.Start(ctx)

	d.HandleMideaState("1720000001", map[string]any{"power": true})
	select {
	case c := <-fans.calls:
		if c.thing != "rf01" || c.state["speed"] != float64(1) {
			t.Fatalf("unexpected call: %+v", c)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("automation did not fire")
	}

	// A repeated value must not fire again.
	d.HandleMideaState("1720000001", map[string]any{"power": true})
	select {
	case c := <-fans.calls:
		t.Fatalf("fired again on unchanged value: %+v", c)
	case <-time.After(150 * time.Millisecond):
	}
}

func TestDispatchMideaAction(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	if err := Save(ctx, store, Automation{
		ID: "ac2", Name: "Desliga o ar", Enabled: true,
		Source:  Source{Type: SourceManual},
		Actions: []Action{{Kind: ActionMidea, Device: "1720000001", State: map[string]any{"power": false}}},
	}); err != nil {
		t.Fatal(err)
	}
	midea := &fakeMidea{calls: make(chan mideaCall, 4)}
	d := New(store, nil, &fakeFans{calls: make(chan fanCall, 1)}, &fakeTuya{calls: make(chan tuyaCall, 1)})
	d.SetMidea(midea)
	d.Start(ctx)

	if _, err := d.Run(ctx, "ac2"); err != nil {
		t.Fatal(err)
	}
	select {
	case c := <-midea.calls:
		if c.id != "1720000001" || c.state["power"] != false {
			t.Fatalf("unexpected call: %+v", c)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("midea action did not run")
	}
}

// TestDispatchReportFiresRepeated checks that a "report" event fires every time,
// even with the same value (two unlocks with the same fingerprint), unlike a
// state "dp" event which is deduped by value.
func TestDispatchReportFiresRepeated(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	if err := Save(ctx, store, Automation{
		ID: "chegada", Name: "Chegada", Enabled: true,
		Source:  Source{Type: SourceTuyaDP, Device: "lock1", DP: "1"},
		Actions: []Action{{Kind: ActionFan, Thing: "rf01", DeviceID: "fan0", State: map[string]any{"light": true}}},
	}); err != nil {
		t.Fatal(err)
	}

	fans := &fakeFans{calls: make(chan fanCall, 4)}
	svc := &fakeSvc{}
	d := New(store, svc, fans, &fakeTuya{calls: make(chan tuyaCall, 1)})
	d.Start(ctx)
	defer d.Close()

	for i := 0; i < 2; i++ {
		svc.emit("lock1", service.EventRecord{Kind: "report", DPs: map[string]any{"1": float64(11)}})
	}

	for i := 0; i < 2; i++ {
		select {
		case c := <-fans.calls:
			if c.deviceID != "fan0" {
				t.Fatalf("unexpected call: %+v", c)
			}
		case <-time.After(2 * time.Second):
			t.Fatalf("only %d fire(s) before timeout", i)
		}
	}
	select {
	case extra := <-fans.calls:
		t.Fatalf("unexpected extra fire: %+v", extra)
	case <-time.After(150 * time.Millisecond):
	}
}

func TestScheduleActive(t *testing.T) {
	// 2026-11-13 is a Friday.
	friday := func(hour, minute int) time.Time {
		return time.Date(2026, 11, 13, hour, minute, 0, 0, time.Local)
	}
	saturday := func(hour, minute int) time.Time {
		return time.Date(2026, 11, 14, hour, minute, 0, 0, time.Local)
	}
	monday := func(hour, minute int) time.Time {
		return time.Date(2026, 11, 16, hour, minute, 0, 0, time.Local)
	}

	tests := []struct {
		name   string
		sched  Schedule
		at     time.Time
		wanted bool
	}{
		{name: "empty means always", sched: Schedule{}, at: friday(12, 0), wanted: true},
		{name: "overnight before start", sched: Schedule{Start: "18:00", End: "03:00"}, at: friday(17, 59), wanted: false},
		{name: "overnight at start", sched: Schedule{Start: "18:00", End: "03:00"}, at: friday(18, 0), wanted: true},
		{name: "overnight before midnight", sched: Schedule{Start: "18:00", End: "03:00"}, at: friday(23, 59), wanted: true},
		{name: "overnight after midnight", sched: Schedule{Start: "18:00", End: "03:00"}, at: saturday(2, 59), wanted: true},
		{name: "overnight at end", sched: Schedule{Start: "18:00", End: "03:00"}, at: saturday(3, 0), wanted: false},
		{name: "overnight midday", sched: Schedule{Start: "18:00", End: "03:00"}, at: saturday(12, 0), wanted: false},
		{name: "overnight day filter start", sched: Schedule{Start: "18:00", End: "03:00", Days: []int{5}}, at: friday(20, 0), wanted: true},
		{name: "overnight day filter carries to next day", sched: Schedule{Start: "18:00", End: "03:00", Days: []int{5}}, at: saturday(2, 0), wanted: true},
		{name: "overnight day filter excludes other day", sched: Schedule{Start: "18:00", End: "03:00", Days: []int{5}}, at: saturday(20, 0), wanted: false},
		{name: "overnight day filter excludes previous day", sched: Schedule{Start: "18:00", End: "03:00", Days: []int{5}}, at: friday(2, 0), wanted: false},
		{name: "same-day inside", sched: Schedule{Start: "08:00", End: "12:00", Days: []int{1}}, at: monday(9, 0), wanted: true},
		{name: "same-day at end", sched: Schedule{Start: "08:00", End: "12:00", Days: []int{1}}, at: monday(12, 0), wanted: false},
		{name: "same-day other weekday", sched: Schedule{Start: "08:00", End: "12:00", Days: []int{1}}, at: friday(9, 0), wanted: false},
		{name: "equal times all day on selected day", sched: Schedule{Start: "22:00", End: "22:00", Days: []int{5}}, at: friday(9, 0), wanted: true},
		{name: "equal times other day", sched: Schedule{Start: "22:00", End: "22:00", Days: []int{5}}, at: saturday(9, 0), wanted: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.sched.Active(tt.at); got != tt.wanted {
				t.Fatalf("Active(%s) = %v, want %v", tt.at.Format(time.RFC3339), got, tt.wanted)
			}
		})
	}
}

// TestDispatchMultipleDPs checks an unlock group: a lock reports a different DP
// for each opening method, so one rule listens to all of them.
func TestDispatchMultipleDPs(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	if err := Save(ctx, store, Automation{
		ID: "porta", Name: "Porta", Enabled: true,
		Source:  Source{Type: SourceTuyaDP, Device: "lock1", DPs: []string{"1", "2", "3"}},
		Actions: []Action{{Kind: ActionFan, Thing: "rf01", DeviceID: "fan0", State: map[string]any{"light": true}}},
	}); err != nil {
		t.Fatal(err)
	}

	fans := &fakeFans{calls: make(chan fanCall, 4)}
	svc := &fakeSvc{}
	d := New(store, svc, fans, &fakeTuya{calls: make(chan tuyaCall, 1)})
	d.Start(ctx)
	defer d.Close()

	svc.emit("lock1", service.EventRecord{Kind: "report", DPs: map[string]any{"2": float64(11)}})
	select {
	case c := <-fans.calls:
		if c.deviceID != "fan0" {
			t.Fatalf("unexpected call: %+v", c)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("action did not fire for a DP in the group")
	}

	svc.emit("lock1", service.EventRecord{Kind: "report", DPs: map[string]any{"9": float64(11)}})
	select {
	case extra := <-fans.calls:
		t.Fatalf("unexpected fire for a DP outside the group: %+v", extra)
	case <-time.After(150 * time.Millisecond):
	}
}

// TestDispatchScheduleUsesEventTime checks the window is evaluated against the
// event timestamp, so a delayed poll (02:59 delivered after 03:00) still fires.
func TestDispatchScheduleUsesEventTime(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	if err := Save(ctx, store, Automation{
		ID: "noite", Name: "Noite", Enabled: true,
		Source:   Source{Type: SourceTuyaDP, Device: "lock1", DP: "1"},
		Schedule: Schedule{Start: "18:00", End: "03:00"},
		Actions:  []Action{{Kind: ActionFan, Thing: "rf01", DeviceID: "fan0", State: map[string]any{"light": true}}},
	}); err != nil {
		t.Fatal(err)
	}

	fans := &fakeFans{calls: make(chan fanCall, 4)}
	svc := &fakeSvc{}
	d := New(store, svc, fans, &fakeTuya{calls: make(chan tuyaCall, 1)})
	d.Start(ctx)
	defer d.Close()

	svc.emit("lock1", service.EventRecord{
		At:   time.Date(2026, 11, 13, 15, 0, 0, 0, time.Local),
		Kind: "report",
		DPs:  map[string]any{"1": float64(11)},
	})
	select {
	case c := <-fans.calls:
		t.Fatalf("fired outside the window: %+v", c)
	case <-time.After(150 * time.Millisecond):
	}

	svc.emit("lock1", service.EventRecord{
		At:   time.Date(2026, 11, 13, 20, 0, 0, 0, time.Local),
		Kind: "report",
		DPs:  map[string]any{"1": float64(12)},
	})
	select {
	case c := <-fans.calls:
		if c.deviceID != "fan0" {
			t.Fatalf("unexpected call: %+v", c)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("action did not fire inside the window")
	}
}

// TestDispatchManualIgnoresSchedule documents the product rule: the time window
// gates automatic triggers only; a manual scene always runs when tapped.
func TestDispatchManualIgnoresSchedule(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	if err := Save(ctx, store, Automation{
		ID: "cena", Name: "Cena", Enabled: true,
		Source:   Source{Type: SourceManual},
		Schedule: Schedule{Start: "18:00", End: "03:00"},
		Actions:  []Action{{Kind: ActionTuya, Device: "lamp1", DP: "1", Value: true}},
	}); err != nil {
		t.Fatal(err)
	}

	tuya := &fakeTuya{calls: make(chan tuyaCall, 1)}
	d := New(store, &fakeSvc{}, &fakeFans{calls: make(chan fanCall, 1)}, tuya)
	d.now = func() time.Time { return time.Date(2026, 11, 13, 15, 0, 0, 0, time.Local) }

	if _, err := d.Run(ctx, "cena"); err != nil {
		t.Fatal(err)
	}
	select {
	case c := <-tuya.calls:
		if c.id != "lamp1" || c.value != true {
			t.Fatalf("unexpected call: %+v", c)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("manual scene was blocked by the schedule")
	}
}

// TestDispatchThingState checks a fan capability as a trigger. It uses the same
// dedup map as the other sources, so a repeated state does not fire again.
func TestDispatchThingState(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	if err := Save(ctx, store, Automation{
		ID: "luz", Name: "Luz do ventilador", Enabled: true,
		Source:  Source{Type: SourceThingState, Device: "rf01/fan0", DP: "light", Value: true},
		Actions: []Action{{Kind: ActionTuya, Device: "lamp1", DP: "1", Value: true}},
	}); err != nil {
		t.Fatal(err)
	}

	tuya := &fakeTuya{calls: make(chan tuyaCall, 2)}
	d := New(store, &fakeSvc{}, &fakeFans{calls: make(chan fanCall, 1)}, tuya)
	d.Start(ctx)
	defer d.Close()

	at := time.Date(2026, 11, 13, 20, 0, 0, 0, time.Local)
	d.HandleThingState("rf01", "fan0", map[string]any{"light": false}, at)
	select {
	case c := <-tuya.calls:
		t.Fatalf("fired for the baseline value: %+v", c)
	case <-time.After(150 * time.Millisecond):
	}

	d.HandleThingState("rf01", "fan0", map[string]any{"light": true}, at)
	select {
	case c := <-tuya.calls:
		if c.id != "lamp1" || c.value != true {
			t.Fatalf("unexpected call: %+v", c)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("fan light did not trigger")
	}

	d.HandleThingState("rf01", "fan0", map[string]any{"light": true}, at)
	select {
	case c := <-tuya.calls:
		t.Fatalf("fired again on unchanged value: %+v", c)
	case <-time.After(150 * time.Millisecond):
	}
}

// TestDispatchEventBusThingState checks the wiring from the hub bus to the
// dispatcher, so a real fan state event (not only a direct handler call) fires.
func TestDispatchEventBusThingState(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	if err := Save(ctx, store, Automation{
		ID: "bus", Name: "Bus", Enabled: true,
		Source:  Source{Type: SourceThingState, Device: "rf01/fan0", DP: "light", Value: true},
		Actions: []Action{{Kind: ActionTuya, Device: "lamp1", DP: "1", Value: true}},
	}); err != nil {
		t.Fatal(err)
	}

	bus := eventbus.New()
	tuya := &fakeTuya{calls: make(chan tuyaCall, 2)}
	d := New(store, &fakeSvc{}, &fakeFans{calls: make(chan fanCall, 1)}, tuya)
	d.SetEventBus(bus)
	d.Start(ctx)
	defer d.Close()

	bus.PublishData("state", map[string]any{
		"thing": "rf01", "id": "fan0", "reported": map[string]any{"light": true},
	})
	select {
	case c := <-tuya.calls:
		if c.id != "lamp1" || c.value != true {
			t.Fatalf("unexpected call: %+v", c)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("bus state did not trigger")
	}
}

func TestResolveInstant(t *testing.T) {
	now := time.Date(2026, 11, 13, 9, 0, 0, 0, time.Local)

	t.Run("relative delay", func(t *testing.T) {
		src := Source{Type: SourceTime, InSecs: 600}
		if err := resolveInstant(&src, now); err != nil {
			t.Fatal(err)
		}
		want := now.Add(10 * time.Minute).Format(time.RFC3339)
		if src.FireAt != want {
			t.Fatalf("fire_at = %q, want %q", src.FireAt, want)
		}
		if src.InSecs != 0 {
			t.Fatalf("in_seconds not cleared: %d", src.InSecs)
		}
	})

	t.Run("at still ahead is today", func(t *testing.T) {
		src := Source{Type: SourceTime, At: "14:00"}
		if err := resolveInstant(&src, now); err != nil {
			t.Fatal(err)
		}
		want := time.Date(2026, 11, 13, 14, 0, 0, 0, time.Local).Format(time.RFC3339)
		if src.FireAt != want {
			t.Fatalf("fire_at = %q, want %q", src.FireAt, want)
		}
	})

	t.Run("at already past is tomorrow", func(t *testing.T) {
		late := time.Date(2026, 11, 13, 15, 0, 0, 0, time.Local)
		src := Source{Type: SourceTime, At: "14:00"}
		if err := resolveInstant(&src, late); err != nil {
			t.Fatal(err)
		}
		want := time.Date(2026, 11, 14, 14, 0, 0, 0, time.Local).Format(time.RFC3339)
		if src.FireAt != want {
			t.Fatalf("fire_at = %q, want %q", src.FireAt, want)
		}
	})

	t.Run("explicit date", func(t *testing.T) {
		src := Source{Type: SourceTime, At: "10:00", Date: "2026-12-01"}
		if err := resolveInstant(&src, now); err != nil {
			t.Fatal(err)
		}
		want := time.Date(2026, 12, 1, 10, 0, 0, 0, time.Local).Format(time.RFC3339)
		if src.FireAt != want {
			t.Fatalf("fire_at = %q, want %q", src.FireAt, want)
		}
	})

	t.Run("weekly keeps today when the time is still ahead", func(t *testing.T) {
		// now is a Friday at 09:00; Friday is selected.
		src := Source{Type: SourceTime, At: "14:00", Days: []int{5}}
		if err := resolveInstant(&src, now); err != nil {
			t.Fatal(err)
		}
		want := time.Date(2026, 11, 13, 14, 0, 0, 0, time.Local).Format(time.RFC3339)
		if src.FireAt != want {
			t.Fatalf("fire_at = %q, want %q", src.FireAt, want)
		}
	})

	t.Run("weekly picks the next matching weekday", func(t *testing.T) {
		// now is a Friday; only Monday is selected.
		src := Source{Type: SourceTime, At: "14:00", Days: []int{1}}
		if err := resolveInstant(&src, now); err != nil {
			t.Fatal(err)
		}
		want := time.Date(2026, 11, 16, 14, 0, 0, 0, time.Local).Format(time.RFC3339)
		if src.FireAt != want {
			t.Fatalf("fire_at = %q, want %q", src.FireAt, want)
		}
	})

	t.Run("already resolved is kept", func(t *testing.T) {
		fixed := time.Date(2026, 11, 13, 12, 0, 0, 0, time.Local).Format(time.RFC3339)
		src := Source{Type: SourceTime, At: "14:00", FireAt: fixed, InSecs: 5}
		if err := resolveInstant(&src, now); err != nil {
			t.Fatal(err)
		}
		if src.FireAt != fixed {
			t.Fatalf("fire_at = %q, want %q", src.FireAt, fixed)
		}
		if src.InSecs != 0 {
			t.Fatalf("in_seconds not cleared: %d", src.InSecs)
		}
	})

	t.Run("invalid at", func(t *testing.T) {
		src := Source{Type: SourceTime, At: "nope"}
		if err := resolveInstant(&src, now); err == nil {
			t.Fatal("expected an error for an invalid at")
		}
	})

	t.Run("event source is untouched", func(t *testing.T) {
		src := Source{Type: SourceTuyaDP, Device: "sw1", DP: "1"}
		if err := resolveInstant(&src, now); err != nil {
			t.Fatal(err)
		}
		if src.FireAt != "" {
			t.Fatalf("event source gained a fire_at: %q", src.FireAt)
		}
	})
}

func TestValidateTimeSource(t *testing.T) {
	action := []Action{{Kind: ActionTuya, Device: "lamp1", DP: "1", Value: false}}
	cases := []struct {
		name    string
		src     Source
		wantErr bool
	}{
		{"relative", Source{Type: SourceTime, InSecs: 600}, false},
		{"at", Source{Type: SourceTime, At: "14:00"}, false},
		{"at with date", Source{Type: SourceTime, At: "14:00", Date: "2026-12-01"}, false},
		{"missing instant", Source{Type: SourceTime}, true},
		{"both at and relative", Source{Type: SourceTime, At: "14:00", InSecs: 60}, true},
		{"bad at", Source{Type: SourceTime, At: "25:00"}, true},
		{"bad date", Source{Type: SourceTime, At: "14:00", Date: "01/12/2026"}, true},
		{"weekly", Source{Type: SourceTime, At: "14:00", Days: []int{1, 3, 5}}, false},
		{"weekly without at", Source{Type: SourceTime, Days: []int{1}}, true},
		{"weekly with date", Source{Type: SourceTime, At: "14:00", Date: "2026-12-01", Days: []int{1}}, true},
		{"weekly bad day", Source{Type: SourceTime, At: "14:00", Days: []int{7}}, true},
		{"weekly duplicate day", Source{Type: SourceTime, At: "14:00", Days: []int{1, 1}}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := Validate(Automation{Name: "x", Enabled: true, Source: tc.src, Actions: action})
			if tc.wantErr && err == nil {
				t.Fatal("expected an error")
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}

	// A schedule window is meaningless on a time trigger (the instant is the
	// schedule) and must be rejected.
	err := Validate(Automation{
		Name: "x", Enabled: true,
		Source:   Source{Type: SourceTime, At: "14:00"},
		Schedule: Schedule{Start: "18:00", End: "23:00"},
		Actions:  action,
	})
	if err == nil {
		t.Fatal("expected a schedule window on a time trigger to be rejected")
	}

	// A weekly rule re-arms itself, so it cannot be volatile (once).
	if err := Validate(Automation{
		Name: "x", Enabled: true, Once: true,
		Source:  Source{Type: SourceTime, At: "14:00", Days: []int{1}},
		Actions: action,
	}); err == nil {
		t.Fatal("expected a volatile weekly rule to be rejected")
	}
}

func TestDispatchTimeFiresAndDeletes(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	now := time.Date(2026, 11, 13, 15, 0, 0, 0, time.Local)

	if err := Save(ctx, store, Automation{
		ID: "off", Name: "Desliga às 14", Enabled: true, Once: true,
		Source:  Source{Type: SourceTime, FireAt: now.Add(-30 * time.Second).Format(time.RFC3339)},
		Actions: []Action{{Kind: ActionTuya, Device: "lamp1", DP: "1", Value: false}},
	}); err != nil {
		t.Fatal(err)
	}

	tuya := &fakeTuya{calls: make(chan tuyaCall, 2)}
	d := New(store, &fakeSvc{}, &fakeFans{calls: make(chan fanCall, 1)}, tuya)
	d.now = func() time.Time { return now }
	d.Start(ctx)
	defer d.Close()

	d.runDue(ctx)

	select {
	case c := <-tuya.calls:
		if c.id != "lamp1" || c.value != false {
			t.Fatalf("unexpected call: %+v", c)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("scheduled automation did not fire")
	}

	if _, err := store.GetAutomation(ctx, "off"); !errors.Is(err, db.ErrNotFound) {
		t.Fatalf("volatile rule was not deleted: err=%v", err)
	}
}

func TestDispatchTimeSkippedWhenStale(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	now := time.Date(2026, 11, 13, 15, 0, 0, 0, time.Local)

	if err := Save(ctx, store, Automation{
		ID: "stale", Name: "Ligar às 10", Enabled: true, Once: true,
		Source:  Source{Type: SourceTime, FireAt: now.Add(-10 * time.Minute).Format(time.RFC3339)},
		Actions: []Action{{Kind: ActionTuya, Device: "lamp1", DP: "1", Value: true}},
	}); err != nil {
		t.Fatal(err)
	}

	tuya := &fakeTuya{calls: make(chan tuyaCall, 2)}
	d := New(store, &fakeSvc{}, &fakeFans{calls: make(chan fanCall, 1)}, tuya)
	d.now = func() time.Time { return now }
	d.Start(ctx)
	defer d.Close()

	d.runDue(ctx)

	select {
	case c := <-tuya.calls:
		t.Fatalf("stale rule fired: %+v", c)
	case <-time.After(200 * time.Millisecond):
	}

	if _, err := store.GetAutomation(ctx, "stale"); !errors.Is(err, db.ErrNotFound) {
		t.Fatalf("stale volatile rule was not deleted: err=%v", err)
	}
}

func TestDispatchTimeNonOnceDisabled(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	now := time.Date(2026, 11, 13, 15, 0, 0, 0, time.Local)

	if err := Save(ctx, store, Automation{
		ID: "keep", Name: "Desliga uma vez", Enabled: true, Once: false,
		Source:  Source{Type: SourceTime, FireAt: now.Add(-30 * time.Second).Format(time.RFC3339)},
		Actions: []Action{{Kind: ActionTuya, Device: "lamp1", DP: "1", Value: false}},
	}); err != nil {
		t.Fatal(err)
	}

	tuya := &fakeTuya{calls: make(chan tuyaCall, 2)}
	d := New(store, &fakeSvc{}, &fakeFans{calls: make(chan fanCall, 1)}, tuya)
	d.now = func() time.Time { return now }
	d.Start(ctx)
	defer d.Close()

	d.runDue(ctx)

	select {
	case <-tuya.calls:
	case <-time.After(2 * time.Second):
		t.Fatal("scheduled automation did not fire")
	}

	got, err := store.GetAutomation(ctx, "keep")
	if err != nil {
		t.Fatalf("non-volatile rule should stay: %v", err)
	}
	if got.Enabled {
		t.Fatal("non-volatile rule should have been disabled")
	}
}

func TestDispatchWeeklyRearms(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	// Friday 14:00:30, right after the Friday 14:00 slot.
	now := time.Date(2026, 11, 13, 14, 0, 30, 0, time.Local)

	if err := Save(ctx, store, Automation{
		ID: "weekly", Name: "Toda sexta às 14", Enabled: true,
		Source:  Source{Type: SourceTime, At: "14:00", Days: []int{5}, FireAt: now.Add(-30 * time.Second).Format(time.RFC3339)},
		Actions: []Action{{Kind: ActionTuya, Device: "lamp1", DP: "1", Value: false}},
	}); err != nil {
		t.Fatal(err)
	}

	tuya := &fakeTuya{calls: make(chan tuyaCall, 2)}
	d := New(store, &fakeSvc{}, &fakeFans{calls: make(chan fanCall, 1)}, tuya)
	d.now = func() time.Time { return now }
	d.Start(ctx)
	defer d.Close()

	d.runDue(ctx)

	select {
	case <-tuya.calls:
	case <-time.After(2 * time.Second):
		t.Fatal("weekly automation did not fire")
	}

	got, err := store.GetAutomation(ctx, "weekly")
	if err != nil {
		t.Fatalf("weekly rule should stay: %v", err)
	}
	if !got.Enabled {
		t.Fatal("weekly rule should stay enabled")
	}
	var src Source
	if err := json.Unmarshal(got.Source, &src); err != nil {
		t.Fatal(err)
	}
	want := time.Date(2026, 11, 20, 14, 0, 0, 0, time.Local).Format(time.RFC3339)
	if src.FireAt != want {
		t.Fatalf("rearmed fire_at = %q, want %q", src.FireAt, want)
	}
}
