// Package voice turns a spoken-text transcript into a device command and runs
// it through the hub, the Tuya service and the automation dispatcher. It owns
// only the intent/execution layer: the microphone, wake word, STT and TTS live
// in the client (the PWA today, a dedicated satellite later), which just posts
// text to POST /api/voice/command.
//
// The parser is a small pt-BR rules engine so the hub stays usable offline; an
// optional cloud LLM (see llm.go) is consulted only when the rules cannot
// resolve a phrase and the operator opted in.
package voice

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"smart_home/hub/internal/db"
	"smart_home/hub/internal/functions"
)

// Action names understood by the voice service.
const (
	ActionPower   = "power"
	ActionLight   = "light"
	ActionSpeed   = "speed"
	ActionClimate = "climate"
	ActionScene   = "scene"
)

// Target kinds of a resolved plan.
const (
	KindFan   = "fan"
	KindTuya  = "tuya"
	KindMidea = "midea"
	KindScene = "scene"
)

// Where a command came from, for observability.
const (
	ProviderRules = "rules"
	ProviderCloud = "cloud"
)

// Command is a parsed, not yet resolved instruction.
type Command struct {
	Action  string // power | light | speed | climate | scene | ""
	On      bool
	Speed   int
	Between *int   // target temperature in °C, only set for the climate action
	Noun    string // fan | light | socket | air | ""
	Room    string // explicit room hint (name), only set by the LLM
	Scene   string // explicit scene hint, only set by the LLM
	Name    string // explicit device name/id hint, only set by the LLM
	Raw     string // normalized transcript
	// Provider is the source of the command; empty means the local rules.
	Provider string
}

func (c Command) provider() string {
	if c.Provider != "" {
		return c.Provider
	}
	return ProviderRules
}

// Candidate is one option offered when a phrase matches more than one device.
type Candidate struct {
	ID    string `json:"id"`
	Kind  string `json:"kind"`
	Label string `json:"label"`
	Room  string `json:"room,omitempty"`
}

// target is a device that matched a command, before it is turned into a Plan.
type target struct {
	kind string
	dev  db.Device
}

// Plan is a resolved, executable intent.
type Plan struct {
	Action     string         `json:"action"`
	Kind       string         `json:"kind"`
	Thing      string         `json:"thing,omitempty"`
	DeviceID   string         `json:"device_id,omitempty"` // sub-device of a gateway
	Device     string         `json:"device,omitempty"`    // Tuya/Midea device
	DP         string         `json:"dp,omitempty"`
	State      map[string]any `json:"state,omitempty"`
	Value      any            `json:"value,omitempty"`
	Scene      string         `json:"scene_id,omitempty"`
	Label      string         `json:"label"`
	Room       string         `json:"room,omitempty"`
	Speech     string         `json:"speech"`
	Confidence float64        `json:"confidence"`
	Provider   string         `json:"provider"`
}

// Result is the outcome of one voice command.
type Result struct {
	Transcript string
	Plan       *Plan
	Result     map[string]any
	Candidates []Candidate
	Speech     string
}

// Scene is a manual automation exposed as a voice scene.
type Scene struct {
	ID   string
	Name string
}

// Catalog is the current device/room/scene inventory used to resolve names.
type Catalog struct {
	Rooms  []db.Room
	Fans   []db.Device
	Tuya   []db.Device
	Midea  []db.Device
	Scenes []Scene
	// Functions is the effective function catalog of each device (labels the
	// voice matches against), keyed by device id. Empty when no function
	// service is attached.
	Functions map[string][]functions.Function
}

// Store reads the rooms and devices the resolver matches against.
type Store interface {
	ListRooms(ctx context.Context) ([]db.Room, error)
	ListDevices(ctx context.Context, kind string) ([]db.Device, error)
}

// SceneSource lists the manual automations exposed as scenes.
type SceneSource interface {
	Scenes(ctx context.Context) ([]Scene, error)
}

// FanSetter sets an absolute state on a fan. It is implemented by the hub.
type FanSetter interface {
	SetState(ctx context.Context, thing, deviceID string, state map[string]any, force bool) (map[string]any, error)
}

// TuyaController writes a data point and reads the cached specs. It is
// implemented by the Tuya service.
type TuyaController interface {
	Control(ctx context.Context, id, dp string, value any) (map[string]any, error)
	Specs(ctx context.Context, id string) ([]db.Spec, error)
}

