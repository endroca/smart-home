// Package hub orchestrates LAN discovery, the commands sent to the devices,
// the state they push back, the durable replica of that state and OTA for the
// smart_home Raspberry Pi hub.
package hub

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math"
	"strings"
	"sync"
	"time"

	"smart_home/hub/internal/config"
	"smart_home/hub/internal/db"
	"smart_home/hub/internal/eventbus"
	"smart_home/hub/internal/fallback"
	"smart_home/hub/internal/model"
	"smart_home/hub/internal/ota"
	"smart_home/hub/internal/store"
)

// Version is the hub release string. It is a var so the deploy build can inject
// the git revision with -ldflags "-X smart_home/hub/internal/hub.Version=...".
var Version = "0.1.0"

// Error is a user-facing hub error with a stable code.
type Error struct {
	Code    string
	Message string
}

func (e *Error) Error() string { return e.Message }

func errf(code, format string, args ...any) error {
	return &Error{Code: code, Message: fmt.Sprintf(format, args...)}
}

// Hub is the application service used by the HTTP API and the device ingest.
type Hub struct {
	cfg      config.Config
	store    *store.Store
	fallback *fallback.Client
	ota      *ota.Manager
	started  time.Time

	// fans persists the sub-devices discovered on a gateway; it is optional and
	// set when the directory also implements FanRegistry.
	fans FanRegistry

	// states persists the last-known reported state of the sub-devices; it is
	// optional and set when the directory also implements StateStore.
	states StateStore

	// things lists the registered gateways, used by the LAN discovery.
	things ThingSource

	// bus, when set, receives inventory/state changes for the UI (SSE).
	bus *eventbus.Bus

	// manifestProbes throttles the manifest re-read triggered by a heartbeat.
	manifestProbeMu sync.Mutex
	manifestProbes  map[string]time.Time

	// hubURLs tracks the push URL already configured on each gateway (see
	// syncDeviceHubURL).
	hubURLMu sync.Mutex
	hubURLs  map[string]*hubURLSync

	// presence tracks the last time a gateway was heard from, so the watchdog
	// can mark a silent one offline (there is no broker Last Will over HTTP).
	presenceMu sync.Mutex
	presence   map[string]time.Time

	// learn tracks the in-progress/last RF learning session per gateway so the
	// UI can poll its status.
	learnMu sync.Mutex
	learn   map[string]*learnSession
}

// learnSession is the state of an RF learning session for one gateway.
type learnSession struct {
	DeviceID  string    `json:"device"`
	Command   string    `json:"command"`
	StartedAt time.Time `json:"startedAt"`
	Active    bool      `json:"active"`
	Result    string    `json:"result,omitempty"`
	Frame     string    `json:"frame,omitempty"`
	Error     string    `json:"error,omitempty"`
}

// FanRegistry registers the sub-devices discovered on a gateway and persists
// their room, so the association survives slot reindexing after fan.remove.
type FanRegistry interface {
	ListFansByOwner(ctx context.Context, owner string) ([]db.Device, error)
	UpsertFan(ctx context.Context, owner, deviceID, firmwareName string) error
	RenameFan(ctx context.Context, owner, deviceID, name string) error
	SetFanFirmwareName(ctx context.Context, owner, deviceID, name string) error
	DeleteFan(ctx context.Context, owner, deviceID string) error
	SetDeviceRoom(ctx context.Context, id, roomID string) error
}

// StateStore persists the last-known reported state and status of a gateway's
// sub-devices. The hub keeps the hot copy in memory; this is the durable
// replica that lets it render the inventory immediately after a restart.
type StateStore interface {
	SetThingState(ctx context.Context, thing, deviceID string, reported map[string]any, source string, deviceTS int64) error
	AllThingStates(ctx context.Context) ([]db.ThingState, error)
	DeleteThingState(ctx context.Context, thing, deviceID string) error
	DeleteThingStates(ctx context.Context, thing string) error
	SetThingStatus(ctx context.Context, thing string, status any, manifestVersion int) error
	AllThingStatuses(ctx context.Context) ([]db.ThingStatus, error)
	DeleteThingStatus(ctx context.Context, thing string) error
}

