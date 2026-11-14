package httpapi

import (
	"context"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"smart_home/hub/internal/voice"
)

// maxVoiceAudio caps one recorded command. A push-to-talk clip is a few
// hundred KB at most; the limit protects the Pi from a runaway upload.
const maxVoiceAudio = 10 << 20 // 10 MiB

// SetVoice attaches the voice intent service. When it is nil the endpoint is
// not served.
func (s *Server) SetVoice(v *voice.Service) { s.voice = v }

func (s *Server) voiceRoutes(mux *http.ServeMux) {
	if s.voice == nil {
		return
	}
	mux.HandleFunc("/api/voice/command", s.voiceCommand)
	mux.HandleFunc("/api/voice/config", s.voiceConfig)
}

// voiceConfig tells the client whether the hub can transcribe recorded audio,
// so it can pick between Whisper, the browser's Web Speech API and the text
// field. It never exposes the API key.
func (s *Server) voiceConfig(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":  true,
		"stt": s.voice.STTInfo(),
		"llm": map[string]any{"enabled": s.cfg.VoiceLLMConfigured()},
	})
}

// voiceCommand interprets one command and, unless execute is false, runs it.
// It accepts the two ways a client can produce one:
//   - application/json {text, execute}: the browser did the speech-to-text;
//   - multipart/form-data with an "audio" file: the hub transcribes it.
//
// After transcription both paths share the same parser and execution.
func (s *Server) voiceCommand(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	if strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/form-data") {
		s.voiceCommandAudio(w, r)
		return
	}

	var req struct {
		Text    string `json:"text"`
		Execute *bool  `json:"execute"`
	}
	if err := decode(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_payload", err.Error())
		return
	}
	if strings.TrimSpace(req.Text) == "" {
		writeError(w, http.StatusBadRequest, "invalid_payload", "text is required")
		return
	}
	execute := true
	if req.Execute != nil {
		execute = *req.Execute
	}
	s.runVoiceCommand(w, r, req.Text, execute)
}

// voiceCommandAudio turns an uploaded recording into a transcript and runs it.
func (s *Server) voiceCommandAudio(w http.ResponseWriter, r *http.Request) {
	if !s.voice.STTInfo().Available {
		writeError(w, http.StatusServiceUnavailable, "stt_unavailable", "voice transcription is not configured")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxVoiceAudio+4096)
	if err := r.ParseMultipartForm(maxVoiceAudio); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_payload", "audio upload too large or malformed")
		return
	}
	defer r.MultipartForm.RemoveAll()

	file, header, err := r.FormFile("audio")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_payload", "multipart field 'audio' is required")
		return
	}
	defer file.Close()
	audio, err := io.ReadAll(io.LimitReader(file, maxVoiceAudio))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_payload", "could not read the audio upload")
		return
	}
	if len(audio) == 0 {
		writeError(w, http.StatusBadRequest, "invalid_payload", "audio is empty")
		return
	}

	execute := true
	if v := strings.TrimSpace(r.FormValue("execute")); v != "" {
		if b, perr := strconv.ParseBool(v); perr == nil {
			execute = b
		}
	}

	ctx, cancel := context.WithTimeout(r.Context(), 45*time.Second)
	defer cancel()
	info := s.voice.STTInfo()
	started := time.Now()
	text, err := s.voice.TranscribeAudio(ctx, audio, header.Filename, header.Header.Get("Content-Type"))
	if err != nil {
		log.Printf("[voice] STT %s/%s falhou em %s: %v", info.Provider, info.Model, time.Since(started).Round(time.Millisecond), err)
		writeError(w, http.StatusBadGateway, "stt_failed", err.Error())
		return
	}
	if strings.TrimSpace(text) == "" {
		log.Printf("[voice] STT %s/%s: %d bytes em %s sem transcricao", info.Provider, info.Model, len(audio), time.Since(started).Round(time.Millisecond))
		writeError(w, http.StatusUnprocessableEntity, "no_speech", "não foi possível transcrever o áudio")
		return
	}
	log.Printf("[voice] STT %s/%s: %d bytes em %s -> %q", info.Provider, info.Model, len(audio), time.Since(started).Round(time.Millisecond), truncateForLog(text))
	s.runVoiceCommand(w, r, text, execute)
}

// truncateForLog keeps the transcript readable in journalctl without dumping
// an unbounded string.
func truncateForLog(text string) string {
	const max = 120
	runes := []rune(strings.TrimSpace(text))
	if len(runes) <= max {
		return string(runes)
	}
	return string(runes[:max]) + "…"
}

// runVoiceCommand resolves a transcript and writes the shared response shape.
func (s *Server) runVoiceCommand(w http.ResponseWriter, r *http.Request, text string, execute bool) {
	res, err := s.voice.Handle(r.Context(), text, execute)
	if err != nil {
		writeError(w, http.StatusBadGateway, "voice_failed", err.Error())
		return
	}
	out := map[string]any{
		"ok":         true,
		"transcript": res.Transcript,
	}
	if res.Plan != nil {
		out["plan"] = res.Plan
	}
	if res.Result != nil {
		out["result"] = res.Result
	}
	if res.Speech != "" {
		out["speech"] = res.Speech
	}
	if len(res.Candidates) > 0 {
		out["needs_disambiguation"] = true
		out["candidates"] = res.Candidates
	}
	writeJSON(w, http.StatusOK, out)
}
