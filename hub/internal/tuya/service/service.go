// Package service ties the Tuya LAN client, the Tuya cloud client and the
// persistent registry together for the hub UI: it lists registered devices,
// scans the LAN, registers keys from the cloud and runs simple listeners.
package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"smart_home/hub/internal/db"
	"smart_home/hub/internal/eventbus"
	"smart_home/hub/internal/tuya"
)

// Errors returned by the service.
var (
	ErrCloudNotConfigured = errors.New("tuya: cloud credentials are not configured")
	ErrNoKey              = errors.New("tuya: device has no local key; register it first")
	ErrNoIP               = errors.New("tuya: device has no LAN address; use \"Escanear rede\" to locate it")
	ErrNoMatch            = errors.New("tuya: no matching cloud device")
	errNoListener         = errors.New("tuya: device is not being listened to")
)

// cloudOnlyCategories are Tuya product categories that sleep on battery and
// therefore cannot be reached reliably over the LAN. They default to
// cloud_only: no LAN listener, state polled from the cloud.
var cloudOnlyCategories = map[string]bool{
	"mk": true, "ms": true, "jtmsbh": true, "jtmspro": true,
	"gyms": true, "hotelms": true, "videolock": true, "photolock": true,
}

// IsCloudOnlyCategory reports whether a Tuya category is a battery device that
// sleeps (a smart lock), which the hub should reach through the cloud.
func IsCloudOnlyCategory(category string) bool {
	return cloudOnlyCategories[strings.ToLower(strings.TrimSpace(category))]
}

// defaultReportCodes are used when a cloud_only device has no cached specs: the
// report-log watcher still asks for the data points that matter for a lock.
var defaultReportCodes = []string{
	"unlock_fingerprint", "unlock_password", "unlock_temporary", "unlock_card",
	"unlock_app", "unlock_offline_pd", "unlock_request", "alarm_lock",
	"reverse_lock", "hijack", "doorbell", "residual_electricity",
}

// reportPageSize is the /report-logs page size used by the event watcher.
const reportPageSize = 100

// Config configures the service.
type Config struct {
	// Store is the shared hub database. The service does not own it; the
	// caller that opened it is responsible for closing it.
	Store       *db.Store
	Cloud       tuya.CloudConfig
	ScanTimeout time.Duration
	// CloudPush enables the Tuya Message Service (Pulsar) listener that feeds
	// cloud_only devices from the cloud instead of polling. It only connects
	// while at least one cloud_only device exists.
	CloudPush bool
	// CloudPushTest selects the test channel (out/event-test) of the Message
	// Service instead of production.
	CloudPushTest bool
}

// Service is the hub-facing Tuya adapter.
type Service struct {
	store *db.Store
	cfg   Config
	cloud *tuya.Cloud

	// discover is the LAN lookup, replaced in tests.
	discover func(context.Context, time.Duration) ([]tuya.Discovered, error)

	mu        sync.Mutex
	listeners map[string]*listener
	onEvent   func(deviceID string, e EventRecord)
	// observed holds data points seen while listening, used to render controls
	// when the cloud specifications are not available.
	observed map[string]map[string]db.Spec
	// lastState is the last DP value published per device. Devices re-send the
	// full DP set every few seconds; this is what lets the service publish only
	// real changes instead of re-emitting an identical snapshot.
	lastState map[string]map[string]any
	// disabled holds devices the operator stopped listening to on purpose, so
	// auto-listen does not restart them until an explicit start or a restart.
	disabled map[string]bool
	// lastEvents keeps the last event per device even after its listener ends,
	// so the UI can still show the last error/telemetry.
	lastEvents map[string]EventRecord
	// eventLog keeps the most recent events per device (a bounded ring buffer),
	// including the reconnect markers the listener emits, so the UI can answer
	// "did this press actually arrive?" instead of showing only the last value.
	eventLog map[string][]EventRecord
	// lastSeen records the last time the hub heard from a device, so the UI can
	// show a device as online even when it is not being streamed.
	lastSeen map[string]time.Time
	// cloudOnly caches which devices are reached through the cloud (they have
	// no LAN listener), so Online can keep them shown as online between the
	// slower cloud polls. Refreshed on every auto-listen pass.
	cloudOnly map[string]bool
	// cloudTTL is the online window for a cloud_only device: the poll interval
	// plus slack, set by StartAutoListen.
	cloudTTL time.Duration
	// eventWindow bounds how far back the report-log poll looks, so a slow tick
	// or a short outage is still covered without scanning days of history.
	eventWindow time.Duration
	// bus, when set, receives the Tuya events for the UI (SSE).
	bus *eventbus.Bus

	// energyMu guards energyCache, which keeps the cloud consumption series per
	// device/range so opening the chart does not hit the cloud every render.
	energyMu    sync.Mutex
	energyCache map[string]energyCacheEntry
	// now is the clock used by the energy series (overridable in tests).
	now func() time.Time
}

type listener struct {
	cancel  context.CancelFunc
	client  *tuya.Client
	last    EventRecord
	hasLast bool
}

// EventRecord is one DP update observed while listening.
type EventRecord struct {
	At   time.Time      `json:"at"`
	Kind string         `json:"kind"`
	DPs  map[string]any `json:"dps"`
}

// ScanResult is one LAN device, marked when already registered.
type ScanResult struct {
	ID         string `json:"id"`
	IP         string `json:"ip"`
	Version    string `json:"version"`
	ProductKey string `json:"productKey,omitempty"`
	Name       string `json:"name,omitempty"`
	Registered bool   `json:"registered"`
}

// CloudDeviceInfo is one device on the Tuya account, shown by the registration
// UI. It is the only way to register a device that never appears in the LAN
// scan, such as a battery lock that sleeps.
type CloudDeviceInfo struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Category   string `json:"category"`
	ProductKey string `json:"product_key,omitempty"`
	HasKey     bool   `json:"has_key"`
	Online     bool   `json:"online"`
	Registered bool   `json:"registered"`
}

