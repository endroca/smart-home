// Package functions owns the per-device function catalog: the labels the voice
// resolver matches against and the actions "desligar tudo" runs.
//
// It derives a generic default catalog from each device's capabilities (the
// ESP32 manifest, the cached Tuya specs or the Midea specs) and lets the
// operator override the labels, aliases, voice and "off" flags through the
// device configuration. The hub therefore never hard-codes what a device can do
// or what "off" means for it.
package functions

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"smart_home/hub/internal/db"
	"smart_home/hub/internal/model"
)

// Drivers, matching the transports the hub knows.
const (
	DriverFan   = "fan"
	DriverTuya  = "tuya"
	DriverMidea = "midea"
)

// Kinds of a function value.
const (
	KindBool   = "bool"
	KindEnum   = "enum"
	KindNumber = "number"
	KindText   = "text"
)

// Config is one operator-editable function of a device, as stored in
// devices.functions_json.
type Config struct {
	Key      string   `json:"key"`
	Label    string   `json:"label"`
	Aliases  []string `json:"aliases,omitempty"`
	Voice    bool     `json:"voice"`
	Off      bool     `json:"off"`
	OffValue any      `json:"off_value,omitempty"`
}

// Function is a Config enriched with the metadata derived from the device, so
// the UI and the resolvers do not have to look at the specs again.
type Function struct {
	Config
	Driver string   `json:"driver"`
	Kind   string   `json:"kind"`
	Values []string `json:"values,omitempty"`
	Min    *float64 `json:"min,omitempty"`
	Max    *float64 `json:"max,omitempty"`
	Step   *float64 `json:"step,omitempty"`
	Unit   string   `json:"unit,omitempty"`
}

// Catalog is the effective function list of one device.
type Catalog struct {
	DeviceID string `json:"device_id"`
	Driver   string `json:"driver"`
	// Configured reports whether the operator edited the catalog (even to an
	// empty list); when false the list is the derived default.
	Configured bool       `json:"configured"`
	Functions  []Function `json:"functions"`
}

// Command is one action "desligar tudo" runs to turn a device off.
type Command struct {
	Key   string `json:"key"`
	Value any    `json:"value"`
}

// ThingSource gives the live manifest/state snapshot (implemented by the hub).
type ThingSource interface {
	Snapshot() []*model.Thing
}

// Service derives and stores the function catalog of devices.
type Service struct {
	store  *db.Store
	things ThingSource
}

// New builds the function service. things may be nil in tests; fan functions
// then come back empty.
func New(store *db.Store, things ThingSource) *Service {
	return &Service{store: store, things: things}
}

// Catalog returns the effective functions of a device: the stored configuration
// when the operator edited it, or the derived defaults otherwise.
func (s *Service) Catalog(ctx context.Context, id string) (*Catalog, error) {
	d, err := s.store.GetDevice(ctx, id)
	if err != nil {
		return nil, err
	}
	derived, err := s.derive(ctx, d)
	if err != nil {
		return nil, err
	}
	cat := &Catalog{DeviceID: id, Driver: driverOf(d), Functions: derived}
	if cat.Functions == nil {
		cat.Functions = []Function{}
	}
	stored := strings.TrimSpace(d.FunctionsJSON)
	if stored == "" {
		return cat, nil
	}
	var cfgs []Config
	if err := json.Unmarshal([]byte(stored), &cfgs); err != nil {
		// A corrupt value falls back to the derived defaults instead of
		// breaking the device.
		return cat, nil
	}
	byKey := make(map[string]Function, len(derived))
	for _, f := range derived {
		byKey[f.Key] = f
	}
	merged := make([]Function, 0, len(cfgs))
	for _, c := range cfgs {
		base, ok := byKey[strings.TrimSpace(c.Key)]
		if !ok {
			// The capability is gone; the stale config is ignored.
			continue
		}
		base.Config = c
		if strings.TrimSpace(base.Label) == "" {
			base.Label = base.Key
		}
		merged = append(merged, base)
	}
	cat.Configured = true
	cat.Functions = merged
	return cat, nil
}