// MideaController applies a state to a Midea appliance. It is implemented by
// the Midea service.
type MideaController interface {
	Apply(ctx context.Context, id string, state map[string]any) (map[string]any, error)
	Devices(ctx context.Context) ([]db.Device, error)
}

// SceneRunner fires a manual automation. It is implemented by the dispatcher.
type SceneRunner interface {
	Run(ctx context.Context, id string) (int, error)
}

// FunctionSource gives the effective function catalog of a device (the labels
// the voice matches and the "off" actions). It is implemented by the functions
// service.
type FunctionSource interface {
	Catalog(ctx context.Context, deviceID string) (*functions.Catalog, error)
}

// Interpreter is the optional natural-language fallback (a cloud LLM). It
// receives the transcript and the current catalog and returns a Command, which
// is then resolved and executed exactly like a locally parsed one.
type Interpreter interface {
	Command(ctx context.Context, text string, cat *Catalog) (*Command, error)
}

// STTInfo describes the configured transcription backend for the UI. It never
// carries the API key.
type STTInfo struct {
	Available bool   `json:"available"`
	Provider  string `json:"provider,omitempty"`
	Model     string `json:"model,omitempty"`
	Language  string `json:"language,omitempty"`
	// URL is the endpoint base the hub posts to (never the key). It makes the
	// provider auditable from the UI or a quick curl.
	URL string `json:"url,omitempty"`
}

// Service resolves and executes voice commands.
type Service struct {
	store     Store
	scenes    SceneSource
	fans      FanSetter
	tuya      TuyaController
	midea     MideaController
	runner    SceneRunner
	llm       Interpreter
	stt       Transcriber
	sttInfo   STTInfo
	functions FunctionSource
}

// New builds the voice service. Any dependency may be nil in tests.
func New(store Store, scenes SceneSource, fans FanSetter, tuya TuyaController, runner SceneRunner) *Service {
	return &Service{store: store, scenes: scenes, fans: fans, tuya: tuya, runner: runner}
}

// SetMidea attaches the Midea controller so "liga o ar" reaches an air
// conditioner.
func (s *Service) SetMidea(m MideaController) { s.midea = m }

// SetFunctions attaches the function catalog, so a phrase can match the labels
// the operator configured on a device instead of the built-in rules.
func (s *Service) SetFunctions(f FunctionSource) { s.functions = f }

// SetInterpreter attaches the optional cloud LLM fallback. When nil the service
// is fully local.
func (s *Service) SetInterpreter(i Interpreter) { s.llm = i }

// SetTranscriber attaches the speech-to-text backend that turns recorded audio
// into a transcript. When nil the hub only accepts text (the browser does the
// recognition, as before). info is exposed to the UI without the API key.
func (s *Service) SetTranscriber(t Transcriber, info STTInfo) {
	s.stt = t
	s.sttInfo = info
}

// STTInfo reports whether the hub can transcribe audio and with which backend.
func (s *Service) STTInfo() STTInfo {
	info := s.sttInfo
	info.Available = s.stt != nil
	return info
}

// TranscribeAudio converts one recorded command into text. The current
// device/room/scene inventory becomes a prompt so the model spells the
// operator's names correctly.
func (s *Service) TranscribeAudio(ctx context.Context, audio []byte, filename, contentType string) (string, error) {
	if s.stt == nil {
		return "", errors.New("voice: STT is not configured")
	}
	var prompt string
	if cat, err := s.loadCatalog(ctx); err == nil {
		prompt = sttPrompt(cat)
	}
	text, err := s.stt.Transcribe(ctx, audio, filename, contentType, prompt)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(text), nil
}