// New builds the hub. dir resolves the per-gateway LAN credentials (token and
// optional URL) and may be nil in tests; when it also implements FanRegistry,
// StateStore or ThingSource, the hub wires those in.
func New(cfg config.Config, dir fallback.Directory) (*Hub, error) {
	st := store.New()
	fb := fallback.New(st, dir)
	h := &Hub{
		cfg:      cfg,
		store:    st,
		fallback: fb,
		started:  time.Now(),
		presence: map[string]time.Time{},
		learn:    map[string]*learnSession{},
		hubURLs:  map[string]*hubURLSync{},
	}
	if fr, ok := dir.(FanRegistry); ok {
		h.fans = fr
	}
	if ss, ok := dir.(StateStore); ok {
		h.states = ss
	}
	if ts, ok := dir.(ThingSource); ok {
		h.things = ts
	}
	h.ota = ota.New(cfg, st, fb)
	return h, nil
}

// Start begins the OTA deadline loop, the presence watchdog and the mDNS
// browse for gateways announcing themselves on the LAN.
func (h *Hub) Start(ctx context.Context) {
	go h.ota.Loop(ctx)
	h.StartPresenceLoop(ctx)
	h.StartMDNSDiscovery(ctx)
}

// LoadState seeds the in-memory inventory from the durable replica so the UI
// can render the last-known state immediately after a restart, before the LAN
// probe answers. The seeded state is marked stale until the device confirms it
// (a fresh report clears the flag).
func (h *Hub) LoadState(ctx context.Context) error {
	if h.states == nil {
		return nil
	}
	rows, err := h.states.AllThingStates(ctx)
	if err != nil {
		return err
	}
	loaded := 0
	for _, row := range rows {
		if len(row.Reported) == 0 {
			continue
		}
		h.store.SetReported(row.Thing, row.DeviceID, row.Reported)
		h.store.MarkStale(row.Thing, row.DeviceID)
		loaded++
	}

	statuses, err := h.states.AllThingStatuses(ctx)
	if err != nil {
		return err
	}
	for _, row := range statuses {
		if len(row.Status) == 0 {
			continue
		}
		var st model.Status
		if err := remarshal(row.Status, &st); err != nil {
			continue
		}
		// Never trust presence across a restart: the gateway is offline until
		// it is heard from again.
		st.Online = false
		st.Thing = row.Thing
		h.store.UpsertStatus(row.Thing, &st)
	}

	if loaded > 0 {
		log.Printf("[hub] loaded %d cached device states", loaded)
	}
	return nil
}

// remarshal converts a decoded JSON object into a typed value through JSON, so
// numbers keep their JSON shape (float64) and unknown fields are dropped.
func remarshal(in map[string]any, out any) error {
	raw, err := json.Marshal(in)
	if err != nil {
		return err
	}
	return json.Unmarshal(raw, out)
}

// statusPersistWorthy reports whether a status update carries information worth
// writing to the durable replica. A bare heartbeat (same address, firmware and
// OTA state) is skipped, so a device beating every 30 s does not wear the SD
// card for nothing.
func statusPersistWorthy(prev, next *model.Status) bool {
	if next == nil {
		return false
	}
	if prev == nil {
		return true
	}
	if prev.Online != next.Online {
		// Only transitions are persisted; a steady online flag is not news.
		return true
	}
	if prev.IP != next.IP || prev.Version != next.Version {
		return true
	}
	switch {
	case prev.OTA == nil && next.OTA == nil:
		return false
	case prev.OTA == nil || next.OTA == nil:
		return true
	default:
		return prev.OTA.State != next.OTA.State || prev.OTA.Version != next.OTA.Version
	}
}

// persistStatus writes the durable replica of a gateway's status when it
// carries something new. Best-effort, like persistReported.
func (h *Hub) persistStatus(thing string, prev, next *model.Status) {
	if h.states == nil || !statusPersistWorthy(prev, next) {
		return
	}
	manifestVersion := 0
	if t, ok := h.store.Get(thing); ok && t.Manifest != nil {
		manifestVersion = t.Manifest.ManifestVersion
	}
	if err := h.states.SetThingStatus(context.Background(), thing, next, manifestVersion); err != nil {
		log.Printf("[hub] persist status %s: %v", thing, err)
	}
}

