package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLoadAndEnv(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "hub.json")
	if err := os.WriteFile(path, []byte(`{
		"http_addr": ":9090",
		"db": "hub.db"
	}`), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HUB_HTTP_ADDR", ":7070")
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.HTTPAddr != ":7070" {
		t.Fatalf("env override failed: %+v", cfg)
	}
	if cfg.Port() != 7070 {
		t.Fatalf("port=%d", cfg.Port())
	}
	if cfg.DB != "hub.db" {
		t.Fatalf("db=%q", cfg.DB)
	}
}

func TestTuyaAutoListenDefaultsAndEnv(t *testing.T) {
	cfg, err := Load("")
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.TuyaAutoListenEnabled() {
		t.Fatal("auto-listen should default on")
	}
	if cfg.TuyaRefresh() != 5*time.Second {
		t.Fatalf("default refresh=%s, want 5s", cfg.TuyaRefresh())
	}

	t.Setenv("TUYA_AUTO_LISTEN", "false")
	t.Setenv("TUYA_REFRESH_SECONDS", "12")
	cfg, err = Load("")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.TuyaAutoListenEnabled() {
		t.Fatal("env should disable auto-listen")
	}
	if cfg.TuyaRefresh() != 12*time.Second {
		t.Fatalf("env refresh=%s, want 12s", cfg.TuyaRefresh())
	}
}

func TestFirmwareStandardized(t *testing.T) {
	// firmware_dir / firmware_base_url are no longer read from the file.
	dir := t.TempDir()
	path := filepath.Join(dir, "hub.json")
	if err := os.WriteFile(path, []byte(`{
		"firmware_dir": "fw",
		"firmware_base_url": "http://example:1234"
	}`), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.FirmwareDir != "firmware" {
		t.Fatalf("firmware_dir should be the default, got %q", cfg.FirmwareDir)
	}
	if cfg.FirmwareBaseURL != "" {
		t.Fatalf("firmware_base_url should be empty, got %q", cfg.FirmwareBaseURL)
	}
}

func TestTuyaCredentials(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "hub.json")
	if err := os.WriteFile(path, []byte(`{
		"tuya_access_id": "id-file",
		"tuya_access_secret": "sec-file",
		"tuya_region": "eu"
	}`), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.TuyaAccessID != "id-file" || cfg.TuyaAccessSecret != "sec-file" || cfg.TuyaRegion != "eu" {
		t.Fatalf("credentials not read from file: %+v", cfg)
	}

	// The environment overrides the file.
	t.Setenv("TUYA_ACCESS_ID", "id-env")
	cfg, err = Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.TuyaAccessID != "id-env" || cfg.TuyaAccessSecret != "sec-file" {
		t.Fatalf("env override failed: %+v", cfg)
	}
}

func TestHubURL(t *testing.T) {
	cfg := Default()
	cfg.PublicURL = "http://hub.example:9000/"
	if got := cfg.HubURL(); got != "http://hub.example:9000" {
		t.Fatalf("public url=%q", got)
	}
	cfg.PublicURL = ""
	if got := cfg.HubURL(); got != "" && got != "http://"+DetectLANIP()+":8080" {
		t.Fatalf("derived url=%q", got)
	}

	t.Setenv("HUB_PUBLIC_URL", "http://env.example:8081/")
	loaded, err := Load("")
	if err != nil {
		t.Fatal(err)
	}
	if got := loaded.HubURL(); got != "http://env.example:8081" {
		t.Fatalf("env url=%q", got)
	}
}

func TestDefaults(t *testing.T) {
	cfg, err := Load(filepath.Join(t.TempDir(), "missing.json"))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.HTTPAddr != ":8080" || cfg.DB != "hub.db" || cfg.FirmwareDir != "firmware" {
		t.Fatalf("defaults: %+v", cfg)
	}
}

func TestVoiceSTTSettings(t *testing.T) {
	// No provider: the hub keeps the browser-side recognition.
	cfg := Default()
	if cfg.VoiceSTTConfigured() {
		t.Fatal("STT should be off by default")
	}

	// Groq applies its endpoint and model defaults and requires a key.
	cfg.VoiceSTTProvider = "groq"
	cfg.VoiceSTTKey = "token"
	cfg.VoiceSTTModel = ""
	s := cfg.VoiceSTTSettings()
	if !cfg.VoiceSTTConfigured() {
		t.Fatal("groq with a key should be configured")
	}
	if s.URL != "https://api.groq.com/openai/v1" || s.Model != "whisper-large-v3-turbo" || s.Language != "pt" {
		t.Fatalf("groq settings=%+v", s)
	}

	// A hosted provider without a key is not usable.
	cfg.VoiceSTTKey = ""
	if cfg.VoiceSTTConfigured() {
		t.Fatal("groq without a key should be disabled")
	}

	// OpenRouter has its own defaults.
	cfg.VoiceSTTProvider = "openrouter"
	cfg.VoiceSTTKey = "token"
	s = cfg.VoiceSTTSettings()
	if s.URL != "https://openrouter.ai/api/v1" || s.Model != "openai/whisper-large-v3-turbo" {
		t.Fatalf("openrouter settings=%+v", s)
	}

	// A URL without a provider means a custom OpenAI-compatible endpoint.
	cfg = Default()
	cfg.VoiceSTTURL = "http://127.0.0.1:8080/v1/"
	cfg.VoiceSTTModel = "whisper-local"
	cfg.VoiceSTTKey = ""
	s = cfg.VoiceSTTSettings()
	if s.Provider != "custom" || s.URL != "http://127.0.0.1:8080/v1" || s.Model != "whisper-local" {
		t.Fatalf("custom settings=%+v", s)
	}
	if !cfg.VoiceSTTConfigured() {
		t.Fatal("custom endpoint should be configured")
	}
}

func TestVoiceSTTEnv(t *testing.T) {
	t.Setenv("VOICE_STT_PROVIDER", "groq")
	t.Setenv("VOICE_STT_KEY", "env-token")
	t.Setenv("VOICE_STT_MODEL", "whisper-large-v3")
	t.Setenv("VOICE_STT_LANGUAGE", "es")
	cfg, err := Load("")
	if err != nil {
		t.Fatal(err)
	}
	s := cfg.VoiceSTTSettings()
	if s.Provider != "groq" || s.Key != "env-token" || s.Model != "whisper-large-v3" || s.Language != "es" {
		t.Fatalf("env settings=%+v", s)
	}
}