// Handle parses text, resolves it and (when execute is true) runs it. A nil
// Plan with candidates means the phrase was ambiguous; a nil Plan with no
// candidates means it was not understood.
func (s *Service) Handle(ctx context.Context, text string, execute bool) (*Result, error) {
	res := &Result{Transcript: strings.TrimSpace(text)}
	if res.Transcript == "" {
		return res, nil
	}
	cat, err := s.loadCatalog(ctx)
	if err != nil {
		return nil, err
	}

	// Reparar a transcricao antes de tudo: o Whisper devolve pontuacao e
	// confusoes ("Luiz" por "luz"), e o parser e o LLM devem ver a mesma frase.
	spoken := repairSpeech(normalize(res.Transcript))
	cmd := parseRaw(spoken)
	plan, cands := s.resolve(ctx, cmd, cat)

	// The cloud fallback only runs when the rules could not decide (neither a
	// plan nor a disambiguation).
	if plan == nil && len(cands) == 0 && s.llm != nil {
		if c, lerr := s.llm.Command(ctx, spoken, cat); lerr == nil && c != nil {
			c.Provider = ProviderCloud
			plan, cands = s.resolve(ctx, *c, cat)
		}
	}

	res.Candidates = cands
	if plan == nil {
		return res, nil
	}
	res.Plan = plan
	res.Speech = plan.Speech
	if !execute {
		return res, nil
	}
	out, err := s.execute(ctx, plan)
	if err != nil {
		return nil, err
	}
	res.Result = out
	return res, nil
}

func (s *Service) loadCatalog(ctx context.Context) (*Catalog, error) {
	cat := &Catalog{}
	if s.store != nil {
		rooms, err := s.store.ListRooms(ctx)
		if err != nil {
			return nil, err
		}
		cat.Rooms = rooms
		fans, err := s.store.ListDevices(ctx, db.KindFan)
		if err != nil {
			return nil, err
		}
		cat.Fans = fans
		tuyas, err := s.store.ListDevices(ctx, db.KindTuya)
		if err != nil {
			return nil, err
		}
		cat.Tuya = tuyas
		if s.midea != nil {
			air, err := s.midea.Devices(ctx)
			if err != nil {
				return nil, err
			}
			cat.Midea = air
		}
	}
	if s.scenes != nil {
		sc, err := s.scenes.Scenes(ctx)
		if err != nil {
			return nil, err
		}
		cat.Scenes = sc
	}
	if s.functions != nil {
		cat.Functions = map[string][]functions.Function{}
		for _, list := range [][]db.Device{cat.Fans, cat.Tuya, cat.Midea} {
			for _, d := range list {
				if _, ok := cat.Functions[d.ID]; ok {
					continue
				}
				// A missing/failed catalog must not break voice: the device just
				// falls back to the built-in rules.
				c, err := s.functions.Catalog(ctx, d.ID)
				if err != nil {
					continue
				}
				cat.Functions[d.ID] = c.Functions
			}
		}
	}
	return cat, nil
}

// resolve turns a Command into a Plan against the catalog, or returns the
// candidates that make it ambiguous.
func (s *Service) resolve(ctx context.Context, cmd Command, cat *Catalog) (*Plan, []Candidate) {
	switch cmd.Action {
	case ActionPower, ActionLight, ActionSpeed, ActionClimate:
		// Labels first: a configured function name wins over the built-in
		// rules, so voice depends on what the operator named, not on the hub.
		if p, cands := s.resolveByLabel(cmd, cat); p != nil || len(cands) > 0 {
			return p, cands
		}
		// A named scene wins only when no device noun was spoken, so a scene
		// called "Sala" never shadows "liga a luz da sala".
		if cmd.Noun == "" && cmd.Action != ActionSpeed {
			if sc := matchScene(cmd.Raw, cat.Scenes); sc != nil {
				return scenePlan(*sc, cmd.provider()), nil
			}
		}
		return s.devicePlan(ctx, cmd, cat)
	case ActionScene, "":
		if sc := matchScene(cmd.Raw, cat.Scenes); sc != nil {
			return scenePlan(*sc, cmd.provider()), nil
		}
		if cmd.Scene != "" {
			if sc := sceneByName(cmd.Scene, cat.Scenes); sc != nil {
				return scenePlan(*sc, cmd.provider()), nil
			}
		}
		return nil, nil
	}
	return nil, nil
}

