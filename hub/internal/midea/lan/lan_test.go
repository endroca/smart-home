package lan

import "testing"

func TestParseCapabilitiesBody(t *testing.T) {
	body := []byte{0xB5, 0x02, 0x43, 0x00, 0x01, 0x01, 0x25, 0x00, 0x01, 0x29, 0x00, 0x00}
	caps, err := parseCapabilitiesBody(body)
	if err != nil {
		t.Fatal(err)
	}
	if !caps.Breeze {
		t.Fatal("breeze flag not decoded from 0x0043")
	}
	if !caps.OutSilent {
		t.Fatal("out-silent flag not decoded from 0x0025")
	}
	if caps.Purifier || caps.FreshAir || caps.SwingAngle {
		t.Fatalf("unexpected flags: %+v", caps)
	}
}

func TestBuildSetCmdRoundTrip(t *testing.T) {
	msgID := uint8(1)
	frame := buildSetCmd(fullState{
		Power:      true,
		Mode:       ModeCool,
		TargetTemp: 24,
		FanSpeed:   FanAuto,
		SwingV:     true,
		Turbo:      true,
		Beep:       true,
	}, &msgID)
	if frame[0] != 0xAA {
		t.Fatalf("frame does not start with 0xAA: %x", frame[0])
	}
	bt, body, err := parseCommandFrame(frame)
	if err != nil {
		t.Fatal(err)
	}
	if bt != 0x40 {
		t.Fatalf("body type = 0x%02X, want 0x40", bt)
	}
	if (body[1] & 0x01) == 0 {
		t.Fatal("power bit not set")
	}
	if mode := Mode((body[2] & 0xE0) >> 5); mode != ModeCool {
		t.Fatalf("mode = %d, want cool", mode)
	}
	if temp := float64(body[2]&0x0F) + 16; temp != 24 {
		t.Fatalf("temperature = %v, want 24", temp)
	}
	if speed := FanSpeed(body[3] & 0x7F); speed != FanAuto {
		t.Fatalf("fan speed = %d, want auto", speed)
	}
}

func TestApplyRequestMergesPartialState(t *testing.T) {
	last := Response{Power: false, Mode: ModeCool, TargetTemp: 24, FanSpeed: FanLow, Eco: true}
	on, heat := true, ModeHeat
	state := applyRequest(last, Request{Power: &on, Mode: &heat})
	if !state.Power || state.Mode != ModeHeat {
		t.Fatalf("changes not applied: %+v", state)
	}
	if state.TargetTemp != 24 || state.FanSpeed != FanLow || !state.Eco {
		t.Fatalf("untouched fields lost: %+v", state)
	}
	if !state.Beep {
		t.Fatal("beep should default to on")
	}
}
