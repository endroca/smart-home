package eventbus

import "testing"

func TestPublishSubscribe(t *testing.T) {
	b := New()
	ch, cancel := b.Subscribe()

	b.PublishData("tuya", map[string]any{"device": "d1"})

	select {
	case ev := <-ch:
		if ev.Type != "tuya" || ev.Data["device"] != "d1" {
			t.Fatalf("unexpected event: %+v", ev)
		}
		if ev.At.IsZero() {
			t.Fatal("event timestamp not set")
		}
	default:
		t.Fatal("event was not delivered")
	}

	cancel()
	// Publishing after cancel must not panic or block.
	b.PublishData("tuya", map[string]any{"device": "d2"})

	// A subscriber that never reads must not block the publisher.
	_, cancel2 := b.Subscribe()
	defer cancel2()
	for i := 0; i < 1000; i++ {
		b.PublishData("tuya", map[string]any{"i": i})
	}
}