// resolveByLabel matches the phrase against the labels and aliases the operator
// configured on each function. The most specific (longest) match wins; when two
// devices match equally it reports the candidates instead of guessing.
func (s *Service) resolveByLabel(cmd Command, cat *Catalog) (*Plan, []Candidate) {
	if len(cat.Functions) == 0 {
		return nil, nil
	}
	roomID, _ := roomOf(cmd, cat.Rooms)

	type hit struct {
		kind  string
		dev   db.Device
		fn    functions.Function
		score int
		value any
	}
	var hits []hit

	consider := func(kind string, d db.Device) {
		if roomID != "" && d.RoomID != roomID {
			return
		}
		for _, fn := range cat.Functions[d.ID] {
			if !fn.Voice {
				continue
			}
			score := 0
			for _, name := range append([]string{fn.Label}, fn.Aliases...) {
				n := normalize(name)
				if n != "" && containsWord(cmd.Raw, n) && len(n) > score {
					score = len(n)
				}
			}
			if score == 0 {
				continue
			}
			value, ok := functionValue(cmd, fn)
			if !ok {
				continue
			}
			hits = append(hits, hit{kind: kind, dev: d, fn: fn, score: score, value: value})
		}
	}

	for _, d := range cat.Fans {
		consider(KindFan, d)
	}
	for _, d := range cat.Tuya {
		consider(KindTuya, d)
	}
	for _, d := range cat.Midea {
		consider(KindMidea, d)
	}
	if len(hits) == 0 {
		return nil, nil
	}

	best := 0
	for _, h := range hits {
		if h.score > best {
			best = h.score
		}
	}
	var top []hit
	devices := map[string]bool{}
	for _, h := range hits {
		if h.score != best {
			continue
		}
		top = append(top, h)
		devices[h.dev.ID] = true
	}
	if len(devices) > 1 {
		out := make([]Candidate, 0, len(devices))
		seen := map[string]bool{}
		for _, h := range top {
			if seen[h.dev.ID] {
				continue
			}
			seen[h.dev.ID] = true
			out = append(out, Candidate{
				ID:    h.dev.ID,
				Kind:  h.kind,
				Label: labelOf(h.dev),
				Room:  roomName(h.dev.RoomID, cat.Rooms),
			})
		}
		return nil, out
	}
	// One device: pick the function with the most specific label.
	pick := top[0]
	for _, h := range top[1:] {
		if len(normalize(h.fn.Label)) > len(normalize(pick.fn.Label)) {
			pick = h
		}
	}
	return s.functionPlan(pick.kind, pick.dev, pick.fn, pick.value, cat), nil
}

// functionValue derives the value to set from the parsed command, or reports
// that the phrase does not carry enough to act.
func functionValue(cmd Command, fn functions.Function) (any, bool) {
	switch fn.Kind {
	case functions.KindBool:
		if cmd.Action == ActionPower || cmd.Action == ActionLight {
			return cmd.On, true
		}
		return nil, false
	case functions.KindNumber:
		switch {
		case cmd.Action == ActionSpeed:
			return float64(cmd.Speed), true
		case cmd.Action == ActionClimate && cmd.Between != nil:
			return float64(*cmd.Between), true
		default:
			if n, ok := firstNumber(cmd.Raw); ok {
				return float64(n), true
			}
			return nil, false
		}
	case functions.KindEnum:
		for _, v := range fn.Values {
			if v != "" && containsWord(cmd.Raw, normalize(v)) {
				return v, true
			}
		}
		return nil, false
	default:
		return nil, false
	}
}

// firstNumber returns the first plain number in the phrase ("modo 2", "leve 3").
func firstNumber(raw string) (int, bool) {
	for _, w := range strings.Fields(raw) {
		if n, ok := wordNumber(w); ok {
			return n, true
		}
	}
	return 0, false
}

// functionPlan turns a matched function into an executable plan, reusing the
// same execution paths as the built-in rules.
func (s *Service) functionPlan(kind string, d db.Device, fn functions.Function, value any, cat *Catalog) *Plan {
	label := labelOf(d)
	p := &Plan{
		Action:     "set",
		Kind:       kind,
		Label:      label,
		Room:       roomName(d.RoomID, cat.Rooms),
		Confidence: 0.9,
		Provider:   ProviderRules,
	}
	switch kind {
	case KindFan:
		p.Thing = d.Owner
		p.DeviceID = d.DeviceID
		if fn.Key == "power" {
			if on, ok := value.(bool); ok && on {
				// The RF remote has no "power on": turning on means choosing a
				// speed.
				p.State = map[string]any{"speed": 1}
				p.Speech = speechPower(label, true)
				return p
			}
		}
		p.State = map[string]any{fn.Key: value}
	case KindTuya:
		p.Device = d.ID
		p.DP = fn.Key
		p.Value = value
	case KindMidea:
		p.Device = d.ID
		p.State = map[string]any{fn.Key: value}
	}
	p.Speech = speechFunction(label, fn, value)
	return p
}

