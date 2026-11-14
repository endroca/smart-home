// Package midea integrates Midea SmartHome V3 appliances with local-first
// control. The cloud client is used only to retrieve a device's LAN secrets.
package midea

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"time"

	"smart_home/hub/internal/db"
	"smart_home/hub/internal/eventbus"
	"smart_home/hub/internal/midea/cloud"
	"smart_home/hub/internal/midea/discovery"
	"smart_home/hub/internal/midea/lan"
)

var ErrCloudNotConfigured = errors.New("midea: SmartHome account is not configured")

type Config struct {
	Store                     *db.Store
	Username, Password, Cloud string
	ScanTimeout               time.Duration
	// StateSource accepts the logical state of every appliance, so the
	// automations can trigger on "power", "target_temp", ... changes.
	StateSource StateSource
	// StatePoll is how often each appliance is read for the state source.
	StatePoll time.Duration
}

// StateSource receives the logical state of an appliance whenever it changes.
// It mirrors dispatch.MideaStateSource; the service declares its own copy so
// the integration package does not depend on the dispatcher.
type StateSource interface {
	HandleMideaState(deviceID string, state map[string]any)
}

type Service struct {
	cfg     Config
	cloud   *cloud.Client
	mu      sync.Mutex
	clients map[string]cachedClient
	// lastSeen records the last time an appliance answered, so the UI can
	// report it as online without dialing every device on each page load.
	lastSeen map[string]time.Time
	// energyCache keeps the last consumption reading per device/range so the
	// UI can poll it without a LAN round trip on every render.
	energyCache map[string]energyCacheEntry
	// stateSource receives the logical state on every change; it is the
	// automation hook, like the Tuya event handler.
	stateSource StateSource
	bus         *eventbus.Bus
	done        chan struct{}
}

type cachedClient struct {
	addr   string
	client *lan.Client
}

type ScanResult struct {
	ID         string `json:"id"`
	IP         string `json:"ip"`
	Port       int    `json:"port"`
	Type       int    `json:"type"`
	Model      string `json:"model"`
	Protocol   int    `json:"protocol"`
	Name       string `json:"name,omitempty"`
	Registered bool   `json:"registered"`
}

func Open(cfg Config) (*Service, error) {
	if cfg.Store == nil {
		return nil, errors.New("midea: database store is required")
	}
	if cfg.ScanTimeout <= 0 {
		cfg.ScanTimeout = 5 * time.Second
	}
	s := &Service{cfg: cfg, clients: map[string]cachedClient{}, lastSeen: map[string]time.Time{}, energyCache: map[string]energyCacheEntry{}, stateSource: cfg.StateSource, done: make(chan struct{})}
	if strings.TrimSpace(cfg.Username) != "" && strings.TrimSpace(cfg.Password) != "" {
		name := cfg.Cloud
		if name == "" {
			name = "SmartHome"
		}
		c, err := cloud.New(name, cfg.Username, cfg.Password)
		if err != nil {
			return nil, err
		}
		s.cloud = c
	}
	return s, nil
}

