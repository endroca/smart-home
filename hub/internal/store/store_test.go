package store

import (
	"testing"

	"smart_home/hub/internal/model"
)

func TestManifestLifecycle(t *testing.T) {
	s := New()
	m1 := &model.Manifest{
		ManifestVersion: 1,
		Thing:           "t1",
		Devices: []model.Device{
			{ID: "fan0", Name: "A", Capabilities: map[string]model.Capability{"power": {Kind: "switch"}}},
			{ID: "fan1", Name: "B", Capabilities: map[string]model.Capability{"power": {Kind: "switch"}}},
		},
	}
	removed, created := s.UpsertManifest("t1", m1)
	if len(removed) != 0 || len(created) != 2 {
		t.Fatalf("removed=%v created=%v", removed, created)
	}
	sh, ok := s.State("t1", "fan1")
	if !ok || sh.DeviceID != "fan1" {
		t.Fatal("fan1 state not created")
	}

	// Older retained manifests must not overwrite a newer one.
	s.UpsertManifest("t1", &model.Manifest{ManifestVersion: 0, Thing: "t1"})
	got, _ := s.Get("t1")
	if got.Manifest.ManifestVersion != 1 || len(got.Manifest.Devices) != 2 {
		t.Fatal("older manifest replaced current")
	}

	m2 := &model.Manifest{
		ManifestVersion: 2,
		Thing:           "t1",
		Devices: []model.Device{
			{ID: "fan0", Name: "A", Capabilities: map[string]model.Capability{"power": {Kind: "switch"}}},
		},
	}
	removed, _ = s.UpsertManifest("t1", m2)
	if len(removed) != 1 || removed[0] != "fan1" {
		t.Fatalf("removed=%v", removed)
	}
	if _, ok := s.State("t1", "fan1"); ok {
		t.Fatal("fan1 should be removed")
	}
}

func TestDeviceStateAndStatus(t *testing.T) {
	s := New()
	s.UpsertStatus("t1", &model.Status{Online: true, Thing: "t1", Version: "1.0.0"})
	s.UpsertState("t1", "fan0", &model.DeviceState{
		Thing:    "t1",
		DeviceID: "fan0",
		Reported: map[string]any{"power": false, "updatedAt": 10},
	})
	sh, ok := s.State("t1", "fan0")
	if !ok || sh.Reported["power"] != false {
		t.Fatal("state not stored")
	}
	// A fresh report clears the stale flag set by the boot seed.
	s.MarkStale("t1", "fan0")
	if sh, _ := s.State("t1", "fan0"); !sh.Stale {
		t.Fatal("stale flag not set")
	}
	s.SetReported("t1", "fan0", map[string]any{"power": true})
	sh, _ = s.State("t1", "fan0")
	if sh.Stale {
		t.Fatal("a fresh report must clear the stale flag")
	}
	if sh.Reported["power"] != true {
		t.Fatal("reported not updated")
	}
	// Presence flips only the online flag and reports the transition.
	if !s.SetOnline("t1", false) {
		t.Fatal("SetOnline should report the transition")
	}
	if s.SetOnline("t1", false) {
		t.Fatal("SetOnline should be a no-op when nothing changes")
	}
	t1, ok := s.Get("t1")
	if !ok || t1.Status == nil || t1.Status.Online {
		t.Fatal("online flag not stored")
	}
	// Snapshot must be a deep copy: flipping the returned value must not touch
	// the store.
	s.SetOnline("t1", true)
	t1.Status.Online = false
	t2, _ := s.Get("t1")
	if !t2.Status.Online {
		t.Fatal("snapshot/get leaked mutable state")
	}
}

func TestRemoveThing(t *testing.T) {
	s := New()
	s.UpsertStatus("t1", &model.Status{Online: true, Thing: "t1"})
	s.RemoveThing("t1")
	if _, ok := s.Get("t1"); ok {
		t.Fatal("thing not removed")
	}
}