// speechFunction builds a short spoken confirmation for a function command.
func speechFunction(label string, fn functions.Function, value any) string {
	if b, ok := value.(bool); ok {
		if fn.Key == "light" {
			return speechLight(label, b)
		}
		return speechPower(label, b)
	}
	if n, ok := value.(float64); ok {
		return fmt.Sprintf("%s: %s %g", label, fn.Label, n)
	}
	return fmt.Sprintf("%s: %s %v", label, fn.Label, value)
}

func (s *Service) devicePlan(ctx context.Context, cmd Command, cat *Catalog) (*Plan, []Candidate) {
	roomID, _ := roomOf(cmd, cat.Rooms)

	var targets []target

	// Speed is fan-only.
	if cmd.Action == ActionSpeed {
		for _, d := range roomFilter(cat.Fans, roomID) {
			targets = append(targets, target{KindFan, d})
		}
		return s.pickPlan(ctx, targets, cmd, cat)
	}

	if cmd.Noun == "" || cmd.Noun == "fan" || cmd.Noun == "light" {
		for _, d := range roomFilter(cat.Fans, roomID) {
			targets = append(targets, target{KindFan, d})
		}
	}
	if cmd.Noun == "" || cmd.Noun == "socket" || cmd.Noun == "air" || cmd.Noun == "light" {
		for _, d := range roomFilter(cat.Tuya, roomID) {
			if s.tuyaMatches(ctx, cmd.Noun, d) {
				targets = append(targets, target{KindTuya, d})
			}
		}
	}
	// An air conditioner is always an air conditioner: the appliance kind
	// says so, unlike Tuya, where the registered name has to describe it.
	if cmd.Noun == "" || cmd.Noun == "air" {
		for _, d := range roomFilter(cat.Midea, roomID) {
			targets = append(targets, target{KindMidea, d})
		}
	}

	// An explicit name hint (only the LLM sets it) wins outright.
	if cmd.Name != "" {
		hint := normalize(cmd.Name)
		var hinted []target
		for _, t := range targets {
			if normalize(t.dev.ID) == hint || normalize(t.dev.Name) == hint {
				hinted = append(hinted, t)
			}
		}
		if len(hinted) > 0 {
			return s.pickPlan(ctx, hinted, cmd, cat)
		}
	}

	// Several candidates: try to break the tie with a distinctive word of a
	// device name spoken in the transcript.
	if len(targets) > 1 {
		var named []target
		for _, t := range targets {
			if nameMatches(t.dev, cmd.Raw) {
				named = append(named, t)
			}
		}
		if len(named) > 0 {
			targets = named
		}
	}
	return s.pickPlan(ctx, targets, cmd, cat)
}

// pickPlan turns the matched targets into a plan, or reports the ambiguity.
func (s *Service) pickPlan(ctx context.Context, targets []target, cmd Command, cat *Catalog) (*Plan, []Candidate) {
	switch {
	case len(targets) == 0:
		return nil, nil
	case len(targets) > 1:
		out := make([]Candidate, 0, len(targets))
		for _, t := range targets {
			out = append(out, Candidate{
				ID:    t.dev.ID,
				Kind:  t.kind,
				Label: labelOf(t.dev),
				Room:  roomName(t.dev.RoomID, cat.Rooms),
			})
		}
		return nil, out
	default:
		return s.planOne(ctx, targets[0].kind, targets[0].dev, cmd, cat)
	}
}

// tuyaMatches filters a Tuya device by the spoken noun. The hub has no
// capability schema for Tuya, so the registered name identifies the type: a
// "Tomada-PC" answers "tomada", an "Ar-condicionado" answers "ar".
func (s *Service) tuyaMatches(ctx context.Context, noun string, d db.Device) bool {
	switch noun {
	case "light":
		return s.tuyaIsLight(ctx, d)
	case "socket":
		return nameHas(d.Name, socketWords)
	case "air":
		return nameHas(d.Name, airWords)
	default: // "" (generic) accepts any Tuya device.
		return true
	}
}