// persistReported writes the durable replica of a sub-device's reported state.
// The in-memory store is the hot cache; this is what survives a restart. It is
// best-effort: a database failure must not break the live path.
func (h *Hub) persistReported(thing, deviceID string, reported map[string]any) {
	if h.states == nil || len(reported) == 0 {
		return
	}
	source, _ := model.AsString(reported["source"])
	deviceTS, _ := model.AsInt(reported["updatedAt"])
	if err := h.states.SetThingState(context.Background(), thing, deviceID, reported, source, deviceTS); err != nil {
		log.Printf("[hub] persist state %s/%s: %v", thing, deviceID, err)
	}
}

// forgetState drops the cached state of one sub-device.
func (h *Hub) forgetState(thing, deviceID string) {
	if h.states == nil {
		return
	}
	if err := h.states.DeleteThingState(context.Background(), thing, deviceID); err != nil {
		log.Printf("[hub] delete state %s/%s: %v", thing, deviceID, err)
	}
}

// forgetThingState drops every cached state of a gateway, including its status.
func (h *Hub) forgetThingState(thing string) {
	if h.states == nil {
		return
	}
	if err := h.states.DeleteThingStates(context.Background(), thing); err != nil {
		log.Printf("[hub] delete states %s: %v", thing, err)
	}
	if err := h.states.DeleteThingStatus(context.Background(), thing); err != nil {
		log.Printf("[hub] delete status %s: %v", thing, err)
	}
}

// SetEventBus attaches the event bus used to push inventory/state changes to
// the UI over SSE.
func (h *Hub) SetEventBus(bus *eventbus.Bus) { h.bus = bus }

// Snapshot returns the inventory for the UI.
func (h *Hub) Snapshot() []*model.Thing { return h.store.Snapshot() }

// Thing returns a deep copy of one thing.
func (h *Hub) Thing(id string) (*model.Thing, bool) { return h.store.Get(id) }

// FirmwareList lists available .bin files.
func (h *Hub) FirmwareList() ([]ota.File, error) { return h.ota.List() }

// FirmwarePath resolves a safe path for a served .bin file.
func (h *Hub) FirmwarePath(name string) (string, error) { return h.ota.FilePath(name) }

// SaveFirmware stores an uploaded .bin in the firmware directory.
func (h *Hub) SaveFirmware(name string, r io.Reader) ([]ota.File, error) { return h.ota.Save(name, r) }

// DeleteFirmware removes a .bin from the firmware directory.
func (h *Hub) DeleteFirmware(name string) ([]ota.File, error) { return h.ota.Delete(name) }

// PendingOTA returns in-flight updates.
func (h *Hub) PendingOTA() map[string]ota.Pending { return h.ota.Pending() }

// InstallFirmware starts an OTA for a thing.
func (h *Hub) InstallFirmware(ctx context.Context, thing, file string) (*ota.Pending, error) {
	return h.ota.Install(ctx, thing, file)
}

// Health returns a small status object for the UI.
func (h *Hub) Health() map[string]any {
	return map[string]any{
		"ok":         true,
		"version":    Version,
		"things":     len(h.store.Snapshot()),
		"uptime":     int64(time.Since(h.started).Seconds()),
		"pendingOTA": h.ota.Pending(),
	}
}

// SetState applies exactly one absolute state field over the LAN.
//
// A fan accepts only one RF command at a time: speed (0 disables, 1..6 selects
// a level), light, direction or the native "turn everything off" command. force
// re-sends the command even when the device believes the state is already
// correct (an explicit tap), without changing which field is sent.
func (h *Hub) SetState(ctx context.Context, thing, deviceID string, state map[string]any, force bool) (map[string]any, error) {
	normalized, _, _, err := h.validateState(thing, deviceID, state)
	if err != nil {
		return nil, err
	}
	if len(normalized) != 1 {
		return nil, &Error{Code: "invalid_state", Message: "a fan accepts one command at a time"}
	}
	return h.applyStateLAN(ctx, thing, deviceID, normalized, force)
}

// applyStateLAN writes the absolute state over the LAN HTTP API and mirrors the
// state the device reports back as the new truth.
func (h *Hub) applyStateLAN(ctx context.Context, thing, deviceID string, state map[string]any, force bool) (map[string]any, error) {
	resp, st, err := h.fallback.ApplyState(ctx, thing, deviceID, state, force)
	if err != nil {
		return nil, err
	}
	reported := map[string]any(nil)
	if st != nil {
		reported = st.Reported
	} else if raw, ok := resp["state"].(map[string]any); ok {
		reported = raw
	}
	if reported != nil {
		h.applyReported(thing, deviceID, reported)
	}
	resp["mode"] = "http"
	return resp, nil
}