// Open prepares the service over a shared database and the optional cloud
// client.
func Open(cfg Config) (*Service, error) {
	if cfg.Store == nil {
		return nil, errors.New("tuya: a database store is required")
	}
	if cfg.ScanTimeout <= 0 {
		cfg.ScanTimeout = 5 * time.Second
	}
	s := &Service{store: cfg.Store, cfg: cfg, discover: tuya.Discover, listeners: map[string]*listener{}, observed: map[string]map[string]db.Spec{}, lastState: map[string]map[string]any{}, disabled: map[string]bool{}, lastEvents: map[string]EventRecord{}, eventLog: map[string][]EventRecord{}, lastSeen: map[string]time.Time{}, cloudOnly: map[string]bool{}, energyCache: map[string]energyCacheEntry{}, now: time.Now}
	if strings.TrimSpace(cfg.Cloud.AccessID) != "" && strings.TrimSpace(cfg.Cloud.AccessSecret) != "" {
		cloud, err := tuya.NewCloud(cfg.Cloud)
		if err != nil {
			return nil, err
		}
		s.cloud = cloud
	}
	return s, nil
}

// Close stops listeners. The database is owned by the caller and is not closed
// here.
func (s *Service) Close() error {
	s.mu.Lock()
	for _, l := range s.listeners {
		if l.cancel != nil {
			l.cancel()
		}
	}
	s.listeners = map[string]*listener{}
	s.mu.Unlock()
	return nil
}

// SetEventBus attaches the event bus used to push Tuya updates to the UI.
func (s *Service) SetEventBus(bus *eventbus.Bus) { s.bus = bus }

// CloudConfigured reports whether cloud registration is available.
func (s *Service) CloudConfigured() bool { return s.cloud != nil }

// Devices returns the registered devices.
func (s *Service) Devices(ctx context.Context) ([]db.Device, error) {
	return s.store.ListDevices(ctx, db.KindTuya)
}

// Scan discovers Tuya devices on the LAN.
func (s *Service) Scan(ctx context.Context) ([]ScanResult, error) {
	devices, err := s.discover(ctx, s.cfg.ScanTimeout)
	if err != nil {
		return nil, err
	}
	out := make([]ScanResult, 0, len(devices))
	for _, d := range devices {
		r := ScanResult{ID: d.ID, IP: d.IP, Version: d.Version, ProductKey: d.ProductKey}
		if dev, err := s.store.GetDevice(ctx, d.ID); err == nil {
			r.Registered = dev.LocalKey != ""
			r.Name = dev.Name
		}
		out = append(out, r)
	}
	return out, nil
}

// CloudDevices lists the devices linked to the app account. It is how a device
// that never shows up in a LAN scan (a sleeping battery lock) gets registered:
// the UI lists the account and the operator picks one.
func (s *Service) CloudDevices(ctx context.Context) ([]CloudDeviceInfo, error) {
	if s.cloud == nil {
		return nil, ErrCloudNotConfigured
	}
	seed := s.cloudSeed(ctx)
	if seed == "" {
		return nil, errors.New("tuya: register one device by its id (or run the LAN scan) so the account can be listed")
	}
	list, err := s.cloud.Devices(ctx, seed)
	if err != nil {
		return nil, err
	}
	out := make([]CloudDeviceInfo, 0, len(list))
	for _, cd := range list {
		info := CloudDeviceInfo{
			ID:         cd.ID,
			Name:       sanitizeName(cd.Name),
			Category:   cd.Category,
			ProductKey: cd.ProductID,
			HasKey:     cd.LocalKey != "",
			Online:     cd.Online,
		}
		if _, err := s.store.GetDevice(ctx, cd.ID); err == nil {
			info.Registered = true
		}
		out = append(out, info)
	}
	return out, nil
}

// cloudSeed is any device id already linked to the account. The cloud uses it
// only to discover the owning uid before it lists every device.
func (s *Service) cloudSeed(ctx context.Context) string {
	list, err := s.store.ListDevices(ctx, db.KindTuya)
	if err != nil {
		return ""
	}
	for _, d := range list {
		if d.ID != "" {
			return d.ID
		}
	}
	return ""
}

// Register fetches the local keys for ids (or every cloud device when ids is
// empty) and stores them in the registry.
func (s *Service) Register(ctx context.Context, ids []string) ([]db.Device, error) {
	if s.cloud == nil {
		return nil, ErrCloudNotConfigured
	}
	seed := ""
	if len(ids) > 0 {
		seed = strings.TrimSpace(ids[0])
	}
	if seed == "" {
		seed = s.cloudSeed(ctx)
	}
	if seed == "" {
		return nil, errors.New("tuya: register one device by its id (or run the LAN scan) so the account can be listed")
	}

	cloudDevices, err := s.cloud.Devices(ctx, seed)
	if err != nil {
		return nil, err
	}
	want := map[string]bool{}
	for _, id := range ids {
		want[id] = true
	}

	registered := []db.Device{}
	for _, cd := range cloudDevices {
		if len(want) > 0 && !want[cd.ID] {
			continue
		}
		if cd.LocalKey == "" {
			continue
		}
		dev := db.Device{
			ID:         cd.ID,
			Kind:       db.KindTuya,
			Name:       sanitizeName(cd.Name),
			Version:    cd.Version,
			ProductKey: cd.ProductID,
			LocalKey:   cd.LocalKey,
			KeySource:  "cloud",
			Category:   cd.Category,
		}
		isNew := false
		if _, err := s.store.GetDevice(ctx, cd.ID); err == nil {
			// Keep the local name and address; only refresh the key.
			dev.Name = ""
			dev.IP = ""
		} else {
			dev.Name = s.uniqueName(ctx, dev.Name)
			isNew = true
		}
		if err := s.store.UpsertDevice(ctx, dev); err != nil {
			return registered, err
		}
		// A battery device that sleeps (a lock) is reached through the cloud, so
		// a freshly registered one defaults to cloud_only: the hub keeps no LAN
		// listener open for it. The operator can still override it.
		if isNew && IsCloudOnlyCategory(cd.Category) {
			_ = s.store.SetDeviceCloudOnly(ctx, cd.ID, true)
		}
		s.refreshSpecsBestEffort(ctx, cd.ID)
		if d, err := s.store.GetDevice(ctx, cd.ID); err == nil {
			registered = append(registered, d)
		}
	}
	if len(registered) == 0 {
		return nil, ErrNoMatch
	}
	s.fillIPs(ctx, registered)
	return registered, nil
}