// tuyaIsLight reports whether a Tuya device is a light: either its name says so
// or it exposes a light data point (switch_led, ...).
func (s *Service) tuyaIsLight(ctx context.Context, d db.Device) bool {
	if nameHas(d.Name, lightWords) {
		return true
	}
	if s.tuya == nil {
		return false
	}
	specs, err := s.tuya.Specs(ctx, d.ID)
	if err != nil {
		return false
	}
	for _, sp := range specs {
		if nameHas(sp.Code, lightWords) {
			return true
		}
	}
	return false
}

func (s *Service) fanPlan(d db.Device, cmd Command, cat *Catalog) *Plan {
	label := labelOf(d)
	p := &Plan{
		Kind: KindFan, Thing: d.Owner, DeviceID: d.DeviceID, Label: label,
		Room: roomName(d.RoomID, cat.Rooms), Confidence: 0.9, Provider: cmd.provider(),
	}
	switch cmd.Action {
	case ActionPower:
		p.Action = ActionPower
		if cmd.On {
			// O controle RF nao tem "ligar": ligar e escolher uma velocidade.
			p.State = map[string]any{"speed": 1}
		} else {
			p.State = map[string]any{"power": false}
		}
		p.Speech = speechPower(label, cmd.On)
	case ActionLight:
		p.Action = ActionLight
		p.State = map[string]any{"light": cmd.On}
		p.Speech = speechLight(label, cmd.On)
	case ActionSpeed:
		p.Action = ActionSpeed
		p.State = map[string]any{"speed": cmd.Speed}
		p.Speech = fmt.Sprintf("%s na velocidade %d", label, cmd.Speed)
	default:
		return nil
	}
	return p
}

func (s *Service) planOne(ctx context.Context, kind string, d db.Device, cmd Command, cat *Catalog) (*Plan, []Candidate) {
	switch kind {
	case KindFan:
		return s.fanPlan(d, cmd, cat), nil
	case KindTuya:
		if cmd.Action == ActionSpeed {
			return nil, nil
		}
		dp, err := s.switchDP(ctx, d.ID, cmd.Action == ActionLight)
		if err != nil || dp == "" {
			return nil, nil
		}
		label := labelOf(d)
		p := &Plan{
			Action: cmd.Action, Kind: KindTuya, Device: d.ID, DP: dp, Value: cmd.On,
			Label: label, Room: roomName(d.RoomID, cat.Rooms), Confidence: 0.85, Provider: cmd.provider(),
		}
		if cmd.Action == ActionLight {
			p.Speech = speechLight(label, cmd.On)
		} else {
			p.Speech = speechPower(label, cmd.On)
		}
		return p, nil
	case KindMidea:
		if cmd.Action == ActionSpeed && cmd.Between == nil {
			return nil, nil
		}
		label := labelOf(d)
		state := map[string]any{"power": cmd.On}
		p := &Plan{
			Action: cmd.Action, Kind: KindMidea, Device: d.ID, State: state,
			Label: label, Room: roomName(d.RoomID, cat.Rooms), Confidence: 0.9, Provider: cmd.provider(),
		}
		if cmd.Between != nil {
			p.Action = ActionClimate
			p.State = map[string]any{"power": true, "target_temp": cmd.Between}
			p.Speech = fmt.Sprintf("%s em %d graus", label, cmd.Between)
			return p, nil
		}
		p.Speech = speechPower(label, cmd.On)
		return p, nil
	}
	return nil, nil
}

// switchDP finds the writable boolean data point that represents the main
// switch (or the light) of a Tuya device, preferring the codes the UI uses.
func (s *Service) switchDP(ctx context.Context, id string, light bool) (string, error) {
	if s.tuya == nil {
		return "", nil
	}
	specs, err := s.tuya.Specs(ctx, id)
	if err != nil {
		return "", err
	}
	prefer := []string{"switch_1", "switch", "power", "switch_led"}
	if light {
		prefer = []string{"switch_led", "switch_1", "switch", "power"}
	}
	for _, code := range prefer {
		for _, sp := range specs {
			if strings.EqualFold(sp.Code, code) {
				return sp.DPID, nil
			}
		}
	}
	for _, sp := range specs {
		if sp.Writable && strings.EqualFold(sp.Type, "Boolean") {
			return sp.DPID, nil
		}
	}
	return "", nil
}

