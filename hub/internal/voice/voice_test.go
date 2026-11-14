package voice

import (
	"context"
	"strings"
	"testing"

	"smart_home/hub/internal/db"
)

func TestParse(t *testing.T) {
	cases := []struct {
		text string
		want Command
	}{
		{"Liga o ventilador da sala", Command{Action: ActionPower, On: true, Noun: "fan"}},
		{"desliga o ventilador", Command{Action: ActionPower, On: false, Noun: "fan"}},
		{"acende a lâmpada", Command{Action: ActionLight, On: true, Noun: "light"}},
		{"apaga a luz", Command{Action: ActionLight, On: false, Noun: "light"}},
		{"liga a tomada", Command{Action: ActionPower, On: true, Noun: "socket"}},
		{"ventilador na velocidade 3", Command{Action: ActionSpeed, Speed: 3, Noun: "fan"}},
		{"coloca o ventilador no 2", Command{Action: ActionSpeed, Speed: 2, Noun: "fan"}},
		{"modo boa noite", Command{Action: ActionScene}},
		{"bom dia", Command{Action: ""}},
		// Frases reais do operador (infinitivo).
		{"ligar luz do escritorio", Command{Action: ActionLight, On: true, Noun: "light"}},
		{"desligar luz do escritorio", Command{Action: ActionLight, On: false, Noun: "light"}},
		{"ligar luz da sala", Command{Action: ActionLight, On: true, Noun: "light"}},
		{"ligar tomada do quarto", Command{Action: ActionPower, On: true, Noun: "socket"}},
		{"ligar ventilador na velocidade 3", Command{Action: ActionSpeed, Speed: 3, Noun: "fan"}},
		// Transcricao real do Whisper: pontuacao e "Luiz" por "luz".
		{"Ligado, Luiz do Quarto.", Command{Action: ActionLight, On: true, Noun: "light"}},
		{"Desligado, Luiz da Sala.", Command{Action: ActionLight, On: false, Noun: "light"}},
		{"Acender a luz do quarto!", Command{Action: ActionLight, On: true, Noun: "light"}},
	}
	for _, c := range cases {
		got := Parse(c.text)
		if got.Action != c.want.Action || got.On != c.want.On || got.Noun != c.want.Noun {
			t.Errorf("Parse(%q) = %+v, want action=%q on=%v noun=%q", c.text, got, c.want.Action, c.want.On, c.want.Noun)
			continue
		}
		if c.want.Action == ActionSpeed && got.Speed != c.want.Speed {
			t.Errorf("Parse(%q) speed = %d, want %d", c.text, got.Speed, c.want.Speed)
		}
	}
}

func testCatalog() *Catalog {
	return &Catalog{
		Rooms: []db.Room{{ID: "sala", Name: "Sala"}, {ID: "quarto", Name: "Quarto"}},
		Fans: []db.Device{
			{ID: "gw/fan0", Kind: db.KindFan, Owner: "gw", DeviceID: "fan0", Name: "Ventilador", RoomID: "sala"},
			{ID: "gw/fan1", Kind: db.KindFan, Owner: "gw", DeviceID: "fan1", Name: "Ventilador 2", RoomID: "quarto"},
		},
		Scenes: []Scene{{ID: "auto-1", Name: "Boa noite"}},
	}
}

func TestResolveFanPowerByRoom(t *testing.T) {
	s := New(nil, nil, nil, nil, nil)
	plan, cands := s.resolve(context.Background(), Parse("liga o ventilador do quarto"), testCatalog())
	if plan == nil || len(cands) != 0 {
		t.Fatalf("plan=%+v cands=%+v", plan, cands)
	}
	if plan.Kind != KindFan || plan.Thing != "gw" || plan.DeviceID != "fan1" {
		t.Fatalf("unexpected plan: %+v", plan)
	}
	if plan.State["speed"] != 1 {
		t.Fatalf("want speed 1: %+v", plan.State)
	}
}

func TestResolveAmbiguousFan(t *testing.T) {
	cat := &Catalog{Fans: []db.Device{
		{ID: "gw/fan0", Kind: db.KindFan, Owner: "gw", DeviceID: "fan0", Name: "Ventilador Sala", RoomID: "sala"},
		{ID: "gw/fan1", Kind: db.KindFan, Owner: "gw", DeviceID: "fan1", Name: "Ventilador Quarto", RoomID: "quarto"},
	}}
	s := New(nil, nil, nil, nil, nil)
	plan, cands := s.resolve(context.Background(), Parse("liga o ventilador"), cat)
	if plan != nil || len(cands) != 2 {
		t.Fatalf("expected 2 candidates, plan=%+v cands=%+v", plan, cands)
	}
}