// fillIPs looks up LAN addresses for devices that have none, so a device
// registered only from the cloud can still be dialed. Failures are ignored:
// a later scan at runtime resolves the address anyway.
func (s *Service) fillIPs(ctx context.Context, devices []db.Device) {
	missing := map[string]bool{}
	for _, d := range devices {
		if d.IP == "" {
			missing[d.ID] = true
		}
	}
	if len(missing) == 0 {
		return
	}
	found, err := s.discover(ctx, s.cfg.ScanTimeout)
	if err != nil {
		return
	}
	byID := map[string]string{}
	for _, d := range found {
		if d.IP != "" {
			byID[d.ID] = d.IP
		}
	}
	for i := range devices {
		ip, ok := byID[devices[i].ID]
		if !ok || ip == "" {
			continue
		}
		if err := s.store.SetDeviceIP(ctx, devices[i].ID, ip); err != nil {
			continue
		}
		devices[i].IP = ip
	}
}

// resolve returns the current LAN address and protocol version of a device as
// seen by discovery (either may be empty).
func (s *Service) resolve(ctx context.Context, id string) (ip, version string) {
	found, err := s.discover(ctx, s.cfg.ScanTimeout)
	if err != nil {
		return "", ""
	}
	for _, d := range found {
		if d.ID == id {
			return d.IP, d.Version
		}
	}
	return "", ""
}

// StartListen begins streaming DP updates for a registered device. It clears
// any manual "stop" so an explicit start always wins over auto-listen.
func (s *Service) StartListen(ctx context.Context, id string) error {
	s.mu.Lock()
	delete(s.disabled, id)
	s.mu.Unlock()
	return s.startListen(ctx, id)
}

// startListen is the internal listener starter shared by the explicit API and
// auto-listen.
func (s *Service) startListen(ctx context.Context, id string) error {
	dev, err := s.store.GetDevice(ctx, id)
	if err != nil {
		return err
	}
	// A cloud_only device sleeps: there is no LAN link to keep open. Its state
	// and events come from the cloud poll instead, so this is a no-op success
	// (and an automation on it does not log a spurious LAN failure).
	if dev.CloudOnly {
		return nil
	}
	if dev.LocalKey == "" {
		return ErrNoKey
	}
	if dev.IP == "" {
		// A device registered only from the cloud has no address; resolve it
		// from the LAN before dialing so we never dial an empty host.
		ip, version := s.resolve(ctx, dev.ID)
		if ip == "" {
			return ErrNoIP
		}
		dev.IP = ip
		_ = s.store.SetDeviceIP(ctx, dev.ID, ip)
		if dev.Version == "" && version != "" {
			dev.Version = version
			_ = s.store.SetDeviceVersion(ctx, dev.ID, version)
		}
	}

	s.mu.Lock()
	if _, ok := s.listeners[id]; ok {
		s.mu.Unlock()
		return nil
	}
	lctx, cancel := context.WithCancel(context.Background())
	l := &listener{cancel: cancel}
	s.listeners[id] = l
	s.mu.Unlock()

	cfg := tuya.Config{
		Name:      dev.Name,
		ID:        dev.ID,
		IP:        dev.IP,
		Port:      dev.Port,
		Key:       dev.LocalKey,
		Version:   parseVersion(dev.Version),
		Device22:  dev.Device22,
		Timeout:   3 * time.Second,
		Heartbeat: 15 * time.Second,

		DiscoveryTimeout: s.cfg.ScanTimeout,
	}
	// Persist a rediscovered address so the next start already has it.
	cfg.OnIPChange = func(_, newIP string) {
		_ = s.store.SetDeviceIP(context.Background(), dev.ID, newIP)
	}

	go func() {
		var (
			c   *tuya.Client
			err error
		)
		// On exit, drop the finished listener so auto-listen can retry a device
		// that was unreachable. A deliberate StopListen already removed it and
		// marked it disabled, so this is a no-op then.
		defer func() {
			if c != nil {
				_ = c.Close()
			}
			s.mu.Lock()
			if cur := s.listeners[id]; cur == l {
				l.client = nil
				delete(s.listeners, id)
			}
			s.mu.Unlock()
		}()

		if cfg.Version == "" {
			v, derr := tuya.Detect(lctx, cfg)
			if derr != nil {
				s.appendEvent(id, EventRecord{At: time.Now(), Kind: "error", DPs: map[string]any{"error": derr.Error()}})
				return
			}
			cfg.Version = v
			_ = s.store.SetDeviceVersion(context.Background(), dev.ID, string(v))
		}
		c, err = tuya.Dial(lctx, cfg)
		if err != nil {
			s.appendEvent(id, EventRecord{At: time.Now(), Kind: "error", DPs: map[string]any{"error": err.Error()}})
			return
		}
		s.mu.Lock()
		if cur := s.listeners[id]; cur == l {
			l.client = c
		}
		s.mu.Unlock()
		_ = c.Listen(lctx, func(e tuya.Event) {
			s.appendEvent(id, EventRecord{At: e.At, Kind: e.Kind, DPs: e.DPs})
		})
	}()
	return nil
}

// StopListen stops streaming for a device and remembers the choice so
// auto-listen does not immediately restart it.
func (s *Service) StopListen(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if l := s.listeners[id]; l != nil {
		l.cancel()
		delete(s.listeners, id)
	}
	s.disabled[id] = true
}

// Forget stops listening and removes a device from the registry.
func (s *Service) Forget(ctx context.Context, id string) error {
	s.StopListen(id)
	s.mu.Lock()
	delete(s.observed, id)
	delete(s.lastState, id)
	delete(s.disabled, id)
	delete(s.lastEvents, id)
	delete(s.lastSeen, id)
	delete(s.eventLog, id)
	s.mu.Unlock()
	return s.store.DeleteDevice(ctx, id)
}

