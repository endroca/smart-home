package voice

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"smart_home/hub/internal/db"
)

// LLM is an OpenAI-compatible chat-completions interpreter used as a fallback
// when the local rules cannot resolve a phrase. It is opt-in and never on the
// critical path: without it the hub is fully local.
//
// It implements Interpreter: it maps free-form text to the same Command shape,
// which the service then resolves against the real catalog and executes through
// the usual control paths.
type LLM struct {
	url    string
	key    string
	model  string
	client *http.Client
}

// NewLLM builds the fallback interpreter. An empty model defaults to a small,
// cheap one.
func NewLLM(url, key, model string) *LLM {
	if model == "" {
		model = "gpt-4o-mini"
	}
	return &LLM{url: url, key: key, model: model, client: &http.Client{Timeout: 12 * time.Second}}
}

// Command asks the model to turn text into an intent. It returns an error when
// the endpoint is unset or the response cannot be parsed, so the caller simply
// keeps the (empty) rules result.
func (l *LLM) Command(ctx context.Context, text string, cat *Catalog) (*Command, error) {
	if l == nil || l.url == "" {
		return nil, errors.New("voice: LLM is not configured")
	}
	reqBody := map[string]any{
		"model": l.model,
		"messages": []map[string]any{
			{"role": "system", "content": systemPrompt(cat)},
			{"role": "user", "content": text},
		},
		"tools": []map[string]any{{
			"type": "function",
			"function": map[string]any{
				"name":        "control_device",
				"description": "Converte um comando de voz em português na ação de controlar um dispositivo da casa.",
				"parameters":  toolSchema(),
			},
		}},
		"tool_choice": "required",
		"temperature": 0,
	}
	buf, err := json.Marshal(reqBody)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, l.url, bytes.NewReader(buf))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if l.key != "" {
		req.Header.Set("Authorization", "Bearer "+l.key)
	}
	resp, err := l.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode/100 != 2 {
		return nil, fmt.Errorf("voice: LLM HTTP %d", resp.StatusCode)
	}
	args, err := extractToolArgs(body)
	if err != nil {
		return nil, err
	}

	var out struct {
		Action string `json:"action"`
		Noun   string `json:"noun"`
		On     *bool  `json:"on"`
		Speed  *int   `json:"speed"`
		Temp   *int   `json:"temperature"`
		Room   string `json:"room"`
		Scene  string `json:"scene"`
		Name   string `json:"name"`
	}
	if err := json.Unmarshal(args, &out); err != nil {
		return nil, err
	}

	cmd := &Command{
		Action:   strings.ToLower(strings.TrimSpace(out.Action)),
		Noun:     strings.ToLower(strings.TrimSpace(out.Noun)),
		Room:     strings.TrimSpace(out.Room),
		Scene:    strings.TrimSpace(out.Scene),
		Name:     strings.TrimSpace(out.Name),
		Raw:      normalize(text),
		Provider: ProviderCloud,
	}
	if out.On != nil {
		cmd.On = *out.On
	}
	if out.Speed != nil {
		cmd.Speed = *out.Speed
	}
	if out.Temp != nil {
		cmd.Between = out.Temp
		if cmd.Action == ActionPower {
			cmd.Action = ActionClimate
		}
	}
	if cmd.Action == "" {
		return nil, errors.New("voice: empty LLM intent")
	}
	return cmd, nil
}

// extractToolArgs pulls the JSON arguments out of a chat-completions response,
// falling back to a JSON object embedded in the message content.
func extractToolArgs(body []byte) ([]byte, error) {
	var r struct {
		Choices []struct {
			Message struct {
				ToolCalls []struct {
					Function struct {
						Arguments string `json:"arguments"`
					} `json:"function"`
				} `json:"tool_calls"`
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(body, &r); err != nil {
		return nil, err
	}
	if len(r.Choices) == 0 {
		return nil, errors.New("voice: LLM returned no choices")
	}
	msg := r.Choices[0].Message
	if len(msg.ToolCalls) > 0 && strings.TrimSpace(msg.ToolCalls[0].Function.Arguments) != "" {
		return []byte(msg.ToolCalls[0].Function.Arguments), nil
	}
	if c := strings.TrimSpace(msg.Content); c != "" {
		if i := strings.IndexByte(c, '{'); i >= 0 {
			if j := strings.LastIndexByte(c, '}'); j > i {
				return []byte(c[i : j+1]), nil
			}
		}
	}
	return nil, errors.New("voice: LLM returned no tool call")
}

// systemPrompt gives the model the current inventory so it references real
// devices and rooms, which the resolver then matches back.
func systemPrompt(cat *Catalog) string {
	var b strings.Builder
	b.WriteString("Você converte um comando de voz em português na chamada da ferramenta control_device. ")
	b.WriteString("Use apenas os cômodos, dispositivos e cenas listados. ")
	b.WriteString("O cômodo de cada dispositivo aparece entre colchetes e pode NÃO estar no nome dele. ")
	b.WriteString("action: power (ligar/desligar), light (luz), speed (velocidade 0-6), scene (executar cena). ")
	b.WriteString("noun: fan, light, socket, air, ou vazio. Preencha room/name/scene apenas quando a frase os citar.\n")
	if len(cat.Rooms) > 0 {
		b.WriteString("Cômodos: ")
		for i, r := range cat.Rooms {
			if i > 0 {
				b.WriteString(", ")
			}
			b.WriteString(r.Name)
		}
		b.WriteString("\n")
	}
	writeDevices := func(label string, ds []db.Device) {
		if len(ds) == 0 {
			return
		}
		b.WriteString(label)
		b.WriteString(": ")
		for i, d := range ds {
			if i > 0 {
				b.WriteString("; ")
			}
			b.WriteString(labelOf(d))
			if room := roomName(d.RoomID, cat.Rooms); room != "" {
				b.WriteString(" [" + room + "]")
			}
			if d.ID != "" {
				b.WriteString(" (" + d.ID + ")")
			}
		}
		b.WriteString("\n")
	}
	writeDevices("Ventiladores", cat.Fans)
	writeDevices("Dispositivos Tuya", cat.Tuya)
	if len(cat.Scenes) > 0 {
		b.WriteString("Cenas: ")
		for i, sc := range cat.Scenes {
			if i > 0 {
				b.WriteString(", ")
			}
			b.WriteString(sc.Name)
		}
		b.WriteString("\n")
	}
	return b.String()
}

func toolSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"action":      map[string]any{"type": "string", "enum": []string{"power", "light", "speed", "climate", "scene"}},
			"noun":        map[string]any{"type": "string", "enum": []string{"fan", "light", "socket", "air", ""}},
			"on":          map[string]any{"type": "boolean"},
			"speed":       map[string]any{"type": "integer", "minimum": 0, "maximum": 6},
			"temperature": map[string]any{"type": "integer", "minimum": 16, "maximum": 30},
			"room":        map[string]any{"type": "string"},
			"scene":       map[string]any{"type": "string"},
			"name":        map[string]any{"type": "string"},
		},
		"required": []string{"action"},
	}
}