func (s *Service) Close() error {
	select {
	case <-s.done:
	default:
		close(s.done)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, cached := range s.clients {
		_ = cached.client.Close()
		delete(s.clients, id)
	}
	return nil
}

func (s *Service) CloudConfigured() bool         { return s.cloud != nil }
func (s *Service) SetEventBus(bus *eventbus.Bus) { s.bus = bus }

// Online reports whether the hub has a live LAN link to the appliance or heard
// from it recently. It never dials, so listing devices stays cheap.
func (s *Service) Online(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if cached, ok := s.clients[id]; ok && cached.client != nil {
		return true
	}
	seen, ok := s.lastSeen[id]
	return ok && time.Since(seen) < 90*time.Second
}

func (s *Service) markSeen(id string) {
	s.mu.Lock()
	s.lastSeen[id] = time.Now()
	s.mu.Unlock()
}

// Forget closes the cached LAN link and removes the appliance from the
// registry. Its cached capabilities and state are removed with the device row.
func (s *Service) Forget(ctx context.Context, id string) error {
	s.invalidateClient(id)
	s.mu.Lock()
	delete(s.lastSeen, id)
	s.mu.Unlock()
	return s.cfg.Store.DeleteDevice(ctx, id)
}

func (s *Service) Devices(ctx context.Context) ([]db.Device, error) {
	return s.cfg.Store.ListDevices(ctx, db.KindMidea)
}

// CachedState returns the last state persisted for an appliance (no LAN I/O).
func (s *Service) CachedState(ctx context.Context, id string) (map[string]any, error) {
	return s.cfg.Store.MideaStates(ctx, id)
}

// States is the automation-facing alias of CachedState.
func (s *Service) States(ctx context.Context, id string) (map[string]any, error) {
	return s.CachedState(ctx, id)
}

// StateSourceSetter attaches the automation sink. It is typed as any so the
// service stays usable with a structural interface (dispatch.MideaStateSource)
// without importing the dispatcher package.
func (s *Service) StateSourceSetter(source any) {
	if source == nil {
		return
	}
	if typed, ok := source.(StateSource); ok {
		s.mu.Lock()
		s.stateSource = typed
		s.mu.Unlock()
	}
}

func (s *Service) Scan(ctx context.Context, target string) ([]ScanResult, error) {
	devices, err := discovery.Discover(ctx, discovery.Options{Target: target, Timeout: s.cfg.ScanTimeout})
	if err != nil {
		return nil, err
	}
	// Some Wi-Fi access points isolate broadcasts from clients. If a broadcast
	// finds nothing, try known appliance addresses directly as a recovery path.
	if len(devices) == 0 && target == "" {
		registered, listErr := s.Devices(ctx)
		if listErr == nil {
			for _, known := range registered {
				if known.IP == "" || ctx.Err() != nil {
					continue
				}
				direct, directErr := discovery.Discover(ctx, discovery.Options{Target: known.IP, Timeout: s.cfg.ScanTimeout})
				if directErr == nil {
					devices = append(devices, direct...)
				}
			}
		}
	}
	out := make([]ScanResult, 0, len(devices))
	for _, d := range devices {
		if d.Type != 172 || d.Protocol != 3 {
			continue
		}
		r := ScanResult{ID: strconv.FormatUint(d.ID, 10), IP: d.IP, Port: d.Port, Type: d.Type, Model: d.Model, Protocol: d.Protocol}
		if stored, e := s.cfg.Store.GetDevice(ctx, r.ID); e == nil {
			r.Registered = stored.LocalKey != ""
			r.Name = stored.Name
		}
		out = append(out, r)
	}
	return out, nil
}

// Register discovers the appliance on the LAN, obtains its per-device token
// and key from SmartHome, verifies them over the LAN, and stores them.
func (s *Service) Register(ctx context.Context, id, name, ip string) (db.Device, error) {
	if s.cloud == nil {
		return db.Device{}, ErrCloudNotConfigured
	}
	applianceID, err := strconv.ParseUint(id, 10, 64)
	if err != nil {
		return db.Device{}, fmt.Errorf("midea: invalid appliance id: %w", err)
	}
	var found *discovery.Device
	devices, err := discovery.Discover(ctx, discovery.Options{Target: ip, Timeout: s.cfg.ScanTimeout})
	if err != nil {
		return db.Device{}, err
	}
	for i := range devices {
		if devices[i].ID == applianceID && devices[i].Type == 172 && devices[i].Protocol == 3 {
			found = &devices[i]
			break
		}
	}
	if found == nil {
		return db.Device{}, errors.New("midea: appliance not found on LAN as a V3 air conditioner")
	}
	if err := s.cloud.Login(ctx); err != nil {
		return db.Device{}, fmt.Errorf("midea: SmartHome login: %w", err)
	}
	var credential cloud.Credential
	got := false
	for _, method := range []cloud.UDPPIDMethod{cloud.UDPPIDBig, cloud.UDPPIDLittle, cloud.UDPPIDReversedBig} {
		credential, got, err = s.cloud.GetToken(ctx, applianceID, method)
		if err != nil {
			return db.Device{}, err
		}
		if got && s.verify(ctx, *found, credential) == nil {
			break
		}
		got = false
	}
	if !got {
		return db.Device{}, errors.New("midea: SmartHome did not return a LAN credential accepted by the appliance")
	}
	if name == "" {
		name = fmt.Sprintf("Ar-condicionado Midea %s", id)
	}
	d := db.Device{ID: id, Kind: db.KindMidea, Name: name, IP: found.IP, Port: found.Port, Version: "3", ProductKey: found.Model, LocalKey: credential.Key, Token: credential.Token, KeySource: "midea_smarthome"}
	if err := s.cfg.Store.UpsertDevice(ctx, d); err != nil {
		return db.Device{}, err
	}
	// Cache the capabilities and initial state now, so the UI and automations
	// can work from the local database after registration.
	if _, err := s.RefreshSpecs(ctx, id); err != nil {
		return db.Device{}, fmt.Errorf("midea: retrieve appliance capabilities: %w", err)
	}
	if _, err := s.State(ctx, id); err != nil {
		return db.Device{}, fmt.Errorf("midea: read initial appliance state: %w", err)
	}
	return s.cfg.Store.GetDevice(ctx, id)
}

func (s *Service) Specs(ctx context.Context, id string) ([]db.MideaSpec, error) {
	return s.cfg.Store.ListMideaSpecs(ctx, id)
}

func (s *Service) RefreshSpecs(ctx context.Context, id string) ([]db.MideaSpec, error) {
	caps, err := s.Capabilities(ctx, id)
	if err != nil {
		return nil, err
	}
	specs := specsFromCapabilities(id, caps)
	if err := s.cfg.Store.ReplaceMideaSpecs(ctx, id, specs); err != nil {
		return nil, err
	}
	return specs, nil
}

func specsFromCapabilities(deviceID string, caps lan.Capabilities) []db.MideaSpec {
	makeSpec := func(key, name, kind string, values any) db.MideaSpec {
		raw, _ := json.Marshal(values)
		return db.MideaSpec{DeviceID: deviceID, ID: key, Name: name, Type: kind, Values: raw, Writable: true}
	}
	out := []db.MideaSpec{
		makeSpec("power", "Energia", "boolean", nil),
		makeSpec("mode", "Modo", "enum", []string{"auto", "cool", "dry", "heat", "fan"}),
		makeSpec("target_temp", "Temperatura", "integer", map[string]int{"min": 16, "max": 30, "step": 1}),
		makeSpec("fan_speed", "Velocidade do ventilador", "enum", []string{"auto", "silent", "low", "medium", "high", "full"}),
		makeSpec("swing_v", "Fluxo de ar vertical", "boolean", nil), makeSpec("swing_h", "Fluxo de ar horizontal", "boolean", nil),
		makeSpec("turbo", "Boost", "boolean", nil), makeSpec("eco", "Eco", "boolean", nil), makeSpec("display", "LED do aparelho", "boolean", nil), makeSpec("sleep", "Sono", "boolean", nil),
	}
	if min, max := tempRange(caps); min > 0 && max > min {
		out[2] = makeSpec("target_temp", "Temperatura", "integer", map[string]int{"min": min, "max": max, "step": 1})
	}
	if caps.Breeze {
		out = append(out, makeSpec("breeze", "Breeze Away", "enum", []string{"off", "away", "mild", "breezeless"}))
	}
	if caps.SwingAngle {
		out = append(out, makeSpec("airflow_angle", "Posição do fluxo de ar", "enum", []int{0, 1, 25, 50, 75, 100}))
	}
	if caps.Purifier {
		out = append(out, makeSpec("purifier", "Purificador", "boolean", nil))
	}
	if caps.FreshAir {
		out = append(out, makeSpec("fresh_air", "Ar externo", "enum", []string{"off", "low", "medium", "high", "boost"}))
	}
	if caps.OutSilent {
		out = append(out, makeSpec("out_silent", "Unidade externa silenciosa", "boolean", nil))
	}
	if _, ok := caps.Raw[0x0039]; ok {
		out = append(out, makeSpec("clean", "Autolimpeza", "boolean", nil))
	}
	return out
}

// tempRange returns the temperature limits the appliance advertises, or zero
// values when it did not report them. The narrower of cool/heat wins so the UI
// never offers a setpoint the appliance refuses.
func tempRange(caps lan.Capabilities) (int, int) {
	low, high := 0, 0
	for _, r := range [][2]float64{{caps.MinCoolTemp, caps.MaxCoolTemp}, {caps.MinHeatTemp, caps.MaxHeatTemp}} {
		if r[0] <= 0 || r[1] <= r[0] {
			continue
		}
		if low == 0 || int(r[0]) > low {
			low = int(r[0])
		}
		if high == 0 || int(r[1]) < high {
			high = int(r[1])
		}
	}
	if low < 16 {
		low = 16
	}
	if high > 30 {
		high = 30
	}
	return low, high
}

func (s *Service) verify(ctx context.Context, d discovery.Device, c cloud.Credential) error {
	token, e := hex.DecodeString(c.Token)
	if e != nil {
		return e
	}
	key, e := hex.DecodeString(c.Key)
	if e != nil {
		return e
	}
	client, e := lan.NewClient(net.JoinHostPort(d.IP, strconv.Itoa(d.Port)), token, key, d.ID, lan.DefaultOptions())
	if e != nil {
		return e
	}
	defer client.Close()
	if e = client.Connect(ctx); e != nil {
		return e
	}
	_, e = client.Poll(ctx)
	return e
}

func (s *Service) resolve(ctx context.Context, id string) (db.Device, discovery.Device, error) {
	d, err := s.cfg.Store.GetDevice(ctx, id)
	if err != nil {
		return d, discovery.Device{}, err
	}
	if d.Kind != db.KindMidea {
		return d, discovery.Device{}, errors.New("midea: device has a different kind")
	}
	deviceID, err := strconv.ParseUint(id, 10, 64)
	if err != nil {
		return d, discovery.Device{}, err
	}
	if d.IP != "" {
		port := d.Port
		if port == 0 {
			port = 6444
		}
		return d, discovery.Device{ID: deviceID, IP: d.IP, Port: port, Type: 172, Model: d.ProductKey, Protocol: 3}, nil
	}
	return d, discovery.Device{}, errors.New("midea: device has no saved IP; scan the local network")
}

func (s *Service) rediscover(ctx context.Context, id string) (db.Device, discovery.Device, error) {
	deviceID, err := strconv.ParseUint(id, 10, 64)
	if err != nil {
		return db.Device{}, discovery.Device{}, err
	}
	found, err := discovery.Discover(ctx, discovery.Options{Timeout: s.cfg.ScanTimeout})
	if err != nil {
		return db.Device{}, discovery.Device{}, err
	}
	for _, candidate := range found {
		if candidate.ID == deviceID {
			if err := s.cfg.Store.SetDeviceIP(ctx, id, candidate.IP); err != nil {
				return db.Device{}, discovery.Device{}, err
			}
			d, err := s.cfg.Store.GetDevice(ctx, id)
			if err != nil {
				return db.Device{}, discovery.Device{}, err
			}
			d.Port = candidate.Port
			s.invalidateClient(id)
			return d, candidate, nil
		}
	}
	return db.Device{}, discovery.Device{}, errors.New("midea: appliance not found on local network")
}

func (s *Service) State(ctx context.Context, id string) (lan.Response, error) {
	d, found, err := s.resolve(ctx, id)
	if err != nil {
		return lan.Response{}, err
	}
	c, err := s.client(d, found.ID)
	if err != nil {
		return lan.Response{}, err
	}
	if err = c.Connect(ctx); err == nil {
		if state, pollErr := c.Poll(ctx); pollErr == nil {
			if saveErr := s.persistState(ctx, id, state, nil); saveErr != nil {
				return lan.Response{}, saveErr
			}
			return state, nil
		} else {
			err = pollErr
		}
	}
	if _, _, refreshErr := s.rediscover(ctx, id); refreshErr != nil {
		return lan.Response{}, err
	}
	d, found, err = s.resolve(ctx, id)
	if err != nil {
		return lan.Response{}, err
	}
	c, err = s.client(d, found.ID)
	if err != nil {
		return lan.Response{}, err
	}
	if err = c.Connect(ctx); err != nil {
		return lan.Response{}, err
	}
	state, err := c.Poll(ctx)
	if err != nil {
		return lan.Response{}, err
	}
	if err = s.persistState(ctx, id, state, nil); err != nil {
		return lan.Response{}, err
	}
	return state, nil
}

func (s *Service) Control(ctx context.Context, id string, req lan.Request, props lan.PropertiesRequest) (lan.Response, error) {
	d, found, err := s.resolve(ctx, id)
	if err != nil {
		return lan.Response{}, err
	}
	c, err := s.client(d, found.ID)
	if err != nil {
		return lan.Response{}, err
	}
	if err = c.Connect(ctx); err != nil {
		if _, _, refreshErr := s.rediscover(ctx, id); refreshErr != nil {
			return lan.Response{}, err
		}
		d, found, err = s.resolve(ctx, id)
		if err != nil {
			return lan.Response{}, err
		}
		c, err = s.client(d, found.ID)
		if err != nil {
			return lan.Response{}, err
		}
		if err = c.Connect(ctx); err != nil {
			return lan.Response{}, err
		}
	}
	state, err := c.Update(ctx, req)
	if err != nil {
		return lan.Response{}, err
	}
	if hasProperties(props) {
		if err = c.SetProperties(ctx, props); err != nil {
			return lan.Response{}, err
		}
		state, err = c.Poll(ctx)
	}
	if err == nil {
		err = s.persistState(ctx, id, state, propertyState(props))
	}
	return state, err
}

// Apply translates the logical state keys used by the API, automations and
// voice into the Midea LAN request and writes it. Unknown keys are ignored, so
// a caller can send a partial state.
func (s *Service) Apply(ctx context.Context, id string, state map[string]any) (map[string]any, error) {
	req, props := requestFor(state)
	resp, err := s.Control(ctx, id, req, props)
	if err != nil {
		return nil, err
	}
	out := StateMap(resp)
	for key, value := range propertyState(props) {
		out[key] = value
	}
	return out, nil
}

// requestFor translates the logical state keys used by the API, voice and
// automations into a Midea LAN request. Unknown keys are ignored.
func requestFor(state map[string]any) (lan.Request, lan.PropertiesRequest) {
	var req lan.Request
	var props lan.PropertiesRequest
	for key, value := range state {
		switch key {
		case "power":
			if v, ok := asBool(value); ok {
				req.Power = &v
			}
		case "mode":
			if v, ok := asMode(value); ok {
				req.Mode = &v
			}
		case "target_temp":
			if v, ok := asFloat(value); ok {
				v = math.Round(v)
				if v < 16 {
					v = 16
				}
				if v > 30 {
					v = 30
				}
				req.TargetTemp = &v
			}
		case "fan_speed":
			if v, ok := asFanSpeed(value); ok {
				req.FanSpeed = &v
			}
		case "swing_v":
			if v, ok := asBool(value); ok {
				req.SwingV = &v
			}
		case "swing_h":
			if v, ok := asBool(value); ok {
				req.SwingH = &v
			}
		case "eco":
			if v, ok := asBool(value); ok {
				req.Eco = &v
			}
		case "turbo":
			if v, ok := asBool(value); ok {
				req.Turbo = &v
			}
		case "sleep":
			if v, ok := asBool(value); ok {
				req.Sleep = &v
			}
		case "display":
			if v, ok := asBool(value); ok {
				req.Display = &v
			}
		case "purifier":
			if v, ok := asBool(value); ok {
				req.Purifier = &v
			}
		case "breeze":
			if v, ok := asBreeze(value); ok {
				props.Breeze = &v
			}
		case "airflow_angle":
			if v, ok := asAngle(value); ok {
				props.SwingVAngle = &v
			}
		case "fresh_air":
			if v, ok := asFreshAir(value); ok {
				props.FreshAir = &v
			}
		case "out_silent":
			if v, ok := asBool(value); ok {
				props.OutSilent = &v
			}
		case "clean":
			if v, ok := asBool(value); ok {
				props.SelfClean = &v
			}

		}
	}
	return req, props
}

// StateMap converts a decoded LAN response into the logical state keys shared
// by the API, SSE and the UI.
func StateMap(state lan.Response) map[string]any {
	return map[string]any{"power": state.Power, "mode": state.Mode.String(), "target_temp": state.TargetTemp, "indoor_temp": state.IndoorTemp, "outdoor_temp": state.OutdoorTemp, "fan_speed": state.FanSpeed.String(), "swing_v": state.SwingV, "swing_h": state.SwingH, "eco": state.Eco, "turbo": state.Turbo, "display": state.DisplayOn, "sleep": state.Sleep, "purifier": state.Purifier, "aux_heat": state.AuxHeat, "frost_protect": state.FrostProtect, "comfort_mode": state.ComfortMode, "follow_me": state.FollowMe, "humidity": state.Humidity, "target_humidity": state.TargetHumidity}
}

// propertyState echoes the advanced properties a command set, because the
// appliance does not report all of them in the 0xC0 state frame.
func propertyState(props lan.PropertiesRequest) map[string]any {
	out := map[string]any{}
	if props.Breeze != nil {
		out["breeze"] = props.Breeze.String()
	}
	if props.SwingVAngle != nil {
		out["airflow_angle"] = int(*props.SwingVAngle)
	}
	if props.FreshAir != nil {
		out["fresh_air"] = props.FreshAir.String()
	}
	if props.OutSilent != nil {
		out["out_silent"] = *props.OutSilent
	}
	if props.SelfClean != nil {
		out["clean"] = *props.SelfClean
	}
	return out
}

func asBool(value any) (bool, bool) {
	switch v := value.(type) {
	case bool:
		return v, true
	case float64:
		return v != 0, true
	default:
		return false, false
	}
}

func asFloat(value any) (float64, bool) {
	switch v := value.(type) {
	case float64:
		return v, true
	case int:
		return float64(v), true
	case string:
		parsed, err := strconv.ParseFloat(v, 64)
		return parsed, err == nil
	default:
		return 0, false
	}
}

func asMode(value any) (lan.Mode, bool) {
	if name, ok := value.(string); ok {
		modes := map[string]lan.Mode{"auto": lan.ModeAuto, "cool": lan.ModeCool, "dry": lan.ModeDry, "heat": lan.ModeHeat, "fan": lan.ModeFan}
		if mode, ok := modes[name]; ok {
			return mode, true
		}
		return 0, false
	}
	if n, ok := asFloat(value); ok && n >= 1 && n <= 5 {
		return lan.Mode(n), true
	}
	return 0, false
}

func asFanSpeed(value any) (lan.FanSpeed, bool) {
	if name, ok := value.(string); ok {
		speeds := map[string]lan.FanSpeed{"silent": lan.FanSilent, "low": lan.FanLow, "medium": lan.FanMedium, "high": lan.FanHigh, "full": lan.FanFull, "auto": lan.FanAuto}
		if speed, ok := speeds[name]; ok {
			return speed, true
		}
		return 0, false
	}
	if n, ok := asFloat(value); ok {
		return lan.FanSpeed(n), true
	}
	return 0, false
}

func asBreeze(value any) (lan.BreezeMode, bool) {
	modes := map[string]lan.BreezeMode{"off": lan.BreezeOff, "away": lan.BreezeAway, "mild": lan.BreezeMild, "breezeless": lan.BreezeBreezeless}
	if name, ok := value.(string); ok {
		if mode, ok := modes[name]; ok {
			return mode, true
		}
		return 0, false
	}
	if n, ok := asFloat(value); ok && n >= 0 && n <= 3 {
		return lan.BreezeMode(n), true
	}
	return 0, false
}

func asFreshAir(value any) (lan.FreshAirSpeed, bool) {
	speeds := map[string]lan.FreshAirSpeed{"off": lan.FreshAirOff, "low": lan.FreshAirLow, "medium": lan.FreshAirMedium, "high": lan.FreshAirHigh, "boost": lan.FreshAirBoost}
	if name, ok := value.(string); ok {
		if speed, ok := speeds[name]; ok {
			return speed, true
		}
		return 0, false
	}
	if n, ok := asFloat(value); ok {
		return lan.FreshAirSpeed(n), true
	}
	return 0, false
}

func asAngle(value any) (lan.SwingAngle, bool) {
	if n, ok := asFloat(value); ok {
		angle := lan.SwingAngle(n)
		switch angle {
		case lan.SwingAngleOff, lan.SwingAnglePos1, lan.SwingAnglePos2, lan.SwingAnglePos3, lan.SwingAnglePos4, lan.SwingAnglePos5:
			return angle, true
		}
	}
	return 0, false
}

func (s *Service) persistState(ctx context.Context, id string, state lan.Response, extra map[string]any) error {
	values := StateMap(state)
	for key, value := range extra {
		values[key] = value
	}
	if err := s.cfg.Store.SetMideaStates(ctx, id, values); err != nil {
		return err
	}
	s.markSeen(id)
	return nil
}

func hasProperties(p lan.PropertiesRequest) bool {
	return p.SwingVAngle != nil || p.SwingHAngle != nil || p.Breeze != nil || p.FreshAir != nil || p.OutSilent != nil || p.SelfClean != nil
}

func (s *Service) Capabilities(ctx context.Context, id string) (lan.Capabilities, error) {
	d, found, err := s.resolve(ctx, id)
	if err != nil {
		return lan.Capabilities{}, err
	}
	c, err := s.client(d, found.ID)
	if err != nil {
		return lan.Capabilities{}, err
	}
	if err = c.Connect(ctx); err != nil {
		return lan.Capabilities{}, err
	}
	return c.Capabilities(ctx)
}

func (s *Service) client(d db.Device, id uint64) (*lan.Client, error) {
	port := d.Port
	if port == 0 {
		port = 6444
	}
	addr := net.JoinHostPort(d.IP, strconv.Itoa(port))
	s.mu.Lock()
	defer s.mu.Unlock()
	if existing, ok := s.clients[d.ID]; ok && existing.addr == addr {
		return existing.client, nil
	}
	if existing, ok := s.clients[d.ID]; ok {
		_ = existing.client.Close()
		delete(s.clients, d.ID)
	}
	token, err := hex.DecodeString(d.Token)
	if err != nil {
		return nil, fmt.Errorf("midea: invalid stored token: %w", err)
	}
	key, err := hex.DecodeString(d.LocalKey)
	if err != nil {
		return nil, fmt.Errorf("midea: invalid stored key: %w", err)
	}
	opts := lan.DefaultOptions()
	opts.PostAuthDelay = 250 * time.Millisecond
	opts.CmdMaxAttempts = 1
	opts.CmdAttemptTimeout = 900 * time.Millisecond
	opts.ReadTimeout = 2 * time.Second
	c, err := lan.NewClient(addr, token, key, id, opts)
	if err != nil {
		return nil, err
	}
	s.clients[d.ID] = cachedClient{addr: addr, client: c}
	go s.listenStatus(d.ID, c.StatusUpdates())
	return c, nil
}

func (s *Service) listenStatus(id string, updates <-chan lan.Response) {
	for {
		select {
		case <-s.done:
			return
		case state := <-updates:
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			before, _ := s.cfg.Store.MideaStates(ctx, id)
			err := s.persistState(ctx, id, state, nil)
			after, _ := s.cfg.Store.MideaStates(ctx, id)
			cancel()
			if err == nil {
				s.publishChange(id, before, after)
			}
		}
	}
}

// publishChange fans a state change out to the UI (SSE) and to the automation
// source. Values are published as they arrive, so the automation sees "off ->
// on" even though only one value was stored.
func (s *Service) publishChange(id string, before, after map[string]any) {
	if len(after) == 0 {
		return
	}
	changed := map[string]any{}
	for key, value := range after {
		if old, ok := before[key]; !ok || !reflect.DeepEqual(old, value) {
			changed[key] = value
		}
	}
	if len(changed) == 0 {
		return
	}
	if s.bus != nil {
		s.bus.PublishData("midea", map[string]any{"device": id, "state": changed})
	}
	if s.stateSource != nil {
		state := make(map[string]any, len(changed))
		for key, value := range changed {
			state[key] = value
		}
		s.stateSource.HandleMideaState(id, state)
	}
}

func (s *Service) invalidateClient(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if cached, ok := s.clients[id]; ok {
		_ = cached.client.Close()
		delete(s.clients, id)
	}
}

// RefreshIPs periodically rediscovers each registered device by its stable appliance ID.
func (s *Service) RefreshIPs(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		interval = time.Minute
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			devices, err := s.Devices(ctx)
			if err != nil {
				continue
			}
			found, err := discovery.Discover(ctx, discovery.Options{Timeout: s.cfg.ScanTimeout})
			if err != nil {
				continue
			}
			byID := make(map[uint64]discovery.Device, len(found))
			for _, device := range found {
				byID[device.ID] = device
			}
			for _, d := range devices {
				if ctx.Err() != nil {
					return
				}
				id, parseErr := strconv.ParseUint(d.ID, 10, 64)
				if parseErr != nil {
					continue
				}
				device, ok := byID[id]
				if !ok || device.Type != 172 || device.Protocol != 3 || device.IP == d.IP {
					continue
				}
				if s.cfg.Store.SetDeviceIP(ctx, d.ID, device.IP) == nil {
					s.invalidateClient(d.ID)
				}
			}
		}
	}
}

// PollStates is a slow recovery poll. Normal state changes arrive from the
// authenticated connection's unsolicited status stream.
func (s *Service) PollStates(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		interval = time.Minute
	}
	if s.cfg.StatePoll > 0 {
		interval = s.cfg.StatePoll
	}
	poll := func() {
		devices, err := s.Devices(ctx)
		if err != nil {
			return
		}
		for _, device := range devices {
			if ctx.Err() != nil {
				return
			}
			before, _ := s.cfg.Store.MideaStates(ctx, device.ID)
			if _, err := s.State(ctx, device.ID); err != nil {
				continue
			}
			after, _ := s.cfg.Store.MideaStates(ctx, device.ID)
			s.publishChange(device.ID, before, after)
		}
	}
	poll()
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			poll()
		}
	}
}