// Control writes one DP on a device. For a normal device it prefers the direct
// LAN link (fast and independent of the internet), with the cloud as fallback.
// For a cloud_only device it is the other way around: the cloud first (how it is
// reached while it sleeps), with the LAN as the fallback when the cloud is
// unavailable (and the device has a key/address).
func (s *Service) Control(ctx context.Context, id, dp string, value any) (map[string]any, error) {
	if strings.TrimSpace(id) == "" || strings.TrimSpace(dp) == "" {
		return nil, errors.New("tuya: device id and dp are required")
	}
	dev, err := s.store.GetDevice(ctx, id)
	if err != nil {
		return nil, err
	}

	if dev.CloudOnly {
		// Cloud first.
		var cloudErr error
		if s.cloud != nil {
			if err := s.cloud.Command(ctx, id, s.codeFor(ctx, id, dp), value); err == nil {
				s.markSeen(id)
				return map[string]any{"mode": "cloud", "dp": dp, "value": value}, nil
			} else {
				cloudErr = err
			}
		}
		// LAN fallback: only when the cloud did not answer.
		if lanErr := s.controlByLAN(ctx, dev, dp, value); lanErr == nil {
			s.markSeen(id)
			return map[string]any{"mode": "lan", "dp": dp, "value": value}, nil
		} else if cloudErr != nil {
			return nil, fmt.Errorf("tuya: cloud control failed (%v); LAN fallback failed (%v)", cloudErr, lanErr)
		} else {
			return nil, lanErr
		}
	}

	// LAN first (default for a device that does not sleep).
	lanErr := s.controlByLAN(ctx, dev, dp, value)
	if lanErr == nil {
		s.markSeen(id)
		return map[string]any{"mode": "lan", "dp": dp, "value": value}, nil
	}
	if s.cloud == nil {
		if lanErr != ErrNoKey {
			return nil, fmt.Errorf("tuya: LAN control failed (%v); the cloud is not configured", lanErr)
		}
		return nil, ErrNoKey
	}
	if err := s.cloud.Command(ctx, id, s.codeFor(ctx, id, dp), value); err != nil {
		if lanErr != ErrNoKey {
			return nil, fmt.Errorf("tuya: LAN control failed (%v); cloud failed: %w", lanErr, err)
		}
		return nil, err
	}
	s.markSeen(id)
	return map[string]any{"mode": "cloud", "dp": dp, "value": value}, nil
}

// controlByLAN writes a DP over the LAN, resolving the address first. It reuses
// the listener's connection when one is open. It returns ErrNoKey when the
// device has no local key (so the caller can decide whether that is an error).
func (s *Service) controlByLAN(ctx context.Context, dev db.Device, dp string, value any) error {
	if dev.LocalKey == "" {
		return ErrNoKey
	}
	if dev.IP == "" {
		if ip, version := s.resolve(ctx, dev.ID); ip != "" {
			dev.IP = ip
			_ = s.store.SetDeviceIP(ctx, dev.ID, ip)
			if dev.Version == "" && version != "" {
				_ = s.store.SetDeviceVersion(ctx, dev.ID, version)
			}
		}
	}
	if dev.IP == "" {
		return ErrNoIP
	}
	// Prefer the listener's connection: a Tuya device usually allows a single
	// client, and the reply is read by the listen loop.
	if err := s.controlViaListener(dev.ID, dp, value); err == nil {
		return nil
	}
	return s.controlLAN(ctx, dev, dp, value)
}

func (s *Service) controlLAN(ctx context.Context, dev db.Device, dp string, value any) error {
	cfg := tuya.Config{
		Name:             dev.Name,
		ID:               dev.ID,
		IP:               dev.IP,
		Port:             dev.Port,
		Key:              dev.LocalKey,
		Version:          parseVersion(dev.Version),
		Device22:         dev.Device22,
		Timeout:          5 * time.Second,
		DiscoveryTimeout: s.cfg.ScanTimeout,
	}
	if cfg.Version == "" {
		v, err := tuya.Detect(ctx, cfg)
		if err != nil {
			return err
		}
		cfg.Version = v
		_ = s.store.SetDeviceVersion(ctx, dev.ID, string(v))
	}
	c, err := tuya.Dial(ctx, cfg)
	if err != nil {
		return err
	}
	defer c.Close()
	_, err = c.Set(ctx, dp, value)
	return err
}

// controlViaListener writes a DP on the connection owned by the listener, if
// one is open, then asks for a fresh snapshot. Returns errNoListener when the
// device is not being listened to (the caller falls back to a short-lived
// connection).
func (s *Service) controlViaListener(id, dp string, value any) error {
	c := s.listenerClient(id)
	if c == nil {
		return errNoListener
	}
	if err := c.SetNoWait(dp, value); err != nil {
		return err
	}
	// The reply arrives on the listen loop; a query right after makes the new
	// state show up without waiting for the next periodic refresh.
	_ = c.Query()
	return nil
}

// queryViaListener sends a DP_QUERY on the listener's connection.
func (s *Service) queryViaListener(id string) error {
	c := s.listenerClient(id)
	if c == nil {
		return errNoListener
	}
	return c.Query()
}

func (s *Service) listenerClient(id string) *tuya.Client {
	s.mu.Lock()
	defer s.mu.Unlock()
	if l := s.listeners[id]; l != nil {
		return l.client
	}
	return nil
}

// Refresh asks a device for its current data points. It reuses the listener's
// connection when one is open and otherwise opens a short-lived connection.
// It is generic: it works for any device with telemetry, not just metering
// plugs.
func (s *Service) Refresh(ctx context.Context, id string) (map[string]any, error) {
	if err := s.queryViaListener(id); err == nil {
		return s.store.States(ctx, id)
	}
	return s.RefreshState(ctx, id)
}

// StartAutoListen keeps a listener open for every registered Tuya device and
// periodically asks for a fresh snapshot on the same connection, so any device
// with telemetry updates without the operator pressing "Ouvir". No device type
// is special-cased. Devices the operator stopped on purpose are left alone.
//
// Devices marked cloud_only (battery devices that sleep) get no listener: their
// state is polled from the cloud at cloudStatus, and their report logs are
// turned into events at cloudEvents (a lock's unlock should trigger a scene
// promptly, so events are polled faster than the state snapshot).
func (s *Service) StartAutoListen(ctx context.Context, lan, cloudStatus, cloudEvents time.Duration) {
	if lan <= 0 {
		lan = 5 * time.Second
	}
	if cloudStatus <= 0 {
		cloudStatus = 300 * time.Second
	}
	if cloudEvents <= 0 {
		cloudEvents = 60 * time.Second
	}
	s.mu.Lock()
	// Keep a cloud_only device shown as online between two state polls.
	s.cloudTTL = cloudStatus + 30*time.Second
	// Look back far enough that a slow tick or a short outage is still covered.
	s.eventWindow = 3 * cloudEvents
	if s.eventWindow < 5*time.Minute {
		s.eventWindow = 5 * time.Minute
	}
	s.mu.Unlock()
	// The Message Service listener is independent of the LAN/cloud tickers: it
	// runs only while a cloud_only device exists.
	s.startCloudPush(ctx)
	go func() {
		s.autoListen(ctx)
		// Read cloud_only devices once at startup so the UI shows their last
		// state (for example a lock's battery) without waiting for the ticker.
		// The report poll seeds its cursor (no automation fires on old events).
		s.cloudPoll(ctx)
		s.reportPoll(ctx)
		refresh := time.NewTicker(lan)
		rescan := time.NewTicker(30 * time.Second)
		status := time.NewTicker(cloudStatus)
		events := time.NewTicker(cloudEvents)
		defer refresh.Stop()
		defer rescan.Stop()
		defer status.Stop()
		defer events.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-rescan.C:
				s.autoListen(ctx)
			case <-refresh.C:
				s.refreshStale(lan)
			case <-status.C:
				s.cloudPoll(ctx)
			case <-events.C:
				s.reportPoll(ctx)
			}
		}
	}()
}

