package voice

import "testing"

func TestParseAirTemperature(t *testing.T) {
	cases := map[string]int{
		"poe o ar em 22 graus":          22,
		"ar 24":                         24,
		"deixa o ar condicionado em 19": 19,
		"ar de 18 ate 24":               24,
	}
	for phrase, want := range cases {
		cmd := Parse(phrase)
		if cmd.Action != ActionClimate || cmd.Between == nil || *cmd.Between != want {
			t.Fatalf("%q: action=%q temp=%v, want climate %d", phrase, cmd.Action, cmd.Between, want)
		}
	}
}

func TestParseAirTemperatureFromSpokenNumber(t *testing.T) {
	// "dois quatro graus" reads as 24 only when digits are spelled one by one;
	// the rules keep to arabic digits, so this must not become a speed.
	cmd := Parse("coloca o ar em 26 graus")
	if cmd.Action != ActionClimate || cmd.Between == nil || *cmd.Between != 26 {
		t.Fatalf("action=%q temp=%v", cmd.Action, cmd.Between)
	}
}

func TestParseFanSpeedIsNotTemperature(t *testing.T) {
	cmd := Parse("ventilador velocidade 3")
	if cmd.Action != ActionSpeed || cmd.Speed != 3 {
		t.Fatalf("action=%q speed=%d", cmd.Action, cmd.Speed)
	}
}
