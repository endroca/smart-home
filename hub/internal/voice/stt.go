package voice

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"path/filepath"
	"strings"
	"time"
)

// Transcriber turns recorded audio into text. The client captures the audio;
// the hub forwards it to a speech-to-text backend. Keeping it behind an
// interface lets the parser tests run without any network.
type Transcriber interface {
	Transcribe(ctx context.Context, audio []byte, filename, contentType, prompt string) (string, error)
}

// STT is an OpenAI-compatible /audio/transcriptions client. Groq, OpenAI and
// OpenRouter all accept this multipart shape, and so does a local whisper.cpp
// server, so one implementation covers every provider.
type STT struct {
	url      string
	key      string
	model    string
	language string
	client   *http.Client
}

// NewSTT builds the transcription client. An empty language defaults to
// Portuguese, which is the hub's target language.
func NewSTT(url, key, model, language string) *STT {
	if language == "" {
		language = "pt"
	}
	return &STT{
		url:      strings.TrimRight(url, "/"),
		key:      key,
		model:    model,
		language: language,
		client:   &http.Client{Timeout: 30 * time.Second},
	}
}

// Transcribe posts the audio and returns the recognized text. The prompt is
// an optional hint (device/room/scene names) that helps the model spell the
// operator's vocabulary correctly.
func (s *STT) Transcribe(ctx context.Context, audio []byte, filename, contentType, prompt string) (string, error) {
	if s == nil || s.url == "" || s.model == "" {
		return "", errors.New("voice: STT is not configured")
	}
	if len(audio) == 0 {
		return "", errors.New("voice: empty audio")
	}
	if filename == "" {
		filename = audioFilename(contentType)
	}

	var body bytes.Buffer
	mw := multipart.NewWriter(&body)

	part, err := createFilePart(mw, filename, contentType)
	if err != nil {
		return "", err
	}
	if _, err := part.Write(audio); err != nil {
		return "", err
	}
	fields := [][2]string{
		{"model", s.model},
		{"language", s.language},
		{"response_format", "json"},
	}
	// Whisper accepts temperature 0 (the recommended value for commands); the
	// newer OpenAI transcribe models reject it, so only send it for Whisper.
	if strings.Contains(strings.ToLower(s.model), "whisper") {
		fields = append(fields, [2]string{"temperature", "0"})
	}
	for _, f := range fields {
		if err := mw.WriteField(f[0], f[1]); err != nil {
			return "", err
		}
	}
	if prompt != "" {
		if err := mw.WriteField("prompt", prompt); err != nil {
			return "", err
		}
	}
	if err := mw.Close(); err != nil {
		return "", err
	}

	endpoint := s.url
	if !strings.HasSuffix(endpoint, "/audio/transcriptions") {
		endpoint += "/audio/transcriptions"
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, &body)
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.Header.Set("Accept", "application/json")
	if s.key != "" {
		req.Header.Set("Authorization", "Bearer "+s.key)
	}

	resp, err := s.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode/100 != 2 {
		msg := strings.TrimSpace(string(raw))
		if len(msg) > 300 {
			msg = msg[:300]
		}
		return "", fmt.Errorf("voice: STT HTTP %d: %s", resp.StatusCode, msg)
	}
	return parseTranscript(raw)
}

// parseTranscript reads the JSON body, falling back to plain text for
// OpenAI-compatible servers that ignore response_format.
func parseTranscript(raw []byte) (string, error) {
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" {
		return "", errors.New("voice: STT returned an empty response")
	}
	if strings.HasPrefix(trimmed, "{") {
		var out struct {
			Text string `json:"text"`
		}
		if err := json.Unmarshal([]byte(trimmed), &out); err != nil {
			return "", fmt.Errorf("voice: STT response: %w", err)
		}
		return strings.TrimSpace(out.Text), nil
	}
	return trimmed, nil
}

// createFilePart writes the "file" form field with the real content type.
// multipart.CreateFormFile always sends application/octet-stream, and some
// providers sniff the type to pick the decoder.
func createFilePart(mw *multipart.Writer, filename, contentType string) (io.Writer, error) {
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	name := filepath.Base(filename)
	name = strings.NewReplacer("\"", "", "\r", "", "\n", "").Replace(name)
	h := make(textproto.MIMEHeader)
	h.Set("Content-Disposition", fmt.Sprintf(`form-data; name="file"; filename="%s"`, name))
	h.Set("Content-Type", contentType)
	return mw.CreatePart(h)
}

// audioFilename derives a filename from the recorded MIME type. Groq and
// OpenRouter both use the extension to pick the decoder.
func audioFilename(contentType string) string {
	ct := strings.ToLower(contentType)
	switch {
	case strings.Contains(ct, "webm"):
		return "comando.webm"
	case strings.Contains(ct, "mp4"), strings.Contains(ct, "m4a"), strings.Contains(ct, "aac"):
		return "comando.m4a"
	case strings.Contains(ct, "ogg"):
		return "comando.ogg"
	case strings.Contains(ct, "wav"):
		return "comando.wav"
	case strings.Contains(ct, "mpeg"), strings.Contains(ct, "mp3"):
		return "comando.mp3"
	case strings.Contains(ct, "flac"):
		return "comando.flac"
	default:
		return "comando.webm"
	}
}