func (s *Service) autoListen(ctx context.Context) {
	devices, err := s.store.ListDevices(ctx, db.KindTuya)
	if err != nil {
		return
	}
	// Remember which devices are cloud_only so Online can widen their window.
	cloud := map[string]bool{}
	for _, d := range devices {
		if d.CloudOnly {
			cloud[d.ID] = true
		}
	}
	s.mu.Lock()
	s.cloudOnly = cloud
	s.mu.Unlock()
	for _, d := range devices {
		if ctx.Err() != nil {
			return
		}
		if d.LocalKey == "" {
			continue
		}
		// A cloud_only device sleeps: it has no LAN listener and is polled from
		// the cloud instead (see cloudPoll).
		if d.CloudOnly {
			continue
		}
		s.mu.Lock()
		_, listening := s.listeners[d.ID]
		disabled := s.disabled[d.ID]
		s.mu.Unlock()
		if listening || disabled {
			continue
		}
		if err := s.startListen(ctx, d.ID); err != nil {
			log.Printf("[tuya] auto-listen %s: %v", d.ID, err)
		}
	}
}

// cloudPoll refreshes the state of the cloud_only devices from the Tuya cloud.
// They are not reachable on the LAN (they sleep), so this is their only state
// source; it runs at the cloud cadence to respect the Tuya API quota.
func (s *Service) cloudPoll(ctx context.Context) {
	devices, err := s.store.ListDevices(ctx, db.KindTuya)
	if err != nil {
		return
	}
	for _, d := range devices {
		if ctx.Err() != nil {
			return
		}
		if !d.CloudOnly {
			continue
		}
		cctx, cancel := context.WithTimeout(ctx, 20*time.Second)
		_, _ = s.RefreshState(cctx, d.ID)
		cancel()
	}
}

// reportPoll turns the recent report logs of the cloud_only devices into
// events. It is separate from cloudPoll (the state snapshot) because events want
// a faster cadence. A device with no cursor yet just seeds it, so enabling the
// watcher never replays history into automations.
func (s *Service) reportPoll(ctx context.Context) {
	devices, err := s.store.ListDevices(ctx, db.KindTuya)
	if err != nil {
		return
	}
	for _, d := range devices {
		if ctx.Err() != nil {
			return
		}
		if !d.CloudOnly {
			continue
		}
		s.processReports(ctx, d)
	}
}

// processReports fires one event per new report-log entry of a device, in
// chronological order, advancing a durable cursor so each entry is delivered
// exactly once (even across a hub restart).
func (s *Service) processReports(ctx context.Context, d db.Device) {
	if s.cloud == nil {
		return
	}
	codes := s.reportCodes(ctx, d.ID)
	if len(codes) == 0 {
		return
	}
	cursor, haveCursor, err := s.store.ReportCursor(ctx, d.ID)
	if err != nil {
		return
	}

	now := time.Now()
	if !haveCursor {
		// First sight: look back far enough to observe the device's existing
		// history, remember the newest entry as the cursor, and fire nothing, so
		// enabling the watcher never replays old events into automations.
		seedStart := now.Add(-24 * time.Hour).UnixMilli()
		cctx, cancel := context.WithTimeout(ctx, 20*time.Second)
		defer cancel()
		logs, err := s.cloud.ReportLogs(cctx, d.ID, codes, seedStart, now.UnixMilli()+1, reportPageSize)
		if err != nil {
			return
		}
		max := int64(0)
		for _, l := range logs {
			if l.EventTime > max {
				max = l.EventTime
			}
		}
		_ = s.store.SetReportCursor(ctx, d.ID, max)
		return
	}

	start := now.Add(-s.reportLookback()).UnixMilli()
	if cursor > start {
		// Resume from just after the last delivered entry.
		start = cursor
	}

	cctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	logs, err := s.cloud.ReportLogs(cctx, d.ID, codes, start, now.UnixMilli()+1, reportPageSize)
	if err != nil {
		return
	}

	sort.Slice(logs, func(i, j int) bool { return logs[i].EventTime < logs[j].EventTime })
	byCode := s.codeToDP(ctx, d.ID)
	max := cursor
	for _, l := range logs {
		if l.EventTime <= cursor || l.Code == "" {
			continue
		}
		s.publishReport(d.ID, byCode, l)
		if l.EventTime > max {
			max = l.EventTime
		}
	}
	if max > cursor {
		_ = s.store.SetReportCursor(ctx, d.ID, max)
	}
}

// publishReport records one report entry as a "report" event: it carries the DP
// id (so it lands in the same state map the UI renders) and kind "report", which
// the dispatcher fires without value dedup because it is an event, not a state.
func (s *Service) publishReport(id string, byCode map[string]string, l tuya.ReportLog) {
	dp := byCode[l.Code]
	if dp == "" {
		dp = l.Code
	}
	s.recordEvent(id, EventRecord{
		At:   time.UnixMilli(l.EventTime),
		Kind: "report",
		DPs:  map[string]any{dp: coerceReportValue(l.Value)},
	})
}

