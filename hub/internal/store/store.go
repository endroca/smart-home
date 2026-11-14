// Package store keeps the hot, in-memory inventory of the gateways: the
// manifest each one advertises, its status and the state it reports for each
// sub-device. It is safe for concurrent use by the HTTP handlers and the
// device ingest.
package store

import (
	"encoding/json"
	"sort"
	"sync"
	"time"

	"smart_home/hub/internal/model"
)

// Store is the in-memory inventory.
type Store struct {
	mu     sync.RWMutex
	things map[string]*model.Thing

	// announced holds the LAN address each gateway announced over mDNS, keyed
	// by its thing name. It is how the hub reaches a gateway before an explicit
	// URL or IP is configured.
	announced map[string]string
}

// New returns an empty store.
func New() *Store {
	return &Store{things: make(map[string]*model.Thing), announced: map[string]string{}}
}

// SetAnnouncedURL remembers the address a gateway announced over mDNS.
func (s *Store) SetAnnouncedURL(id, url string) {
	if id == "" || url == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.announced == nil {
		s.announced = map[string]string{}
	}
	s.announced[id] = url
}

// AnnouncedURL returns the address the gateway announced over mDNS ("" when it
// never announced itself).
func (s *Store) AnnouncedURL(id string) string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.announced[id]
}

func cloneThing(t *model.Thing) *model.Thing {
	if t == nil {
		return nil
	}
	data, err := json.Marshal(t)
	if err != nil {
		return nil
	}
	var out model.Thing
	if err := json.Unmarshal(data, &out); err != nil {
		return nil
	}
	if out.States == nil {
		out.States = map[string]*model.DeviceState{}
	}
	return &out
}

func (s *Store) thingLocked(id string) *model.Thing {
	t := s.things[id]
	if t == nil {
		t = &model.Thing{ID: id, States: map[string]*model.DeviceState{}}
		s.things[id] = t
	}
	return t
}

// UpsertManifest replaces the manifest and returns the ids of the sub-devices
// that were removed and created. Removed entries are deleted from the local
// inventory immediately; the caller drops their replicated state as well.
func (s *Store) UpsertManifest(id string, m *model.Manifest) (removed, created []string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	t := s.thingLocked(id)
	if t.Manifest != nil && m != nil && m.ManifestVersion < t.Manifest.ManifestVersion {
		return nil, nil
	}
	if t.States == nil {
		t.States = map[string]*model.DeviceState{}
	}

	old := map[string]bool{}
	if t.Manifest != nil {
		for _, d := range t.Manifest.Devices {
			old[d.ID] = true
		}
	}
	now := map[string]bool{}
	if m != nil {
		for _, d := range m.Devices {
			now[d.ID] = true
		}
	}
	for id := range old {
		if !now[id] {
			removed = append(removed, id)
			delete(t.States, id)
		}
	}
	for id := range now {
		if !old[id] {
			created = append(created, id)
		}
		if t.States[id] == nil {
			t.States[id] = &model.DeviceState{Thing: id, DeviceID: id}
		}
	}
	t.Manifest = m
	t.UpdatedAt = time.Now().UTC()
	sort.Strings(removed)
	sort.Strings(created)
	return removed, created
}

// RemoveThing deletes a thing when an empty retained manifest is received.
func (s *Store) RemoveThing(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.things, id)
}

// UpsertStatus replaces the retained status.
func (s *Store) UpsertStatus(id string, st *model.Status) {
	s.mu.Lock()
	defer s.mu.Unlock()
	t := s.thingLocked(id)
	t.Status = st
	t.UpdatedAt = time.Now().UTC()
}

// UpsertState replaces the state of one sub-device.
func (s *Store) UpsertState(id, deviceID string, st *model.DeviceState) {
	s.mu.Lock()
	defer s.mu.Unlock()
	t := s.thingLocked(id)
	if t.States == nil {
		t.States = map[string]*model.DeviceState{}
	}
	st.Thing = id
	st.DeviceID = deviceID
	t.States[deviceID] = st
	t.UpdatedAt = time.Now().UTC()
}

