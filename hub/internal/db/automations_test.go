package db

import (
	"context"
	"testing"
)

// TestAutomationScheduleRoundTrip checks the schedule JSON survives the
// migration/upsert/read path and that rows created before the schedule column
// existed still read as an empty schedule.
func TestAutomationScheduleRoundTrip(t *testing.T) {
	s := openTemp(t)
	ctx := context.Background()

	if err := s.UpsertAutomation(ctx, Automation{
		ID:       "a1",
		Name:     "Porta",
		Enabled:  true,
		Once:     true,
		Source:   []byte(`{"type":"tuya_dp","device":"lock1","dps":["1","2"]}`),
		Schedule: []byte(`{"start":"18:00","end":"03:00","days":[5]}`),
		Delay:    []byte(`{"enabled":true,"ms":1500}`),
		Actions:  []byte(`[{"kind":"tuya","device":"lamp1","dp":"1","value":true}]`),
	}); err != nil {
		t.Fatal(err)
	}

	got, err := s.GetAutomation(ctx, "a1")
	if err != nil {
		t.Fatal(err)
	}
	if !got.Once {
		t.Fatal("once flag did not survive the round trip")
	}
	if string(got.Schedule) != `{"start":"18:00","end":"03:00","days":[5]}` {
		t.Fatalf("schedule = %s", got.Schedule)
	}
	if string(got.Delay) != `{"enabled":true,"ms":1500}` {
		t.Fatalf("delay = %s", got.Delay)
	}

	// An automation saved without a schedule gets the neutral document.
	if err := s.UpsertAutomation(ctx, Automation{
		ID:      "a2",
		Name:    "Cena",
		Enabled: true,
		Source:  []byte(`{"type":"manual"}`),
		Actions: []byte(`[{"kind":"fan","thing":"rf01","device_id":"fan0","state":{"light":true}}]`),
	}); err != nil {
		t.Fatal(err)
	}
	got, err = s.GetAutomation(ctx, "a2")
	if err != nil {
		t.Fatal(err)
	}
	if string(got.Schedule) != `{}` {
		t.Fatalf("default schedule = %s, want {}", got.Schedule)
	}
	if got.Once {
		t.Fatal("a default automation should not be volatile")
	}
}