// reportCodes are the DP codes requested from /report-logs: the cached specs
// when available, otherwise a default set that covers a lock.
func (s *Service) reportCodes(ctx context.Context, id string) []string {
	specs, err := s.store.ListSpecs(ctx, id)
	if err != nil || len(specs) == 0 {
		return defaultReportCodes
	}
	codes := make([]string, 0, len(specs))
	for _, sp := range specs {
		if sp.Code != "" {
			codes = append(codes, sp.Code)
		}
	}
	if len(codes) == 0 {
		return defaultReportCodes
	}
	return codes
}

func (s *Service) reportLookback() time.Duration {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.eventWindow <= 0 {
		return 5 * time.Minute
	}
	return s.eventWindow
}

// coerceReportValue turns a raw report-log value into the same shape the state
// map uses: a numeric string becomes a number and "true"/"false" become a bool,
// so an automation value compares consistently.
func coerceReportValue(v any) any {
	str, ok := v.(string)
	if !ok {
		return v
	}
	switch strings.TrimSpace(str) {
	case "true":
		return true
	case "false":
		return false
	}
	if f, err := strconv.ParseFloat(strings.TrimSpace(str), 64); err == nil {
		return f
	}
	return str
}

// refreshStale queries only the devices whose last update is older than the
// interval, so an actively pushing device is not polled needlessly while an
// idle one still reports its telemetry at a predictable cadence.
func (s *Service) refreshStale(interval time.Duration) {
	now := time.Now()
	var clients []*tuya.Client
	s.mu.Lock()
	for _, l := range s.listeners {
		if l.client == nil {
			continue
		}
		if l.hasLast && now.Sub(l.last.At) < interval {
			continue
		}
		clients = append(clients, l.client)
	}
	s.mu.Unlock()
	for _, c := range clients {
		_ = c.Query()
	}
}

// refreshSpecsBestEffort caches the cloud DP specifications, ignoring failures
// (some sub-devices do not expose them directly).
func (s *Service) refreshSpecsBestEffort(ctx context.Context, id string) {
	if s.cloud == nil {
		return
	}
	if _, err := s.RefreshSpecs(ctx, id); err != nil {
		// Not fatal: the UI can trigger a refresh later.
		_ = err
	}
}

// State returns the last-known data points of a device (no device query).
func (s *Service) State(ctx context.Context, id string) (map[string]any, error) {
	return s.store.States(ctx, id)
}

// RefreshState reads the current data points of a device and stores them. For a
// normal device it reads the LAN first with the cloud as fallback; for a
// cloud_only device it is the opposite: the cloud first, the LAN as the fallback
// when the cloud is unavailable.
func (s *Service) RefreshState(ctx context.Context, id string) (map[string]any, error) {
	dev, err := s.store.GetDevice(ctx, id)
	if err != nil {
		return nil, err
	}

	if dev.CloudOnly {
		var cloudErr error
		if s.cloud != nil {
			if dps, err := s.refreshCloud(ctx, id); err == nil {
				return dps, nil
			} else {
				cloudErr = err
			}
		}
		if dev.LocalKey != "" {
			if dps, err := s.refreshLAN(ctx, dev); err == nil {
				return dps, nil
			} else if cloudErr != nil {
				return nil, fmt.Errorf("tuya: cloud read failed (%v); LAN fallback failed (%v)", cloudErr, err)
			} else {
				return nil, err
			}
		}
		if cloudErr != nil {
			return nil, fmt.Errorf("tuya: cloud read failed (%v); no LAN fallback available", cloudErr)
		}
		return nil, ErrNoKey
	}

	// LAN first (default for a device that does not sleep).
	if dev.LocalKey != "" {
		if dps, err := s.refreshLAN(ctx, dev); err == nil {
			return dps, nil
		}
	}
	if s.cloud == nil {
		return nil, ErrNoKey
	}
	return s.refreshCloud(ctx, id)
}

// refreshLAN reads a device over the LAN (resolving the address first) and
// persists the result.
func (s *Service) refreshLAN(ctx context.Context, dev db.Device) (map[string]any, error) {
	if dev.LocalKey == "" {
		return nil, ErrNoKey
	}
	if dev.IP == "" {
		if ip, version := s.resolve(ctx, dev.ID); ip != "" {
			dev.IP = ip
			_ = s.store.SetDeviceIP(ctx, dev.ID, ip)
			if dev.Version == "" && version != "" {
				_ = s.store.SetDeviceVersion(ctx, dev.ID, version)
			}
		}
	}
	if dev.IP == "" {
		return nil, ErrNoIP
	}
	dps, err := s.statusLAN(ctx, dev)
	if err != nil {
		return nil, err
	}
	_ = s.store.ReplaceStates(ctx, dev.ID, dps)
	s.publishState(dev.ID, dps)
	return dps, nil
}

// refreshCloud reads a device through the Tuya cloud and persists the result.
func (s *Service) refreshCloud(ctx context.Context, id string) (map[string]any, error) {
	if s.cloud == nil {
		return nil, ErrCloudNotConfigured
	}
	values, err := s.cloud.Status(ctx, id)
	if err != nil {
		return nil, err
	}
	byCode := s.codeToDP(ctx, id)
	dps := map[string]any{}
	for _, v := range values {
		key := byCode[v.Code]
		if key == "" {
			key = v.Code
		}
		dps[key] = v.Value
	}
	_ = s.store.ReplaceStates(ctx, id, dps)
	s.publishState(id, dps)
	return dps, nil
}

// publishState pushes a full state snapshot to the UI over SSE.
func (s *Service) publishState(id string, dps map[string]any) {
	if len(dps) == 0 {
		return
	}
	s.markSeen(id)
	// Feed the change cache so a DP report that repeats what we just published
	// (or read) is not emitted again.
	s.trackState(id, dps)
	if s.bus == nil {
		return
	}
	s.bus.PublishData("tuya", map[string]any{"device": id, "kind": "state", "dps": dps})
}