// Correct corrects the known state of a sub-device without transmitting RF.
func (h *Hub) Correct(ctx context.Context, thing, deviceID string, state map[string]any) (map[string]any, error) {
	normalized, _, _, err := h.validateState(thing, deviceID, state)
	if err != nil {
		return nil, err
	}
	if !h.fallback.Available(thing) {
		return nil, &Error{Code: "fallback_unavailable", Message: "no LAN token/URL for this gateway"}
	}
	resp, st, err := h.fallback.Correct(ctx, thing, deviceID, normalized)
	if err != nil {
		return nil, errf("fallback_failed", "LAN correction failed: %v", err)
	}
	if st != nil && len(st.Reported) > 0 {
		h.applyReported(thing, deviceID, st.Reported)
	}
	return resp, nil
}

// Command validates and executes a one-shot command on the device over the LAN.
func (h *Hub) Command(ctx context.Context, thing, action string, fields map[string]any) (map[string]any, error) {
	deviceID, _ := fields["id"].(string)
	if err := h.validateAction(thing, deviceID, action); err != nil {
		return nil, err
	}
	// A rename is a hub-side display name: it must not travel to the device,
	// whose name is independent and only changes from its own UI.
	if action == "fan.rename" {
		name, _ := fields["name"].(string)
		name = strings.TrimSpace(name)
		if name == "" {
			return nil, errf("invalid_payload", "name is required")
		}
		if h.fans == nil {
			return nil, errf("unavailable", "fan registry is not configured")
		}
		if err := h.fans.RenameFan(ctx, thing, deviceID, name); err != nil {
			return nil, errf("store_failed", "rename fan: %v", err)
		}
		if h.bus != nil {
			h.bus.PublishData("fan", map[string]any{"thing": thing, "id": deviceID})
		}
		return map[string]any{"id": newID(), "ok": true}, nil
	}
	out, err := h.fallback.Command(ctx, thing, action, fields)
	if err != nil {
		return nil, errf("fallback_failed", "LAN command failed: %v", err)
	}
	if action == "fan.remove" && deviceID != "" {
		h.store.DeleteState(thing, deviceID)
		h.forgetState(thing, deviceID)
	}
	return map[string]any{"id": newID(), "ok": true, "result": out}, nil
}

// SendCommand executes a command and returns it with an id. It exists for the
// OTA manager, which shares the same LAN path.
func (h *Hub) SendCommand(ctx context.Context, thing, action string, fields map[string]any) (string, map[string]any, error) {
	ack, err := h.Command(ctx, thing, action, fields)
	if err != nil {
		return "", nil, err
	}
	id, _ := ack["id"].(string)
	return id, ack, nil
}

// Learn starts an RF learning session and polls it to completion.
func (h *Hub) Learn(ctx context.Context, thing, deviceID, command string) (map[string]any, error) {
	if err := h.validateAction(thing, deviceID, "learn.start"); err != nil {
		return nil, err
	}
	if !h.fallback.Available(thing) {
		return nil, &Error{Code: "fallback_unavailable", Message: "no LAN token/URL for this gateway"}
	}
	h.beginLearn(thing, deviceID, command)
	res, err := h.fallback.Learn(ctx, thing, deviceID, command)
	if err != nil {
		h.endLearn(thing, "error", "", err.Error())
		return nil, errf("fallback_failed", "LAN learn failed: %v", err)
	}
	// The LAN path returns the final status, so finish here. The device also
	// pushes learn.finished, which ends the session again harmlessly.
	if r, _ := res["result"].(string); r != "" && r != "listening" && r != "idle" {
		frame, _ := res["frame"].(string)
		h.endLearn(thing, r, frame, "")
	}
	return res, nil
}

// LearnCancel cancels the current learning session.
func (h *Hub) LearnCancel(ctx context.Context, thing string) (map[string]any, error) {
	res, err := h.Command(ctx, thing, "learn.cancel", map[string]any{})
	if err != nil {
		return res, err
	}
	h.endLearn(thing, "cancelled", "", "")
	return res, nil
}