func (s *Service) execute(ctx context.Context, p *Plan) (map[string]any, error) {
	switch p.Kind {
	case KindFan:
		if s.fans == nil {
			return nil, errors.New("voice: fan control is not available")
		}
		return s.fans.SetState(ctx, p.Thing, p.DeviceID, p.State, false)
	case KindTuya:
		if s.tuya == nil {
			return nil, errors.New("voice: tuya control is not available")
		}
		return s.tuya.Control(ctx, p.Device, p.DP, p.Value)
	case KindMidea:
		if s.midea == nil {
			return nil, errors.New("voice: midea control is not available")
		}
		return s.midea.Apply(ctx, p.Device, p.State)
	case KindScene:
		if s.runner == nil {
			return nil, errors.New("voice: no automation runner is available")
		}
		n, err := s.runner.Run(ctx, p.Scene)
		if err != nil {
			return nil, err
		}
		return map[string]any{"actions": n}, nil
	}
	return nil, fmt.Errorf("voice: unknown target kind %q", p.Kind)
}

/* ---------- helpers de resolução ---------- */

func roomOf(cmd Command, rooms []db.Room) (string, string) {
	if cmd.Room != "" {
		if r := roomByName(cmd.Room, rooms); r != nil {
			return r.ID, r.Name
		}
	}
	for _, r := range rooms {
		n := normalize(r.Name)
		if len(n) >= 3 && containsWord(cmd.Raw, n) {
			return r.ID, r.Name
		}
	}
	return "", ""
}

func roomByName(name string, rooms []db.Room) *db.Room {
	n := normalize(name)
	for i := range rooms {
		if normalize(rooms[i].Name) == n || normalize(rooms[i].ID) == n {
			return &rooms[i]
		}
	}
	return nil
}

func roomName(id string, rooms []db.Room) string {
	for _, r := range rooms {
		if r.ID == id {
			return r.Name
		}
	}
	return ""
}

func matchScene(raw string, scenes []Scene) *Scene {
	var best *Scene
	bestLen := 0
	for i := range scenes {
		n := normalize(scenes[i].Name)
		if len(n) < 4 || !containsWord(raw, n) {
			continue
		}
		if len(n) > bestLen {
			best = &scenes[i]
			bestLen = len(n)
		}
	}
	return best
}

func sceneByName(name string, scenes []Scene) *Scene {
	n := normalize(name)
	for i := range scenes {
		if normalize(scenes[i].Name) == n || normalize(scenes[i].ID) == n {
			return &scenes[i]
		}
	}
	return nil
}

func scenePlan(sc Scene, provider string) *Plan {
	return &Plan{
		Action: ActionScene, Kind: KindScene, Scene: sc.ID, Label: sc.Name,
		Speech: "Executando " + sc.Name, Confidence: 0.95, Provider: provider,
	}
}

// roomFilter keeps the devices of one room (all of them when no room is known).
func roomFilter(devs []db.Device, roomID string) []db.Device {
	if roomID == "" {
		return devs
	}
	out := []db.Device{}
	for _, d := range devs {
		if d.RoomID == roomID {
			out = append(out, d)
		}
	}
	return out
}

// nameMatches reports whether the transcript speaks a distinctive part of a
// device's name. It only breaks a tie between candidates: a device named just
// "Ventilador" or "Luz" carries the type, not a name.
func nameMatches(d db.Device, raw string) bool {
	for _, tok := range nameTokens(d.Name) {
		if containsWord(raw, tok) {
			return true
		}
	}
	return false
}

// nameTokens lists the significant words of a device name: type words
// ("ventilador", "tomada", "ar") are dropped because they describe the class,
// not the device, and short words are too noisy to match.
func nameTokens(name string) []string {
	n := wordReplacer.Replace(normalize(name))
	var out []string
	for _, w := range strings.Fields(n) {
		if len(w) < 4 || nounOf(w) != "" {
			continue
		}
		out = append(out, w)
	}
	return out
}

func labelOf(d db.Device) string {
	switch {
	case d.Name != "":
		return d.Name
	case d.DeviceID != "":
		return d.DeviceID
	default:
		return d.ID
	}
}

// sttPromptMax bounds the transcription prompt. Whisper-style APIs cap prompts
// at 224 tokens; 600 characters stays comfortably below that in Portuguese.
const sttPromptMax = 600