func TestResolveSceneByName(t *testing.T) {
	s := New(nil, nil, nil, nil, nil)
	plan, _ := s.resolve(context.Background(), Parse("modo boa noite"), testCatalog())
	if plan == nil || plan.Kind != KindScene || plan.Scene != "auto-1" {
		t.Fatalf("unexpected plan: %+v", plan)
	}
}

func TestResolveSpeed(t *testing.T) {
	cat := &Catalog{Fans: []db.Device{{ID: "gw/fan0", Kind: db.KindFan, Owner: "gw", DeviceID: "fan0", Name: "Ventilador"}}}
	s := New(nil, nil, nil, nil, nil)
	plan, _ := s.resolve(context.Background(), Parse("velocidade 4"), cat)
	if plan == nil || plan.Action != ActionSpeed || plan.State["speed"] != 4 {
		t.Fatalf("unexpected plan: %+v", plan)
	}
}

type fakeTuya struct{ specs []db.Spec }

func (f fakeTuya) Control(ctx context.Context, id, dp string, value any) (map[string]any, error) {
	return map[string]any{"dp": dp, "value": value}, nil
}

func (f fakeTuya) Specs(ctx context.Context, id string) ([]db.Spec, error) { return f.specs, nil }

func TestResolveTuyaPower(t *testing.T) {
	cat := &Catalog{Tuya: []db.Device{{ID: "tuya-1", Kind: db.KindTuya, Name: "Tomada"}}}
	tuya := fakeTuya{specs: []db.Spec{{DPID: "1", Code: "switch_1", Type: "Boolean", Writable: true}}}
	s := New(nil, nil, nil, tuya, nil)
	plan, _ := s.resolve(context.Background(), Parse("liga a tomada"), cat)
	if plan == nil || plan.Kind != KindTuya || plan.DP != "1" || plan.Value != true {
		t.Fatalf("unexpected plan: %+v", plan)
	}
}

type fakeStore struct {
	rooms []db.Room
	fans  []db.Device
}

func (f fakeStore) ListRooms(ctx context.Context) ([]db.Room, error) { return f.rooms, nil }

func (f fakeStore) ListDevices(ctx context.Context, kind string) ([]db.Device, error) {
	if kind == db.KindFan {
		return f.fans, nil
	}
	return nil, nil
}

type fakeFans struct{ got map[string]any }

func (f *fakeFans) SetState(ctx context.Context, thing, deviceID string, state map[string]any, force bool) (map[string]any, error) {
	f.got = state
	return map[string]any{"mode": "test"}, nil
}

func TestHandleExecutesFan(t *testing.T) {
	fans := &fakeFans{}
	store := fakeStore{
		rooms: []db.Room{{ID: "sala", Name: "Sala"}},
		fans:  []db.Device{{ID: "gw/fan0", Kind: db.KindFan, Owner: "gw", DeviceID: "fan0", Name: "Ventilador", RoomID: "sala"}},
	}
	s := New(store, nil, fans, nil, nil)
	res, err := s.Handle(context.Background(), "liga o ventilador da sala", true)
	if err != nil {
		t.Fatal(err)
	}
	if res.Plan == nil || res.Result == nil {
		t.Fatalf("res=%+v", res)
	}
	if fans.got["speed"] != 1 {
		t.Fatalf("state=%+v", fans.got)
	}
	if res.Speech == "" {
		t.Fatal("expected a speech confirmation")
	}
}

func TestHandleNoMatch(t *testing.T) {
	s := New(nil, nil, nil, nil, nil)
	res, err := s.Handle(context.Background(), "faça um café", true)
	if err != nil {
		t.Fatal(err)
	}
	if res.Plan != nil || len(res.Candidates) != 0 {
		t.Fatalf("expected no plan, got %+v", res)
	}
}