func (h *Hub) beginLearn(thing, deviceID, command string) {
	h.learnMu.Lock()
	h.learn[thing] = &learnSession{DeviceID: deviceID, Command: command, StartedAt: time.Now().UTC(), Active: true}
	h.learnMu.Unlock()
}

func (h *Hub) endLearn(thing, result, frame, errMsg string) {
	h.learnMu.Lock()
	s := h.learn[thing]
	if s == nil {
		s = &learnSession{}
		h.learn[thing] = s
	}
	s.Active = false
	if result != "" {
		s.Result = result
	}
	if frame != "" {
		s.Frame = frame
	}
	if errMsg != "" {
		s.Error = errMsg
	}
	h.learnMu.Unlock()
	if h.bus != nil {
		h.bus.PublishData("learn", map[string]any{"thing": thing})
	}
}

// LearnStatus returns the current or last learning session of a gateway.
func (h *Hub) LearnStatus(thing string) map[string]any {
	h.learnMu.Lock()
	defer h.learnMu.Unlock()
	s := h.learn[thing]
	if s == nil {
		return map[string]any{"active": false, "result": "idle"}
	}
	return map[string]any{
		"active":    s.Active,
		"device":    s.DeviceID,
		"command":   s.Command,
		"startedAt": s.StartedAt,
		"result":    s.Result,
		"frame":     s.Frame,
		"error":     s.Error,
	}
}

// Diag returns the device's local diagnostics over the LAN (RF counters, NVS,
// heap, uptime, hub link).
func (h *Hub) Diag(ctx context.Context, thing string) (map[string]any, error) {
	if !h.fallback.Available(thing) {
		return nil, &Error{Code: "fallback_unavailable", Message: "no LAN token/URL for this gateway"}
	}
	res, err := h.fallback.Diag(ctx, thing)
	if err != nil {
		return nil, &Error{Code: "fallback_failed", Message: err.Error()}
	}
	return res, nil
}

// ConfigGet fetches the device configuration.
func (h *Hub) ConfigGet(ctx context.Context, thing string) (map[string]any, error) {
	return h.Command(ctx, thing, "config.get", map[string]any{})
}

// ConfigSet replaces the device configuration.
func (h *Hub) ConfigSet(ctx context.Context, thing string, cfgMap map[string]any) (map[string]any, error) {
	return h.Command(ctx, thing, "config.set", map[string]any{"config": cfgMap})
}

// Reboot restarts a device.
func (h *Hub) Reboot(ctx context.Context, thing string) (map[string]any, error) {
	return h.Command(ctx, thing, "reboot", map[string]any{})
}

// isOffline reports whether the gateway is currently considered unreachable.
func (h *Hub) isOffline(thing string) bool {
	t, ok := h.store.Get(thing)
	return ok && t.Status != nil && !t.Status.Online
}

func (h *Hub) validateAction(thing, deviceID, action string) error {
	t, ok := h.store.Get(thing)
	if !ok {
		return &Error{Code: "unknown_thing", Message: "thing not found"}
	}
	if t.Manifest == nil {
		return &Error{Code: "unknown_thing", Message: "manifest not received yet"}
	}
	needsDevice := action == "fan.remove" || action == "fan.rename" ||
		action == "state.correct" || action == "learn.start"
	if needsDevice && deviceID == "" {
		return &Error{Code: "invalid_payload", Message: "id is required for " + action}
	}
	if t.Manifest.HasAction(action) {
		return nil
	}
	if deviceID != "" {
		if d, ok := t.Device(deviceID); ok && d.HasAction(action) {
			return nil
		}
	}
	// learn.cancel and similar device actions are not tied to one device.
	for _, d := range t.Manifest.Devices {
		if d.HasAction(action) {
			return nil
		}
	}
	return &Error{Code: "not_supported", Message: "action " + action + " is not declared by the manifest"}
}