// trackState records the last DP values seen for a device and reports whether
// any of them differs from the cached copy. Tuya devices re-send their whole DP
// set every few seconds; without this the hub would re-publish an identical
// snapshot each time and the UI would redraw (and drop in-flight edits) for
// nothing.
func (s *Service) trackState(id string, dps map[string]any) bool {
	if len(dps) == 0 {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	prev := s.lastState[id]
	if prev == nil {
		prev = make(map[string]any, len(dps))
		s.lastState[id] = prev
	}
	changed := false
	for dp, v := range dps {
		if old, ok := prev[dp]; !ok || !sameDPValue(old, v) {
			changed = true
		}
		prev[dp] = v
	}
	return changed
}

// sameDPValue compares two DP values, tolerating the numeric types the Tuya
// protocol, JSON decoding and the database produce for the same number.
func sameDPValue(a, b any) bool {
	if reflect.DeepEqual(a, b) {
		return true
	}
	af, aok := asFloat(a)
	bf, bok := asFloat(b)
	return aok && bok && af == bf
}

func asFloat(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case float32:
		return float64(n), true
	case int:
		return float64(n), true
	case int32:
		return float64(n), true
	case int64:
		return float64(n), true
	case uint:
		return float64(n), true
	case uint64:
		return float64(n), true
	case json.Number:
		f, err := n.Float64()
		return f, err == nil
	}
	return 0, false
}

func (s *Service) statusLAN(ctx context.Context, dev db.Device) (map[string]any, error) {
	cfg := tuya.Config{
		Name:             dev.Name,
		ID:               dev.ID,
		IP:               dev.IP,
		Port:             dev.Port,
		Key:              dev.LocalKey,
		Version:          parseVersion(dev.Version),
		Device22:         dev.Device22,
		Timeout:          5 * time.Second,
		DiscoveryTimeout: s.cfg.ScanTimeout,
	}
	if cfg.Version == "" {
		v, err := tuya.Detect(ctx, cfg)
		if err != nil {
			return nil, err
		}
		cfg.Version = v
		_ = s.store.SetDeviceVersion(ctx, dev.ID, string(v))
	}
	c, err := tuya.Dial(ctx, cfg)
	if err != nil {
		return nil, err
	}
	defer c.Close()
	res, err := c.Status(ctx)
	if err != nil {
		return nil, err
	}
	return res.DPs, nil
}

// codeToDP maps a cloud DP code to its dp id using the cached specs.
func (s *Service) codeToDP(ctx context.Context, id string) map[string]string {
	specs, err := s.store.ListSpecs(ctx, id)
	if err != nil {
		return nil
	}
	m := map[string]string{}
	for _, sp := range specs {
		if sp.Code != "" {
			m[sp.Code] = sp.DPID
		}
	}
	return m
}

// codeFor maps a DP id to its cloud function code, falling back to the id.
func (s *Service) codeFor(ctx context.Context, id, dp string) string {
	specs, err := s.store.ListSpecs(ctx, id)
	if err != nil {
		return dp
	}
	for _, sp := range specs {
		if sp.DPID == dp && sp.Code != "" {
			return sp.Code
		}
	}
	return dp
}

// Specs returns the data-point specifications of a device: the cloud-cached
// ones when available, otherwise the ones inferred from observed DPs.
func (s *Service) Specs(ctx context.Context, id string) ([]db.Spec, error) {
	list, err := s.store.ListSpecs(ctx, id)
	if err != nil {
		return nil, err
	}
	if len(list) > 0 {
		return list, nil
	}
	return s.observedSpecs(id), nil
}

func (s *Service) observedSpecs(id string) []db.Spec {
	s.mu.Lock()
	defer s.mu.Unlock()
	m := s.observed[id]
	if len(m) == 0 {
		return []db.Spec{}
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([]db.Spec, 0, len(keys))
	for _, k := range keys {
		out = append(out, m[k])
	}
	return out
}

// RefreshSpecs fetches the data-point specifications from the cloud and caches
// them. The cloud lists a DP in both "functions" (writable) and "status", so
// entries are merged by dp id.
func (s *Service) RefreshSpecs(ctx context.Context, id string) ([]db.Spec, error) {
	if s.cloud == nil {
		return nil, ErrCloudNotConfigured
	}
	specs, err := s.cloud.Specifications(ctx, id)
	if err != nil {
		return nil, err
	}
	merged := map[string]db.Spec{}
	for _, sp := range specs {
		if sp.DPID == "" {
			continue
		}
		values, _ := json.Marshal(sp.Values)
		cur, ok := merged[sp.DPID]
		if !ok {
			merged[sp.DPID] = db.Spec{
				DeviceID: id,
				DPID:     sp.DPID,
				Code:     sp.Code,
				Name:     sp.Name,
				Desc:     sp.Desc,
				Type:     sp.Type,
				Values:   values,
				Writable: sp.Writable,
			}
			continue
		}
		// Merge: a writable (function) entry wins the flag; keep the first
		// non-empty code/type/values/name/desc.
		cur.Writable = cur.Writable || sp.Writable
		if cur.Code == "" {
			cur.Code = sp.Code
		}
		if cur.Name == "" {
			cur.Name = sp.Name
		}
		if cur.Desc == "" {
			cur.Desc = sp.Desc
		}
		if cur.Type == "" {
			cur.Type = sp.Type
		}
		if (len(cur.Values) == 0 || string(cur.Values) == "null") && len(values) > 0 {
			cur.Values = values
		}
		merged[sp.DPID] = cur
	}
	out := make([]db.Spec, 0, len(merged))
	for _, sp := range merged {
		out = append(out, sp)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].DPID < out[j].DPID })
	if err := s.store.ReplaceSpecs(ctx, id, out); err != nil {
		return nil, err
	}
	return out, nil
}

// Listening reports which device IDs are currently being listened to.
func (s *Service) Listening() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]string, 0, len(s.listeners))
	for id := range s.listeners {
		out = append(out, id)
	}
	return out
}

// LastEvent returns the last event seen for a device, if any.
func (s *Service) LastEvent(id string) (EventRecord, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.lastEvents[id]
	return e, ok
}

// eventLogCap bounds the per-device event ring buffer kept for the UI.
const eventLogCap = 40

// appendLogLocked records an event in the per-device ring buffer, dropping the
// oldest entries once it is full. It must be called with s.mu held.
func (s *Service) appendLogLocked(id string, e EventRecord) {
	if s.eventLog == nil {
		return
	}
	buf := append(s.eventLog[id], e)
	if len(buf) > eventLogCap {
		buf = append([]EventRecord(nil), buf[len(buf)-eventLogCap:]...)
	}
	s.eventLog[id] = buf
}