// DeleteState removes one sub-device state locally.
func (s *Store) DeleteState(id, deviceID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if t := s.things[id]; t != nil {
		delete(t.States, deviceID)
		t.UpdatedAt = time.Now().UTC()
	}
}

// SetReported updates the local reported state from a device report.
// A fresh report always clears the stale flag: the device just confirmed it.
// It creates the thing entry when missing, so the durable replica can be
// seeded at boot before any manifest arrives.
func (s *Store) SetReported(id, deviceID string, reported map[string]any) {
	s.mu.Lock()
	defer s.mu.Unlock()
	t := s.thingLocked(id)
	if t.States == nil {
		t.States = map[string]*model.DeviceState{}
	}
	st := t.States[deviceID]
	if st == nil {
		st = &model.DeviceState{Thing: id, DeviceID: deviceID}
		t.States[deviceID] = st
	}
	st.Reported = reported
	st.Stale = false
	st.UpdatedAt = time.Now().UTC()
	t.UpdatedAt = st.UpdatedAt
}

// MarkStale flags a state as restored from the durable replica but not yet
// confirmed by the device.
func (s *Store) MarkStale(id, deviceID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	t := s.things[id]
	if t == nil || t.States[deviceID] == nil {
		return
	}
	t.States[deviceID].Stale = true
}

// SetOnline flips only the presence flag of a gateway and reports whether it
// changed. It keeps the diagnostics the last status carried; the heartbeat
// watchdog uses it to mark a silent gateway offline.
func (s *Store) SetOnline(id string, online bool) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	t := s.things[id]
	if t == nil || t.Status == nil || t.Status.Online == online {
		return false
	}
	t.Status.Online = online
	t.UpdatedAt = time.Now().UTC()
	return true
}

// SetEvent records the last asynchronous event for a thing.
func (s *Store) SetEvent(id string, event map[string]any) {
	s.mu.Lock()
	defer s.mu.Unlock()
	t := s.thingLocked(id)
	t.LastEvent = event
	t.UpdatedAt = time.Now().UTC()
}

// SetAck records the last command acknowledgement for a thing.
func (s *Store) SetAck(id string, ack map[string]any) {
	s.mu.Lock()
	defer s.mu.Unlock()
	t := s.thingLocked(id)
	t.LastAck = ack
	t.UpdatedAt = time.Now().UTC()
}

// Get returns a deep copy of one thing.
func (s *Store) Get(id string) (*model.Thing, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	t, ok := s.things[id]
	if !ok {
		return nil, false
	}
	return cloneThing(t), true
}

// State returns a deep copy of one sub-device state.
func (s *Store) State(id, name string) (*model.DeviceState, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	t, ok := s.things[id]
	if !ok || t.States[name] == nil {
		return nil, false
	}
	raw, err := json.Marshal(t.States[name])
	if err != nil {
		return nil, false
	}
	var sh model.DeviceState
	if err := json.Unmarshal(raw, &sh); err != nil {
		return nil, false
	}
	return &sh, true
}

// Snapshot returns deep copies of all things sorted by ID.
func (s *Store) Snapshot() []*model.Thing {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*model.Thing, 0, len(s.things))
	for _, t := range s.things {
		if c := cloneThing(t); c != nil {
			out = append(out, c)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// IDs returns all known thing IDs.
func (s *Store) IDs() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]string, 0, len(s.things))
	for id := range s.things {
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}

// States returns deep copies of all sub-device states for one thing.
func (s *Store) States(id string) map[string]*model.DeviceState {
	s.mu.RLock()
	defer s.mu.RUnlock()
	t := s.things[id]
	if t == nil {
		return nil
	}
	out := make(map[string]*model.DeviceState, len(t.States))
	for name, sh := range t.States {
		raw, err := json.Marshal(sh)
		if err != nil {
			continue
		}
		var cp model.DeviceState
		if err := json.Unmarshal(raw, &cp); err == nil {
			out[name] = &cp
		}
	}
	return out
}