func (h *Hub) validateState(thing, deviceID string, state map[string]any) (map[string]any, *model.Device, *model.Thing, error) {
	if len(state) == 0 {
		return nil, nil, nil, &Error{Code: "invalid_state", Message: "state is empty"}
	}
	t, ok := h.store.Get(thing)
	if !ok {
		return nil, nil, nil, &Error{Code: "unknown_thing", Message: "thing not found"}
	}
	device, ok := t.Device(deviceID)
	if !ok {
		return nil, nil, nil, &Error{Code: "unknown_device", Message: "device not found in manifest"}
	}
	normalized := make(map[string]any, len(state))
	for name, raw := range state {
		cap, ok := device.Capabilities[name]
		if !ok {
			return nil, nil, nil, &Error{Code: "invalid_state", Message: "unknown capability " + name}
		}
		if !cap.IsWritable() {
			return nil, nil, nil, &Error{Code: "invalid_state", Message: "capability " + name + " is read-only"}
		}
		switch cap.Kind {
		case "switch":
			b, ok := model.AsBool(raw)
			if !ok {
				return nil, nil, nil, &Error{Code: "invalid_state", Message: name + " must be boolean"}
			}
			normalized[name] = b
		case "enum":
			s, ok := model.AsString(raw)
			if !ok {
				return nil, nil, nil, &Error{Code: "invalid_state", Message: name + " must be a string"}
			}
			if len(cap.Values) > 0 && !contains(cap.Values, s) {
				return nil, nil, nil, &Error{Code: "invalid_state", Message: name + " must be one of " + strings.Join(cap.Values, ", ")}
			}
			normalized[name] = s
		case "number":
			f, ok := model.AsFloat(raw)
			if !ok {
				return nil, nil, nil, &Error{Code: "invalid_state", Message: name + " must be numeric"}
			}
			if cap.Min != nil && f < *cap.Min {
				return nil, nil, nil, &Error{Code: "invalid_state", Message: fmt.Sprintf("%s must be >= %v", name, *cap.Min)}
			}
			if cap.Max != nil && f > *cap.Max {
				return nil, nil, nil, &Error{Code: "invalid_state", Message: fmt.Sprintf("%s must be <= %v", name, *cap.Max)}
			}
			if math.Trunc(f) == f {
				normalized[name] = int64(f)
			} else {
				normalized[name] = f
			}
		case "text":
			s, ok := model.AsString(raw)
			if !ok {
				return nil, nil, nil, &Error{Code: "invalid_state", Message: name + " must be a string"}
			}
			normalized[name] = s
		default:
			return nil, nil, nil, &Error{Code: "invalid_state", Message: "unsupported capability kind " + cap.Kind}
		}
	}
	return normalized, device, t, nil
}

func contains(values []string, want string) bool {
	for _, v := range values {
		if v == want {
			return true
		}
	}
	return false
}

func (h *Hub) onManifest(thing string, m *model.Manifest) {
	if old, ok := h.store.Get(thing); ok && old.Manifest != nil &&
		m.ManifestVersion < old.Manifest.ManifestVersion {
		log.Printf("[hub] ignoring older manifest %s v%d (current v%d)", thing, m.ManifestVersion, old.Manifest.ManifestVersion)
		return
	}
	removed, _ := h.store.UpsertManifest(thing, m)
	h.reconcileFans(thing, m)
	for _, id := range removed {
		h.forgetState(thing, id)
		h.store.DeleteState(thing, id)
	}
	h.ota.OnVersion(thing, m.Firmware.Version)
	log.Printf("[hub] manifest %s v%d (%d devices)", thing, m.ManifestVersion, len(m.Devices))
	// Notify the UI immediately: without this the browser only learns about a
	// freshly arrived manifest on its next 30s poll, so the sub-devices look
	// like they take forever to appear.
	if h.bus != nil {
		h.bus.PublishData("manifest", map[string]any{"thing": thing, "devices": len(m.Devices)})
	}
}

// manifestDevice finds a sub-device by id in a manifest.
func manifestDevice(m *model.Manifest, id string) (model.Device, bool) {
	for _, d := range m.Devices {
		if d.ID == id {
			return d, true
		}
	}
	return model.Device{}, false
}