// TestResolveUserPhrases locks in the operator's real phrasing against a
// catalog that mirrors the devices registered in the hub.
func TestResolveUserPhrases(t *testing.T) {
	cat := &Catalog{
		Rooms: []db.Room{
			{ID: "sala", Name: "Sala"},
			{ID: "quarto", Name: "Quarto"},
			{ID: "escritorio", Name: "Escritório"},
		},
		Fans: []db.Device{
			{ID: "rf01/fan0", Kind: db.KindFan, Owner: "rf01", DeviceID: "fan0", Name: "Ventilador escritório", RoomID: "escritorio"},
			{ID: "rf01/fan1", Kind: db.KindFan, Owner: "rf01", DeviceID: "fan1", Name: "Ventilador sala", RoomID: "sala"},
			{ID: "rf01/fan2", Kind: db.KindFan, Owner: "rf01", DeviceID: "fan2", Name: "Ventilador quarto", RoomID: "quarto"},
		},
		Tuya: []db.Device{
			{ID: "ar1", Kind: db.KindTuya, Name: "Ar-condicionado", RoomID: "quarto"},
			{ID: "tomada1", Kind: db.KindTuya, Name: "Tomada-PC", RoomID: "escritorio"},
		},
	}
	tuya := fakeTuya{specs: []db.Spec{{DPID: "1", Code: "switch_1", Type: "Boolean", Writable: true}}}
	s := New(nil, nil, nil, tuya, nil)
	ctx := context.Background()

	// "ligar luz do escritorio" → a luz do ventilador do escritório.
	plan, cands := s.resolve(ctx, Parse("ligar luz do escritorio"), cat)
	if plan == nil || len(cands) != 0 || plan.Kind != KindFan || plan.DeviceID != "fan0" || plan.State["light"] != true {
		t.Fatalf("escritorio light: plan=%+v cands=%+v", plan, cands)
	}

	// "desligar luz do escritorio" → mesma luz, apagada.
	plan, _ = s.resolve(ctx, Parse("desligar luz do escritorio"), cat)
	if plan == nil || plan.State["light"] != false {
		t.Fatalf("escritorio light off: plan=%+v", plan)
	}

	// "ligar luz da sala" → a luz do ventilador da sala.
	plan, _ = s.resolve(ctx, Parse("ligar luz da sala"), cat)
	if plan == nil || plan.Kind != KindFan || plan.DeviceID != "fan1" {
		t.Fatalf("sala light: plan=%+v", plan)
	}

	// "ligar tomada do quarto" → não há tomada no quarto: não inventa alvo.
	plan, cands = s.resolve(ctx, Parse("ligar tomada do quarto"), cat)
	if plan != nil || len(cands) != 0 {
		t.Fatalf("tomada do quarto should not match: plan=%+v cands=%+v", plan, cands)
	}

	// "ligar tomada do escritorio" → a Tomada-PC (Tuya), pelo nome.
	plan, _ = s.resolve(ctx, Parse("ligar tomada do escritorio"), cat)
	if plan == nil || plan.Kind != KindTuya || plan.Device != "tomada1" || plan.Value != true {
		t.Fatalf("tomada escritorio: plan=%+v", plan)
	}

	// "ligar ar-condicionado do quarto" → o Ar-condicionado (Tuya), pelo nome.
	plan, _ = s.resolve(ctx, Parse("ligar ar-condicionado do quarto"), cat)
	if plan == nil || plan.Kind != KindTuya || plan.Device != "ar1" {
		t.Fatalf("ar-condicionado: plan=%+v", plan)
	}

	// "ligar ventilador na velocidade 4" sem cômodo → 3 ventiladores: ambíguo.
	plan, cands = s.resolve(ctx, Parse("ligar ventilador na velocidade 4"), cat)
	if plan != nil || len(cands) != 3 {
		t.Fatalf("speed ambiguous: plan=%+v cands=%+v", plan, cands)
	}

	// Com o cômodo resolve sem ambiguidade.
	plan, _ = s.resolve(ctx, Parse("ligar ventilador na velocidade 4 do quarto"), cat)
	if plan == nil || plan.Action != ActionSpeed || plan.DeviceID != "fan2" || plan.State["speed"] != 4 {
		t.Fatalf("speed quarto: plan=%+v", plan)
	}
}

type fakeTranscriber struct {
	text   string
	prompt string
	audio  []byte
}

func (f *fakeTranscriber) Transcribe(ctx context.Context, audio []byte, filename, contentType, prompt string) (string, error) {
	f.audio = audio
	f.prompt = prompt
	return f.text, nil
}

func TestTranscribeAudioUsesCatalogPrompt(t *testing.T) {
	tr := &fakeTranscriber{text: "  ligar luz do escritório  "}
	store := fakeStore{
		rooms: []db.Room{{ID: "escritorio", Name: "Escritório"}},
		fans:  []db.Device{{ID: "gw/fan0", Kind: db.KindFan, Owner: "gw", DeviceID: "fan0", Name: "Ventilador escritório", RoomID: "escritorio"}},
	}
	s := New(store, nil, nil, nil, nil)
	s.SetTranscriber(tr, STTInfo{Provider: "groq", Model: "whisper-large-v3-turbo", Language: "pt"})

	text, err := s.TranscribeAudio(context.Background(), []byte("audio"), "comando.webm", "audio/webm")
	if err != nil {
		t.Fatal(err)
	}
	if text != "ligar luz do escritório" {
		t.Fatalf("text=%q", text)
	}
	if !strings.Contains(tr.prompt, "Escritório") || !strings.Contains(tr.prompt, "Ventilador escritório") {
		t.Fatalf("prompt missing catalog names: %q", tr.prompt)
	}
	if len(tr.audio) == 0 {
		t.Fatal("audio was not forwarded")
	}
	info := s.STTInfo()
	if !info.Available || info.Provider != "groq" || info.Model != "whisper-large-v3-turbo" {
		t.Fatalf("info=%+v", info)
	}
}

