package voice

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSTTTranscribeMultipart(t *testing.T) {
	var (
		gotModel    string
		gotLanguage string
		gotPrompt   string
		gotFilename string
		gotMime     string
		gotAudio    string
		gotAuth     string
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		if r.URL.Path != "/v1/audio/transcriptions" {
			t.Errorf("path=%q", r.URL.Path)
		}
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			t.Errorf("parse multipart: %v", err)
			return
		}
		gotModel = r.FormValue("model")
		gotLanguage = r.FormValue("language")
		gotPrompt = r.FormValue("prompt")
		file, header, err := r.FormFile("file")
		if err != nil {
			t.Errorf("form file: %v", err)
			return
		}
		defer file.Close()
		gotFilename = header.Filename
		gotMime = header.Header.Get("Content-Type")
		data, _ := io.ReadAll(file)
		gotAudio = string(data)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"text":"  ligar luz do escritório  "}`))
	}))
	defer srv.Close()

	s := NewSTT(srv.URL+"/v1", "secret", "whisper-large-v3-turbo", "pt")
	text, err := s.Transcribe(context.Background(), []byte("opus-bytes"), "comando.webm", "audio/webm", "Cômodos: Escritório")
	if err != nil {
		t.Fatal(err)
	}
	if text != "ligar luz do escritório" {
		t.Fatalf("text=%q", text)
	}
	if gotModel != "whisper-large-v3-turbo" || gotLanguage != "pt" || gotPrompt != "Cômodos: Escritório" {
		t.Fatalf("form fields: model=%q lang=%q prompt=%q", gotModel, gotLanguage, gotPrompt)
	}
	if gotFilename != "comando.webm" || gotMime != "audio/webm" || gotAudio != "opus-bytes" {
		t.Fatalf("file: name=%q mime=%q audio=%q", gotFilename, gotMime, gotAudio)
	}
	if gotAuth != "Bearer secret" {
		t.Fatalf("auth=%q", gotAuth)
	}
}

func TestSTTTranscribeHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":{"message":"invalid api key"}}`, http.StatusUnauthorized)
	}))
	defer srv.Close()

	s := NewSTT(srv.URL, "bad", "whisper-large-v3-turbo", "pt")
	_, err := s.Transcribe(context.Background(), []byte("audio"), "", "audio/webm", "")
	if err == nil || !strings.Contains(err.Error(), "401") {
		t.Fatalf("err=%v, want HTTP 401", err)
	}
}

func TestSTTTranscribePlainTextFallback(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("desligar o ventilador\n"))
	}))
	defer srv.Close()

	s := NewSTT(srv.URL, "", "local-whisper", "pt")
	text, err := s.Transcribe(context.Background(), []byte("audio"), "", "audio/wav", "")
	if err != nil {
		t.Fatal(err)
	}
	if text != "desligar o ventilador" {
		t.Fatalf("text=%q", text)
	}
}

func TestSTTTranscribeValidation(t *testing.T) {
	s := NewSTT("http://example.invalid", "k", "m", "pt")
	if _, err := s.Transcribe(context.Background(), nil, "", "audio/webm", ""); err == nil {
		t.Fatal("expected an error for empty audio")
	}
	var missing *STT
	if _, err := missing.Transcribe(context.Background(), []byte("a"), "", "", ""); err == nil {
		t.Fatal("expected an error for an unconfigured STT")
	}
}

func TestAudioFilename(t *testing.T) {
	cases := map[string]string{
		"audio/webm;codecs=opus": "comando.webm",
		"audio/mp4":              "comando.m4a",
		"audio/ogg":              "comando.ogg",
		"audio/wav":              "comando.wav",
		"audio/mpeg":             "comando.mp3",
		"":                       "comando.webm",
	}
	for ct, want := range cases {
		if got := audioFilename(ct); got != want {
			t.Errorf("audioFilename(%q)=%q, want %q", ct, got, want)
		}
	}
}

func TestParseTranscriptEmpty(t *testing.T) {
	if _, err := parseTranscript([]byte("   ")); err == nil {
		t.Fatal("expected an error for an empty body")
	}
	if _, err := parseTranscript([]byte(`{"text":`)); err == nil {
		t.Fatal("expected an error for invalid JSON")
	}
}

var _ Transcriber = (*STT)(nil)