// reconcileFans registers the sub-devices advertised by the manifest and
// removes the ones that disappeared.
//
// The device id (the stable ID) is the identity of a fan: a rename keeps the
// id, so the row and its room are kept. Only a fan whose id is gone (a
// fan.remove reindexed the slots) is matched by name, and only when the name
// is unique in the manifest. Matching by name first would collapse duplicate
// names (for example, renaming every fan to "Ventilador") and move the rooms
// to the wrong fan.
func (h *Hub) reconcileFans(thing string, m *model.Manifest) {
	if h.fans == nil || m == nil {
		return
	}
	ctx := context.Background()
	existing, err := h.fans.ListFansByOwner(ctx, thing)
	if err != nil {
		log.Printf("[hub] list fans %s: %v", thing, err)
		return
	}
	manifestIDs := make(map[string]bool, len(m.Devices))
	nameCount := make(map[string]int, len(m.Devices))
	for _, d := range m.Devices {
		manifestIDs[d.ID] = true
		nameCount[d.Name]++
	}
	nameToID := make(map[string]string, len(m.Devices))
	for _, d := range m.Devices {
		if nameCount[d.Name] == 1 {
			nameToID[d.Name] = d.ID
		}
	}
	used := map[string]bool{}
	for _, f := range existing {
		if manifestIDs[f.DeviceID] {
			// Same id: the row stays and its room is kept. The final pass
			// refreshes only the firmware name (rename on the device).
			continue
		}
		// The slot no longer exists. Match by name only when the name is
		// unique and the target slot is free; otherwise the fan is gone.
		target := nameToID[f.Name]
		if target == "" || used[target] {
			_ = h.fans.DeleteFan(ctx, thing, f.DeviceID)
			continue
		}
		used[target] = true
		room := f.RoomID
		_ = h.fans.DeleteFan(ctx, thing, f.DeviceID)
		// Keep the hub display name chosen for the old slot, but adopt the
		// firmware name the new slot advertises.
		_ = h.fans.UpsertFan(ctx, thing, target, f.Name)
		if d, ok := manifestDevice(m, target); ok {
			_ = h.fans.SetFanFirmwareName(ctx, thing, target, d.Name)
		}
		if room != "" {
			_ = h.fans.SetDeviceRoom(ctx, db.FanID(thing, target), room)
		}
	}
	for _, d := range m.Devices {
		if used[d.ID] {
			continue
		}
		used[d.ID] = true
		if err := h.fans.UpsertFan(ctx, thing, d.ID, d.Name); err != nil {
			log.Printf("[hub] register fan %s/%s: %v", thing, d.ID, err)
		}
	}
}

func (h *Hub) onStatus(thing string, st *model.Status) {
	prev, _ := h.store.Get(thing)
	var prevStatus *model.Status
	if prev != nil {
		prevStatus = prev.Status
	}
	h.store.UpsertStatus(thing, st)
	h.persistStatus(thing, prevStatus, st)
	h.ota.OnVersion(thing, st.Version)
	if h.bus != nil {
		h.bus.PublishData("status", map[string]any{"thing": thing, "status": st})
	}
}

// markSeen records that a gateway was heard from. Presence has no broker Last
// Will over HTTP, so the hub derives it from the heartbeat instead.
func (h *Hub) markSeen(thing string) {
	h.presenceMu.Lock()
	h.presence[thing] = time.Now()
	h.presenceMu.Unlock()
}

// presenceTTL is how long a silent gateway keeps its online flag.
const presenceTTL = 90 * time.Second

// StartPresenceLoop marks a gateway offline when it stops pushing. The last
// status keeps its diagnostics; only online flips.
func (h *Hub) StartPresenceLoop(ctx context.Context) {
	go func() {
		ticker := time.NewTicker(15 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				h.reapSilent()
			}
		}
	}()
}

func (h *Hub) reapSilent() {
	now := time.Now()
	var gone []string
	h.presenceMu.Lock()
	for thing, at := range h.presence {
		if now.Sub(at) > presenceTTL {
			gone = append(gone, thing)
		}
	}
	for _, thing := range gone {
		delete(h.presence, thing)
	}
	h.presenceMu.Unlock()

	for _, thing := range gone {
		if !h.store.SetOnline(thing, false) {
			continue
		}
		log.Printf("[hub] %s heartbeat stopped; marked offline", thing)
		if h.bus != nil {
			if t, ok := h.store.Get(thing); ok && t.Status != nil {
				h.bus.PublishData("status", map[string]any{"thing": thing, "status": t.Status})
			}
		}
	}
}