func TestSTTInfoUnavailableWithoutTranscriber(t *testing.T) {
	s := New(nil, nil, nil, nil, nil)
	if s.STTInfo().Available {
		t.Fatal("expected STT to be unavailable")
	}
}

func TestSTTPromptIsBounded(t *testing.T) {
	cat := &Catalog{}
	for i := 0; i < 200; i++ {
		cat.Fans = append(cat.Fans, db.Device{ID: "d", Name: strings.Repeat("a", 20)})
	}
	if got := len([]rune(sttPrompt(cat))); got > sttPromptMax {
		t.Fatalf("prompt length=%d, want <=%d", got, sttPromptMax)
	}
}

// TestSTTPromptPairsDeviceAndRoom locks the fix for devices whose names do not
// carry the room (three fans all named "Ventilador").
func TestSTTPromptPairsDeviceAndRoom(t *testing.T) {
	cat := &Catalog{
		Rooms: []db.Room{{ID: "sala", Name: "Sala"}, {ID: "quarto", Name: "Quarto"}},
		Fans: []db.Device{
			{ID: "gw/fan1", Kind: db.KindFan, Owner: "gw", DeviceID: "fan1", Name: "Ventilador", RoomID: "sala"},
			{ID: "gw/fan2", Kind: db.KindFan, Owner: "gw", DeviceID: "fan2", Name: "Ventilador", RoomID: "quarto"},
		},
	}
	prompt := sttPrompt(cat)
	for _, want := range []string{
		"Ventilador (Sala)",
		"Ventilador (Quarto)",
		"ligar luz da Sala",
		"ligar luz do Quarto",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("prompt missing %q:\n%s", want, prompt)
		}
	}
}

// TestResolveWhisperConfusionByRoom is the operator's real failure: Whisper
// heard "Ligado, Luiz do Quarto." and the room (not the device name) must pick
// the fan.
func TestResolveWhisperConfusionByRoom(t *testing.T) {
	cat := &Catalog{
		Rooms: []db.Room{
			{ID: "sala", Name: "Sala"},
			{ID: "quarto", Name: "Quarto"},
			{ID: "escritorio", Name: "Escritório"},
		},
		Fans: []db.Device{
			{ID: "rf01/fan0", Kind: db.KindFan, Owner: "rf01", DeviceID: "fan0", Name: "Ventilador", RoomID: "escritorio"},
			{ID: "rf01/fan1", Kind: db.KindFan, Owner: "rf01", DeviceID: "fan1", Name: "Ventilador", RoomID: "sala"},
			{ID: "rf01/fan2", Kind: db.KindFan, Owner: "rf01", DeviceID: "fan2", Name: "Ventilador", RoomID: "quarto"},
		},
	}
	s := New(nil, nil, nil, nil, nil)
	plan, cands := s.resolve(context.Background(), Parse("Ligado, Luiz do Quarto."), cat)
	if plan == nil || len(cands) != 0 {
		t.Fatalf("plan=%+v cands=%+v", plan, cands)
	}
	if plan.Kind != KindFan || plan.DeviceID != "fan2" || plan.State["light"] != true {
		t.Fatalf("unexpected plan: %+v", plan)
	}
}

func TestSystemPromptIncludesRoom(t *testing.T) {
	cat := &Catalog{
		Rooms: []db.Room{{ID: "quarto", Name: "Quarto"}},
		Fans:  []db.Device{{ID: "gw/fan2", Kind: db.KindFan, Owner: "gw", DeviceID: "fan2", Name: "Ventilador", RoomID: "quarto"}},
	}
	prompt := systemPrompt(cat)
	if !strings.Contains(prompt, "Ventilador [Quarto]") {
		t.Fatalf("system prompt missing the room:\n%s", prompt)
	}
	if !strings.Contains(prompt, "pode NÃO estar no nome") {
		t.Fatalf("system prompt does not warn about the room:\n%s", prompt)
	}
}