// Save stores the editable configuration of a device's functions. Every key
// must exist in the derived catalog and a label must not be empty.
func (s *Service) Save(ctx context.Context, id string, cfgs []Config) (*Catalog, error) {
	d, err := s.store.GetDevice(ctx, id)
	if err != nil {
		return nil, err
	}
	derived, err := s.derive(ctx, d)
	if err != nil {
		return nil, err
	}
	known := make(map[string]Function, len(derived))
	for _, f := range derived {
		known[f.Key] = f
	}
	clean := make([]Config, 0, len(cfgs))
	seen := make(map[string]bool, len(cfgs))
	for i, c := range cfgs {
		key := strings.TrimSpace(c.Key)
		f, ok := known[key]
		if !ok {
			return nil, fmt.Errorf("unknown function %q", key)
		}
		if seen[key] {
			return nil, fmt.Errorf("duplicate function %q", key)
		}
		seen[key] = true
		label := strings.TrimSpace(c.Label)
		if label == "" {
			label = f.Label
		}
		if label == "" {
			return nil, fmt.Errorf("function %d needs a label", i+1)
		}
		offValue := c.OffValue
		if c.Off {
			v, err := offValueFor(f, c.OffValue)
			if err != nil {
				return nil, fmt.Errorf("function %q: %w", key, err)
			}
			offValue = v
		} else {
			offValue = nil
		}
		clean = append(clean, Config{
			Key:      key,
			Label:    label,
			Aliases:  cleanAliases(c.Aliases),
			Voice:    c.Voice,
			Off:      c.Off,
			OffValue: offValue,
		})
	}
	raw, err := json.Marshal(clean)
	if err != nil {
		return nil, err
	}
	if err := s.store.SetDeviceFunctions(ctx, id, string(raw)); err != nil {
		return nil, err
	}
	return s.Catalog(ctx, id)
}

// Reset clears the stored configuration, so the device goes back to the derived
// defaults.
func (s *Service) Reset(ctx context.Context, id string) (*Catalog, error) {
	if _, err := s.store.GetDevice(ctx, id); err != nil {
		return nil, err
	}
	if err := s.store.SetDeviceFunctions(ctx, id, ""); err != nil {
		return nil, err
	}
	return s.Catalog(ctx, id)
}

// Off returns the commands that turn a device off, following its catalog: every
// function marked "off", with its configured off value.
func (s *Service) Off(ctx context.Context, id string) ([]Command, error) {
	cat, err := s.Catalog(ctx, id)
	if err != nil {
		return nil, err
	}
	out := []Command{}
	for _, f := range cat.Functions {
		if !f.Off {
			continue
		}
		v := f.OffValue
		if v == nil {
			if d, ok := defaultOffValue(f); ok {
				v = d
			} else {
				continue
			}
		}
		out = append(out, Command{Key: f.Key, Value: v})
	}
	return out, nil
}

/* ---------- derivacao ---------- */

func driverOf(d db.Device) string {
	switch d.Kind {
	case db.KindFan:
		return DriverFan
	case db.KindMidea:
		return DriverMidea
	case db.KindTuya:
		return DriverTuya
	default:
		return ""
	}
}

func (s *Service) derive(ctx context.Context, d db.Device) ([]Function, error) {
	switch d.Kind {
	case db.KindFan:
		return s.deriveFan(d), nil
	case db.KindTuya:
		return s.deriveTuya(ctx, d)
	case db.KindMidea:
		return s.deriveMidea(ctx, d)
	default:
		return nil, nil
	}
}