// IngestState applies a state reported by a device over the push channel. The
// device is the single source of truth, so this only refreshes what the hub
// mirrors; nothing the hub intends enters here.
func (h *Hub) IngestState(thing, deviceID string, reported map[string]any) error {
	if thing == "" || deviceID == "" {
		return errf("invalid_payload", "thing and id are required")
	}
	if len(reported) == 0 {
		return errf("invalid_payload", "reported state is empty")
	}
	h.markSeen(thing)
	h.applyReported(thing, deviceID, reported)
	return nil
}

// applyReported is the single writer of a sub-device's state: the hot cache,
// the durable replica and the UI notification all happen here.
func (h *Hub) applyReported(thing, deviceID string, reported map[string]any) {
	h.store.SetReported(thing, deviceID, reported)
	h.persistReported(thing, deviceID, reported)
	if h.bus != nil {
		h.bus.PublishData("state", map[string]any{
			"thing": thing, "id": deviceID, "reported": reported,
		})
	}
}

// IngestStatus applies a heartbeat/status push. Receiving it proves the device
// is reachable, so the online flag is forced rather than taken from the body.
func (h *Hub) IngestStatus(thing string, st *model.Status) error {
	if thing == "" {
		return errf("invalid_payload", "thing is required")
	}
	if st == nil {
		st = &model.Status{}
	}
	st.Thing = thing
	st.Online = true
	h.markSeen(thing)
	h.onStatus(thing, st)
	h.probeIfManifestChanged(thing, st.ManifestVersion)
	return nil
}

// probeIfManifestChanged re-reads the manifest when a heartbeat advertises a
// version the hub has not seen. This is what lets a device that reports a
// heartbeat drive the discovery, instead of the hub revalidating on a timer.
func (h *Hub) probeIfManifestChanged(thing string, version int64) {
	if version <= 0 || h.things == nil {
		return
	}
	if t, ok := h.store.Get(thing); ok && t.Manifest != nil &&
		int64(t.Manifest.ManifestVersion) == version {
		return
	}
	h.manifestProbeMu.Lock()
	if h.manifestProbes == nil {
		h.manifestProbes = map[string]time.Time{}
	}
	if last, ok := h.manifestProbes[thing]; ok && time.Since(last) < 30*time.Second {
		h.manifestProbeMu.Unlock()
		return
	}
	h.manifestProbes[thing] = time.Now()
	h.manifestProbeMu.Unlock()

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if err := h.ProbeThing(ctx, thing); err != nil {
			log.Printf("[hub] heartbeat probe %s: %v", thing, err)
		}
	}()
}

// IngestEvent applies an asynchronous event pushed by a device.
func (h *Hub) IngestEvent(thing string, event map[string]any) error {
	if thing == "" {
		return errf("invalid_payload", "thing is required")
	}
	if len(event) == 0 {
		return errf("invalid_payload", "event is empty")
	}
	h.markSeen(thing)
	h.onEvent(thing, event)
	return nil
}

func (h *Hub) onEvent(thing string, event map[string]any) {
	h.store.SetEvent(thing, event)
	if h.bus != nil {
		h.bus.PublishData("event", map[string]any{"thing": thing, "event": event})
	}
	name, _ := event["event"].(string)
	switch name {
	case "ota.done", "ota.progress":
		h.ota.OnEvent(thing, event)
	case "learn.finished":
		result := "error"
		if ok, _ := model.AsBool(event["ok"]); ok {
			result = "ok"
		}
		frame, _ := model.AsString(event["frame"])
		errMsg := ""
		if raw, ok := event["error"].(map[string]any); ok {
			if code, ok := model.AsString(raw["code"]); ok {
				errMsg = code
				if code == "cancelled" {
					result = "cancelled"
				}
			}
		}
		h.endLearn(thing, result, frame, errMsg)
		log.Printf("[hub] learn.finished %s (%s)", thing, result)
	case "state.failed":
		// Over HTTP the device answers the command synchronously, so this is
		// only a late failure report (for example the RF never got a frame).
		// There is no pending command to retry: just record it.
		log.Printf("[hub] state.failed %s: %v", thing, event["error"])
	}
}

func newID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return fmt.Sprintf("hub-%d", time.Now().UnixNano())
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	s := hex.EncodeToString(b[:])
	return s[0:8] + "-" + s[8:12] + "-" + s[12:16] + "-" + s[16:20] + "-" + s[20:]
}