// RecentEvents returns up to n of the most recent events seen for a device, in
// chronological order. It is what lets the UI show whether a press actually
// arrived (and where a reconnect happened) instead of only the last value.
func (s *Service) RecentEvents(id string, n int) []EventRecord {
	s.mu.Lock()
	defer s.mu.Unlock()
	buf := s.eventLog[id]
	if n <= 0 || n > len(buf) {
		n = len(buf)
	}
	out := make([]EventRecord, n)
	copy(out, buf[len(buf)-n:])
	return out
}

// SetCloudOnly switches a device between LAN listening and cloud polling. It is
// the runtime half of the "Ler pela nuvem" toggle: turning it on stops the LAN
// listener so the two sources never both fire the same press, and seeds the
// report cursor to now so enabling the cloud never replays recent history into
// automations; turning it off restarts the LAN listener.
func (s *Service) SetCloudOnly(ctx context.Context, id string, cloudOnly bool) error {
	if cloudOnly && s.cloud == nil {
		return ErrCloudNotConfigured
	}
	if err := s.store.SetDeviceCloudOnly(ctx, id, cloudOnly); err != nil {
		return err
	}
	if cloudOnly {
		s.StopListen(id)
		if s.cloud != nil {
			_ = s.store.SetReportCursor(ctx, id, time.Now().UnixMilli())
		}
		return nil
	}
	// A device switched back to the LAN may still lack a key or address; let
	// auto-listen pick it up later instead of failing the toggle.
	if err := s.StartListen(ctx, id); err != nil && err != ErrNoKey && err != ErrNoIP {
		return err
	}
	return nil
}

// markSeen records that the hub just heard from a device.
func (s *Service) markSeen(id string) {
	s.mu.Lock()
	s.lastSeen[id] = time.Now()
	s.mu.Unlock()
}

// Online reports whether the hub currently has a live link to a device or heard
// from it very recently. It is used by the UI so a reachable device shows as
// online even when it is not being streamed. A cloud_only device has no LAN
// link, so its window is the cloud poll interval (plus slack).
func (s *Service) Online(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if l := s.listeners[id]; l != nil && l.client != nil {
		return true
	}
	window := 90 * time.Second
	if s.cloudOnly[id] && s.cloudTTL > window {
		window = s.cloudTTL
	}
	if t, ok := s.lastSeen[id]; ok && time.Since(t) < window {
		return true
	}
	return false
}

// SetEventHandler installs a callback invoked for every event produced by the
// listeners. It is called without holding the service lock, so the handler may
// call other service methods.
func (s *Service) SetEventHandler(fn func(deviceID string, e EventRecord)) {
	s.mu.Lock()
	s.onEvent = fn
	s.mu.Unlock()
}

func (s *Service) appendEvent(id string, e EventRecord) {
	s.mu.Lock()
	l := s.listeners[id]
	if l == nil {
		s.mu.Unlock()
		return
	}
	l.last = e
	l.hasLast = true
	s.lastEvents[id] = e
	s.appendLogLocked(id, e)
	seen := e.At
	if seen.IsZero() {
		seen = time.Now()
	}
	s.lastSeen[id] = seen
	fn := s.onEvent
	s.mu.Unlock()

	s.observe(id, e)
	if fn != nil {
		fn(id, e)
	}
}

// recordEvent records an event for a device and notifies the UI and the
// dispatcher. Unlike appendEvent it does not require an open listener, so a
// cloud_only device (never listened to) can still produce events from its report
// logs. lastSeen is "now" (when the hub heard), not the event time, which may be
// a moment in the past.
func (s *Service) recordEvent(id string, e EventRecord) {
	s.mu.Lock()
	if l := s.listeners[id]; l != nil {
		l.last = e
		l.hasLast = true
	}
	s.lastEvents[id] = e
	s.lastSeen[id] = time.Now()
	s.appendLogLocked(id, e)
	fn := s.onEvent
	s.mu.Unlock()

	s.observe(id, e)
	if fn != nil {
		fn(id, e)
	}
}

// observe records the data points seen for a device so the UI can render
// controls even without the cloud specifications, and persists the last-known
// state.
func (s *Service) observe(id string, e EventRecord) {
	if e.Kind != "status" && e.Kind != "dp" && e.Kind != "report" {
		return
	}
	s.mu.Lock()
	m := s.observed[id]
	if m == nil {
		m = map[string]db.Spec{}
		s.observed[id] = m
	}
	for dp, v := range e.DPs {
		if _, ok := m[dp]; ok {
			continue
		}
		m[dp] = db.Spec{DeviceID: id, DPID: dp, Code: dp, Type: inferType(v), Writable: true}
	}
	s.mu.Unlock()

	// Devices re-send their whole DP set every few seconds. Persisting and
	// re-publishing an unchanged snapshot only makes the UI redraw (and drop
	// edits in progress), so stop here unless something actually moved.
	if !s.trackState(id, e.DPs) {
		return
	}

	// Persist the current state; it is read by the UI even when not listening.
	_ = s.store.SetStates(context.Background(), id, e.DPs)

	if s.bus != nil {
		s.bus.PublishData("tuya", map[string]any{
			"device": id,
			"kind":   e.Kind,
			"dps":    e.DPs,
			"at":     e.At,
		})
	}
}

// inferType guesses a Tuya DP type from an observed value.
func inferType(v any) string {
	switch v.(type) {
	case bool:
		return "Boolean"
	case float64, int64, int, float32:
		return "Integer"
	case string:
		return "String"
	default:
		return "String"
	}
}

func (s *Service) uniqueName(ctx context.Context, base string) string {
	if base == "" {
		base = "tuya"
	}
	if _, err := s.store.GetDeviceByName(ctx, base); errors.Is(err, db.ErrNotFound) {
		return base
	}
	for i := 2; ; i++ {
		name := fmt.Sprintf("%s-%d", base, i)
		if _, err := s.store.GetDeviceByName(ctx, name); errors.Is(err, db.ErrNotFound) {
			return name
		}
	}
}

func parseVersion(s string) tuya.Version {
	if s == "" || s == "auto" {
		return ""
	}
	v, err := tuya.ParseVersion(s)
	if err != nil {
		return ""
	}
	return v
}

func sanitizeName(name string) string {
	name = strings.TrimSpace(name)
	var b strings.Builder
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
			b.WriteRune(r)
		default:
			b.WriteRune('-')
		}
	}
	return strings.Trim(b.String(), "-_")
}