// deriveFan builds the catalog from the capabilities the gateway advertises in
// its manifest.
func (s *Service) deriveFan(d db.Device) []Function {
	if s.things == nil || d.Owner == "" || d.DeviceID == "" {
		return nil
	}
	var caps map[string]model.Capability
	for _, t := range s.things.Snapshot() {
		if t == nil || t.ID != d.Owner {
			continue
		}
		if md, ok := t.Device(d.DeviceID); ok {
			caps = md.Capabilities
		}
		break
	}
	if len(caps) == 0 {
		return nil
	}
	keys := make([]string, 0, len(caps))
	for k := range caps {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	out := make([]Function, 0, len(keys))
	for _, key := range keys {
		c := caps[key]
		if !c.IsWritable() {
			continue
		}
		f := Function{
			Config: Config{Key: key, Label: fanLabel(key), Voice: true},
			Driver: DriverFan,
			Kind:   capKind(c.Kind),
		}
		if f.Kind == KindEnum {
			f.Values = append([]string(nil), c.Values...)
		}
		applyRange(&f, c.Min, c.Max, c.Step, c.Unit)
		if key == "power" {
			f.Off = true
			f.OffValue = false
		}
		out = append(out, f)
	}
	return out
}

// deriveTuya builds the catalog from the writable cached specs, skipping the
// data points that configure the device instead of driving an output.
func (s *Service) deriveTuya(ctx context.Context, d db.Device) ([]Function, error) {
	specs, err := s.store.ListSpecs(ctx, d.ID)
	if err != nil {
		return nil, err
	}
	out := []Function{}
	for _, sp := range specs {
		if !sp.Writable || isTuyaConfigDP(sp.Code) {
			continue
		}
		f := Function{
			Config: Config{Key: sp.DPID, Label: tuyaLabel(sp), Voice: true},
			Driver: DriverTuya,
			Kind:   tuyaKind(sp.Type),
		}
		if f.Kind == KindEnum {
			f.Values = enumStrings(sp.Values)
		}
		if f.Kind == KindNumber {
			applyNumMeta(&f, sp.Values)
		}
		if f.Kind == KindBool && isTuyaOffCode(sp.Code) {
			f.Off = true
			f.OffValue = false
		}
		out = append(out, f)
	}
	return out, nil
}

// deriveMidea builds the catalog from the actions the appliance advertises.
func (s *Service) deriveMidea(ctx context.Context, d db.Device) ([]Function, error) {
	specs, err := s.store.ListMideaSpecs(ctx, d.ID)
	if err != nil {
		return nil, err
	}
	out := []Function{}
	for _, sp := range specs {
		if !sp.Writable {
			continue
		}
		f := Function{
			Config: Config{Key: sp.ID, Label: sp.Name, Voice: true},
			Driver: DriverMidea,
			Kind:   mideaKind(sp.Type),
		}
		if f.Kind == KindEnum {
			f.Values = enumStrings(sp.Values)
		}
		if f.Kind == KindNumber {
			applyNumMeta(&f, sp.Values)
		}
		if sp.ID == "power" {
			f.Off = true
			f.OffValue = false
		}
		out = append(out, f)
	}
	return out, nil
}

/* ---------- helpers ---------- */

// tuyaConfigDPs are writable data points that configure the appliance rather
// than drive an output; they never become functions or "off" targets. Countdowns
// are matched by prefix (see isTuyaConfigDP) since a multi-gang switch exposes
// one per key.
var tuyaConfigDPs = map[string]bool{
	"switch_backlight":  true,
	"switch_inching":    true,
	"child_lock":        true,
	"overcharge_switch": true,
	"relay_status":      true,
	"power_off":         true,
	"light_mode":        true,
	"cycle_time":        true,
	"random_time":       true,
	"cur_power":         true,
	"cur_voltage":       true,
	"cur_current":       true,
}

// isTuyaConfigDP reports whether a data point configures the appliance instead
// of driving an output.
func isTuyaConfigDP(code string) bool {
	return tuyaConfigDPs[code] || strings.HasPrefix(code, "countdown")
}

// tuyaOutputCodes are the boolean data points that drive a physical output.
// "off" only ever touches these, never a configuration flag such as the key
// backlight (which also starts with "switch_"). Numbered gangs are handled by
// tuyaGangCode.
var tuyaOutputCodes = map[string]bool{
	"power":      true,
	"relay":      true,
	"switch":     true,
	"switch_led": true,
	"switch_usb": true,
}

// isTuyaOffCode reports whether a boolean output counts as "off" by default:
// the numbered switch gangs plus power/relay/light/USB.
func isTuyaOffCode(code string) bool {
	return tuyaOutputCodes[code] || tuyaGangCode(code)
}

// tuyaGangCode matches the numbered gangs of a multi-switch (switch_1, ...).
// A non-numeric suffix (switch_led, switch_backlight) is not a gang.
func tuyaGangCode(code string) bool {
	n, ok := strings.CutPrefix(code, "switch_")
	if !ok || n == "" {
		return false
	}
	for _, r := range n {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

var fanLabels = map[string]string{
	"power":     "Ligar/desligar",
	"light":     "Luz",
	"speed":     "Velocidade",
	"direction": "Direção",
}

func fanLabel(key string) string {
	if l, ok := fanLabels[key]; ok {
		return l
	}
	return key
}

var tuyaLabels = map[string]string{
	"power":      "Energia",
	"relay":      "Relé",
	"switch":     "Chave",
	"switch_1":   "Chave 1",
	"switch_2":   "Chave 2",
	"switch_3":   "Chave 3",
	"switch_4":   "Chave 4",
	"switch_led": "Luz do painel",
	"light":      "Luz",
}

func tuyaLabel(sp db.Spec) string {
	if n := strings.TrimSpace(sp.Name); n != "" {
		return n
	}
	if l, ok := tuyaLabels[sp.Code]; ok {
		return l
	}
	if strings.TrimSpace(sp.Code) != "" {
		return sp.Code
	}
	return sp.DPID
}

func capKind(kind string) string {
	switch strings.ToLower(kind) {
	case "switch", "boolean", "bool":
		return KindBool
	case "enum":
		return KindEnum
	case "number", "integer", "float":
		return KindNumber
	default:
		return KindText
	}
}

func tuyaKind(t string) string {
	switch strings.ToLower(t) {
	case "boolean", "bool":
		return KindBool
	case "enum":
		return KindEnum
	case "integer", "number", "float":
		return KindNumber
	default:
		return KindText
	}
}

func mideaKind(t string) string { return tuyaKind(t) }

func applyRange(f *Function, min, max, step *float64, unit string) {
	f.Min, f.Max, f.Step, f.Unit = min, max, step, unit
}

// numMeta is the shape Tuya and Midea use inside `values`/`options`.
type numMeta struct {
	Min   *float64 `json:"min"`
	Max   *float64 `json:"max"`
	Step  *float64 `json:"step"`
	Unit  string   `json:"unit"`
	Range []any    `json:"range"`
}

func parseMeta(raw json.RawMessage) numMeta {
	var m numMeta
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &m)
	}
	return m
}

func applyNumMeta(f *Function, raw json.RawMessage) {
	m := parseMeta(raw)
	f.Min, f.Max, f.Step, f.Unit = m.Min, m.Max, m.Step, m.Unit
}

// enumStrings normalizes `values`/`options`: a raw list or an object with a
// "range" list.
func enumStrings(raw json.RawMessage) []string {
	if len(raw) == 0 {
		return nil
	}
	var list []any
	if err := json.Unmarshal(raw, &list); err != nil {
		var obj struct {
			Range []any `json:"range"`
		}
		if err := json.Unmarshal(raw, &obj); err != nil {
			return nil
		}
		list = obj.Range
	}
	out := make([]string, 0, len(list))
	for _, v := range list {
		out = append(out, fmt.Sprint(v))
	}
	return out
}

func cleanAliases(in []string) []string {
	out := make([]string, 0, len(in))
	seen := map[string]bool{}
	for _, a := range in {
		a = strings.TrimSpace(a)
		if a == "" || seen[strings.ToLower(a)] {
			continue
		}
		seen[strings.ToLower(a)] = true
		out = append(out, a)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// offValueFor validates and normalizes the off value against the function kind.
func offValueFor(f Function, v any) (any, error) {
	if v == nil {
		if d, ok := defaultOffValue(f); ok {
			return d, nil
		}
		return nil, fmt.Errorf("off value is required")
	}
	switch f.Kind {
	case KindBool:
		b, ok := v.(bool)
		if !ok {
			return nil, fmt.Errorf("off value must be true or false")
		}
		return b, nil
	case KindNumber:
		n, ok := asFloat(v)
		if !ok {
			return nil, fmt.Errorf("off value must be a number")
		}
		if (f.Min != nil && n < *f.Min) || (f.Max != nil && n > *f.Max) {
			return nil, fmt.Errorf("off value %v is out of range", n)
		}
		return n, nil
	case KindEnum:
		want := fmt.Sprint(v)
		for _, allowed := range f.Values {
			if allowed == want {
				return v, nil
			}
		}
		return nil, fmt.Errorf("off value %q is not one of %s", want, strings.Join(f.Values, ", "))
	default:
		return fmt.Sprint(v), nil
	}
}

// defaultOffValue is the value used when a function is marked "off" without an
// explicit value. Booleans default to false and numbers to zero; enums and text
// cannot be guessed.
func defaultOffValue(f Function) (any, bool) {
	switch f.Kind {
	case KindBool:
		return false, true
	case KindNumber:
		return float64(0), true
	default:
		return nil, false
	}
}

func asFloat(v any) (float64, bool) {
	switch t := v.(type) {
	case float64:
		return t, true
	case float32:
		return float64(t), true
	case int:
		return float64(t), true
	case int64:
		return float64(t), true
	case json.Number:
		f, err := t.Float64()
		return f, err == nil
	default:
		return 0, false
	}
}
