package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"

	"smart_home/hub/internal/config"
	"smart_home/hub/internal/hub"
	"smart_home/hub/internal/voice"
)

// fakeTranscriber stands in for the Whisper client: it returns a fixed text so
// the HTTP layer can be tested without any network.
type fakeTranscriber struct {
	text string
}

func (f fakeTranscriber) Transcribe(ctx context.Context, audio []byte, filename, contentType, prompt string) (string, error) {
	return f.text, nil
}

func testVoiceServerWithSTT(t *testing.T, tr voice.Transcriber, info voice.STTInfo) (*httptest.Server, func()) {
	t.Helper()
	cfg := config.Config{HTTPAddr: ":0"}
	h, err := hub.New(cfg, nil)
	if err != nil {
		t.Fatal(err)
	}
	s := New(h, cfg, nil)
	svc := voice.New(nil, nil, nil, nil, nil)
	svc.SetTranscriber(tr, info)
	s.SetVoice(svc)
	srv := httptest.NewServer(s.Handler())
	return srv, func() { srv.Close() }
}

// postAudio builds a multipart upload the way the PWA does.
func postAudio(t *testing.T, url string) *http.Response {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	part, err := mw.CreateFormFile("audio", "comando.webm")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write([]byte("fake-opus-bytes")); err != nil {
		t.Fatal(err)
	}
	if err := mw.WriteField("execute", "false"); err != nil {
		t.Fatal(err)
	}
	if err := mw.Close(); err != nil {
		t.Fatal(err)
	}
	req, err := http.NewRequest(http.MethodPost, url+"/api/voice/command", &buf)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", mw.FormDataContentType())
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

func testVoiceServer(t *testing.T) (*httptest.Server, func()) {
	t.Helper()
	cfg := config.Config{HTTPAddr: ":0"}
	h, err := hub.New(cfg, nil)
	if err != nil {
		t.Fatal(err)
	}
	s := New(h, cfg, nil)
	s.SetVoice(voice.New(nil, nil, nil, nil, nil))
	srv := httptest.NewServer(s.Handler())
	return srv, func() {
		srv.Close()
	}
}

func postVoice(t *testing.T, url, body string) *http.Response {
	t.Helper()
	resp, err := http.Post(url+"/api/voice/command", "application/json", bytes.NewReader([]byte(body)))
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

func TestVoiceCommandValidation(t *testing.T) {
	srv, closeFn := testVoiceServer(t)
	defer closeFn()

	resp := postVoice(t, srv.URL, `{"text":""}`)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("empty text status=%d, want 400", resp.StatusCode)
	}
}

func TestVoiceCommandUnknownPhrase(t *testing.T) {
	srv, closeFn := testVoiceServer(t)
	defer closeFn()

	resp := postVoice(t, srv.URL, `{"text":"faça um café"}`)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status=%d, want 200", resp.StatusCode)
	}
	var body map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body["ok"] != true {
		t.Fatalf("ok=%v, want true", body["ok"])
	}
	if _, has := body["plan"]; has {
		t.Fatalf("unexpected plan for unknown phrase: %v", body["plan"])
	}
}

func TestVoiceCommandMethodNotAllowed(t *testing.T) {
	srv, closeFn := testVoiceServer(t)
	defer closeFn()

	resp, err := http.Get(srv.URL + "/api/voice/command")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("GET status=%d, want 405", resp.StatusCode)
	}
}

func TestVoiceCommandAudioTranscribesAndInterprets(t *testing.T) {
	srv, closeFn := testVoiceServerWithSTT(t,
		fakeTranscriber{text: "liga o ventilador da sala"},
		voice.STTInfo{Provider: "groq", Model: "whisper-large-v3-turbo", Language: "pt"},
	)
	defer closeFn()

	resp := postAudio(t, srv.URL)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status=%d, want 200", resp.StatusCode)
	}
	var body map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body["transcript"] != "liga o ventilador da sala" {
		t.Fatalf("transcript=%v", body["transcript"])
	}
}

func TestVoiceCommandAudioNoSpeech(t *testing.T) {
	srv, closeFn := testVoiceServerWithSTT(t, fakeTranscriber{text: "   "}, voice.STTInfo{Provider: "groq"})
	defer closeFn()

	resp := postAudio(t, srv.URL)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("status=%d, want 422", resp.StatusCode)
	}
	var body struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body.Error.Code != "no_speech" {
		t.Fatalf("error code=%q, want no_speech", body.Error.Code)
	}
}

func TestVoiceCommandAudioWithoutSTT(t *testing.T) {
	srv, closeFn := testVoiceServer(t)
	defer closeFn()

	resp := postAudio(t, srv.URL)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("status=%d, want 503", resp.StatusCode)
	}
}

func TestVoiceConfigHidesKey(t *testing.T) {
	srv, closeFn := testVoiceServerWithSTT(t,
		fakeTranscriber{text: "oi"},
		voice.STTInfo{Provider: "groq", Model: "whisper-large-v3-turbo", Language: "pt", URL: "https://api.groq.com/openai/v1"},
	)
	defer closeFn()

	resp, err := http.Get(srv.URL + "/api/voice/config")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status=%d, want 200", resp.StatusCode)
	}
	var body struct {
		STT struct {
			Available bool   `json:"available"`
			Provider  string `json:"provider"`
			Model     string `json:"model"`
			Language  string `json:"language"`
			URL       string `json:"url"`
		} `json:"stt"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if !body.STT.Available || body.STT.Provider != "groq" || body.STT.Model != "whisper-large-v3-turbo" || body.STT.Language != "pt" {
		t.Fatalf("stt=%+v", body.STT)
	}
	if body.STT.URL != "https://api.groq.com/openai/v1" {
		t.Fatalf("stt url=%q", body.STT.URL)
	}
}
