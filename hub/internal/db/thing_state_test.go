package db

import (
	"context"
	"testing"
)

func TestThingStateRoundTrip(t *testing.T) {
	s := openTemp(t)
	ctx := context.Background()

	reported := map[string]any{
		"power": true, "light": false, "speed": int64(3),
		"direction": "forward", "source": "rf_remote", "updatedAt": int64(1762286400),
	}
	if err := s.SetThingState(ctx, "gw", "fan0", reported, "rf_remote", 1762286400); err != nil {
		t.Fatal(err)
	}

	states, err := s.ThingStates(ctx, "gw")
	if err != nil {
		t.Fatal(err)
	}
	if len(states) != 1 {
		t.Fatalf("want 1 state, got %d", len(states))
	}
	st := states[0]
	if st.Thing != "gw" || st.DeviceID != "fan0" {
		t.Fatalf("unexpected identity: %s/%s", st.Thing, st.DeviceID)
	}
	if st.Source != "rf_remote" || st.DeviceTS != 1762286400 {
		t.Fatalf("unexpected metadata: source=%q deviceTS=%d", st.Source, st.DeviceTS)
	}
	if st.ReceivedAt.IsZero() {
		t.Fatal("received_at was not recorded")
	}
	if got, _ := st.Reported["power"].(bool); !got {
		t.Fatalf("power did not round trip: %#v", st.Reported["power"])
	}
	// JSON numbers decode as float64; the integer value must be preserved.
	if got, _ := st.Reported["speed"].(float64); got != 3 {
		t.Fatalf("speed did not round trip: %#v", st.Reported["speed"])
	}
}

func TestThingStateUpsertAndAll(t *testing.T) {
	s := openTemp(t)
	ctx := context.Background()

	if err := s.SetThingState(ctx, "gw", "fan0", map[string]any{"power": false}, "http", 1); err != nil {
		t.Fatal(err)
	}
	if err := s.SetThingState(ctx, "gw", "fan0", map[string]any{"power": true}, "http", 2); err != nil {
		t.Fatal(err)
	}
	if err := s.SetThingState(ctx, "other", "fan0", map[string]any{"power": true}, "", 0); err != nil {
		t.Fatal(err)
	}

	all, err := s.AllThingStates(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 2 {
		t.Fatalf("want 2 states across gateways, got %d", len(all))
	}

	one, err := s.ThingStates(ctx, "gw")
	if err != nil {
		t.Fatal(err)
	}
	if len(one) != 1 {
		t.Fatalf("want 1 state for gw, got %d", len(one))
	}
	if got, _ := one[0].Reported["power"].(bool); !got {
		t.Fatalf("upsert did not overwrite: %#v", one[0].Reported)
	}
	if one[0].DeviceTS != 2 {
		t.Fatalf("upsert did not refresh device_ts: %d", one[0].DeviceTS)
	}
}

func TestThingStateDelete(t *testing.T) {
	s := openTemp(t)
	ctx := context.Background()

	if err := s.SetThingState(ctx, "gw", "fan0", map[string]any{"power": true}, "", 0); err != nil {
		t.Fatal(err)
	}
	if err := s.SetThingState(ctx, "gw", "fan1", map[string]any{"power": true}, "", 0); err != nil {
		t.Fatal(err)
	}

	if err := s.DeleteThingState(ctx, "gw", "fan0"); err != nil {
		t.Fatal(err)
	}
	states, _ := s.ThingStates(ctx, "gw")
	if len(states) != 1 || states[0].DeviceID != "fan1" {
		t.Fatalf("single delete did not stick: %#v", states)
	}

	if err := s.DeleteThingStates(ctx, "gw"); err != nil {
		t.Fatal(err)
	}
	if states, _ := s.ThingStates(ctx, "gw"); len(states) != 0 {
		t.Fatalf("gateway delete did not stick: %#v", states)
	}
}

// Deleting a gateway must drop both its own cached state and its sub-devices'.
func TestDeleteDeviceDropsThingStates(t *testing.T) {
	s := openTemp(t)
	ctx := context.Background()

	if err := s.UpsertDevice(ctx, Device{ID: "gw", Kind: KindThing, Name: "Gateway"}); err != nil {
		t.Fatal(err)
	}
	if err := s.UpsertFan(ctx, "gw", "fan0", "Sala"); err != nil {
		t.Fatal(err)
	}
	if err := s.SetThingState(ctx, "gw", "fan0", map[string]any{"power": true}, "", 0); err != nil {
		t.Fatal(err)
	}

	if err := s.DeleteDevice(ctx, "gw"); err != nil {
		t.Fatal(err)
	}
	if states, _ := s.ThingStates(ctx, "gw"); len(states) != 0 {
		t.Fatalf("gateway deletion left states behind: %#v", states)
	}
}

// Deleting a single fan row must drop only its cached state.
func TestDeleteFanDropsThingState(t *testing.T) {
	s := openTemp(t)
	ctx := context.Background()

	if err := s.UpsertFan(ctx, "gw", "fan0", "Sala"); err != nil {
		t.Fatal(err)
	}
	if err := s.UpsertFan(ctx, "gw", "fan1", "Quarto"); err != nil {
		t.Fatal(err)
	}
	if err := s.SetThingState(ctx, "gw", "fan0", map[string]any{"power": true}, "", 0); err != nil {
		t.Fatal(err)
	}
	if err := s.SetThingState(ctx, "gw", "fan1", map[string]any{"power": false}, "", 0); err != nil {
		t.Fatal(err)
	}

	if err := s.DeleteFan(ctx, "gw", "fan0"); err != nil {
		t.Fatal(err)
	}
	states, _ := s.ThingStates(ctx, "gw")
	if len(states) != 1 || states[0].DeviceID != "fan1" {
		t.Fatalf("fan deletion removed the wrong state: %#v", states)
	}
}
