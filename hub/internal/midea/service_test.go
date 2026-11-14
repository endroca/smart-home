package midea

import (
	"testing"

	"smart_home/hub/internal/midea/lan"
)

func TestSpecsFromCapabilities(t *testing.T) {
	base := specsFromCapabilities("1", lan.Capabilities{})
	keys := map[string]bool{}
	for _, spec := range base {
		keys[spec.ID] = true
	}
	for _, wanted := range []string{"power", "mode", "target_temp", "fan_speed"} {
		if !keys[wanted] {
			t.Fatalf("missing base spec %q", wanted)
		}
	}
	if keys["breeze"] || keys["airflow_angle"] || keys["purifier"] || keys["fresh_air"] || keys["clean"] {
		t.Fatalf("device without capabilities got optional specs: %+v", base)
	}

	full := specsFromCapabilities("1", lan.Capabilities{
		Breeze:     true,
		SwingAngle: true,
		Purifier:   true,
		FreshAir:   true,
		OutSilent:  true,
		Raw:        map[uint16][]byte{0x0039: {1}},
	})
	keys = map[string]bool{}
	for _, spec := range full {
		keys[spec.ID] = true
	}
	for _, wanted := range []string{"breeze", "airflow_angle", "purifier", "fresh_air", "out_silent", "clean"} {
		if !keys[wanted] {
			t.Fatalf("capable appliance missing spec %q", wanted)
		}
	}
}

func TestSpecsUseAdvertisedTemperatureRange(t *testing.T) {
	specs := specsFromCapabilities("1", lan.Capabilities{MinCoolTemp: 17, MaxCoolTemp: 30, MinHeatTemp: 16, MaxHeatTemp: 28})
	for _, spec := range specs {
		if spec.ID != "target_temp" {
			continue
		}
		if got := string(spec.Values); got != `{"max":28,"min":17,"step":1}` {
			t.Fatalf("target_temp options = %s", got)
		}
		return
	}
	t.Fatal("target_temp spec not found")
}

func TestApplyParsesLogicalState(t *testing.T) {
	// requestFor is the pure translation layer behind Apply; exercise it with
	// the same values the API/voice send.
	req, props := requestFor(map[string]any{
		"power":         true,
		"mode":          "heat",
		"target_temp":   float64(30.4),
		"fan_speed":     "auto",
		"swing_v":       true,
		"eco":           true,
		"breeze":        "breezeless",
		"airflow_angle": float64(50),
		"fresh_air":     "medium",
		"out_silent":    true,
		"clean":         true,
	})
	if req.Power == nil || !*req.Power {
		t.Fatal("power not parsed")
	}
	if req.Mode == nil || *req.Mode != lan.ModeHeat {
		t.Fatal("mode not parsed")
	}
	if req.TargetTemp == nil || *req.TargetTemp != 30 {
		t.Fatalf("target_temp not clamped/rounded: %v", req.TargetTemp)
	}
	if req.FanSpeed == nil || *req.FanSpeed != lan.FanAuto {
		t.Fatal("fan_speed not parsed")
	}
	if req.Eco == nil || !*req.Eco {
		t.Fatal("eco not parsed")
	}
	if props.Breeze == nil || *props.Breeze != lan.BreezeBreezeless {
		t.Fatal("breeze not parsed")
	}
	if props.SwingVAngle == nil || *props.SwingVAngle != lan.SwingAnglePos3 {
		t.Fatal("airflow_angle not parsed")
	}
	if props.FreshAir == nil || *props.FreshAir != lan.FreshAirMedium {
		t.Fatal("fresh_air not parsed")
	}
}