// sttPrompt describes the house vocabulary so the transcription model spells
// registered names right and outputs commands in the expected shape. Room and
// device are always paired: the room is NOT part of the device name in the
// database, so "Ventilador" alone would be useless (and deduplicated).
func sttPrompt(cat *Catalog) string {
	var b strings.Builder
	b.WriteString("Comandos de voz em português do Brasil: ligar, desligar, acender, apagar. ")
	if examples := sttExamples(cat); len(examples) > 0 {
		b.WriteString("Exemplos: ")
		b.WriteString(strings.Join(examples, "; "))
		b.WriteString(". ")
	}
	if len(cat.Rooms) > 0 {
		b.WriteString("Cômodos: ")
		for i, r := range cat.Rooms {
			if i > 0 {
				b.WriteString(", ")
			}
			b.WriteString(r.Name)
		}
		b.WriteString(". ")
	}
	phrases := make([]string, 0, len(cat.Fans)+len(cat.Tuya)+len(cat.Midea))
	seen := map[string]bool{}
	for _, devices := range [][]db.Device{cat.Fans, cat.Tuya, cat.Midea} {
		for _, d := range devices {
			name := labelOf(d)
			if name == "" {
				continue
			}
			if room := roomName(d.RoomID, cat.Rooms); room != "" {
				name += " (" + room + ")"
			}
			if seen[name] {
				continue
			}
			seen[name] = true
			phrases = append(phrases, name)
		}
	}
	if len(phrases) > 0 {
		b.WriteString("Dispositivos: ")
		b.WriteString(strings.Join(phrases, ", "))
		b.WriteString(". ")
	}
	if names := allFunctionLabels(cat, 16); len(names) > 0 {
		b.WriteString("Funções: ")
		b.WriteString(strings.Join(names, ", "))
		b.WriteString(". ")
	}
	if len(cat.Scenes) > 0 {
		b.WriteString("Cenas: ")
		for i, sc := range cat.Scenes {
			if i > 0 {
				b.WriteString(", ")
			}
			b.WriteString(sc.Name)
		}
	}
	out := strings.TrimSpace(b.String())
	if runes := []rune(out); len(runes) > sttPromptMax {
		out = strings.TrimSpace(string(runes[:sttPromptMax]))
	}
	return out
}

// sttExamples primes Whisper with the imperative shape of real commands. A
// prompt that looks like a previous transcript works better than a prose
// description, and it prevents completions such as "Ligado, Luiz do Quarto".
func sttExamples(cat *Catalog) []string {
	var out []string
	for i, r := range cat.Rooms {
		if i >= 4 {
			break
		}
		out = append(out, "ligar luz "+roomArticle(r.Name)+" "+r.Name)
	}
	return out
}

// roomArticle picks "da" for feminine room names (ending in "a") and "do"
// otherwise: "da Sala", "do Quarto", "do Escritório".
func roomArticle(name string) string {
	if strings.HasSuffix(normalize(name), "a") {
		return "da"
	}
	return "do"
}

// functionLabels lists the distinct voice labels of a function catalog, capped
// at limit, so a prompt can prime the model with them.
func functionLabels(fns []functions.Function, limit int) []string {
	out := make([]string, 0, len(fns))
	seen := map[string]bool{}
	for _, f := range fns {
		if !f.Voice {
			continue
		}
		l := strings.TrimSpace(f.Label)
		if l == "" || seen[strings.ToLower(l)] {
			continue
		}
		seen[strings.ToLower(l)] = true
		out = append(out, l)
		if len(out) >= limit {
			break
		}
	}
	return out
}

// allFunctionLabels lists distinct voice labels across every device, capped at
// limit.
func allFunctionLabels(cat *Catalog, limit int) []string {
	out := make([]string, 0, limit)
	seen := map[string]bool{}
	for _, d := range append(append(append([]db.Device{}, cat.Fans...), cat.Tuya...), cat.Midea...) {
		for _, l := range functionLabels(cat.Functions[d.ID], limit) {
			k := strings.ToLower(l)
			if seen[k] {
				continue
			}
			seen[k] = true
			out = append(out, l)
			if len(out) >= limit {
				return out
			}
		}
	}
	return out
}

func speechPower(label string, on bool) string {
	if on {
		return "Ligando " + label
	}
	return "Desligando " + label
}

func speechLight(label string, on bool) string {
	if on {
		return "Luz de " + label + " ligada"
	}
	return "Luz de " + label + " apagada"
}
