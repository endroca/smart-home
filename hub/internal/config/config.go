// Package config loads the hub configuration from a JSON file and environment
// variables. The file holds project-level settings (server, LAN URL and the
// Tuya cloud credentials) and is ignored by git.
//
// Per-device data (LAN tokens, URLs, rooms) and the automation rules live in
// the SQLite database, not here.
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Config is the complete hub configuration.
type Config struct {
	HTTPAddr string `json:"http_addr"`

	// FirmwareDir and FirmwareBaseURL are standardized: they default to a
	// build-relative directory and the hub's own LAN address. They are not
	// read from config.json; the environment overrides remain for advanced
	// deployments.
	FirmwareDir     string `json:"-"`
	FirmwareBaseURL string `json:"-"`

	// PublicURL is the base URL devices should use to reach this hub. Empty
	// means "derive it from the LAN address" (see HubURL). It only needs to be
	// set when the hub is not reachable by its LAN IP, for example behind a
	// proxy or on a different VLAN.
	PublicURL string `json:"public_url,omitempty"`

	// DB is the SQLite database shared by the whole hub (devices, rooms,
	// automations and the Tuya registry).
	DB string `json:"db"`

	// Tuya cloud credentials. They may be set in the config file or, with
	// higher precedence, in the environment. The config file is ignored by
	// git, so it is a safe place for the secrets on a single-operator hub.
	TuyaAccessID     string `json:"tuya_access_id"`
	TuyaAccessSecret string `json:"tuya_access_secret"`
	TuyaRegion       string `json:"tuya_region"`

	// TuyaAutoListen keeps a LAN listener open for every registered Tuya device
	// and refreshes telemetry periodically, so any device with telemetry
	// updates without the operator pressing "Ouvir". TuyaRefreshSeconds is the
	// refresh cadence in seconds. Nil means "default on".
	TuyaAutoListen     *bool `json:"tuya_auto_listen,omitempty"`
	TuyaRefreshSeconds int   `json:"tuya_refresh_seconds,omitempty"`

	// TuyaCloudPollSeconds is the cadence of the cloud poll for devices marked
	// cloud_only (battery devices that sleep, such as locks). The Tuya cloud
	// has request quotas, so this is deliberately slower than the LAN refresh.
	TuyaCloudPollSeconds int `json:"tuya_cloud_poll_seconds,omitempty"`

	// TuyaEventPollSeconds is the cadence of the /report-logs poll that turns a
	// cloud_only device's reports into events for automations. It is faster than
	// the state poll because an unlock should trigger a scene promptly.
	TuyaEventPollSeconds int `json:"tuya_event_poll_seconds,omitempty"`

	// TuyaCloudPush enables the Tuya Message Service (Pulsar) listener that
	// feeds cloud_only devices from the cloud instead of polling. Nil means
	// "default on"; it only connects while at least one cloud_only device
	// exists. TuyaCloudPushTest selects the test channel.
	TuyaCloudPush     *bool `json:"tuya_cloud_push,omitempty"`
	TuyaCloudPushTest bool  `json:"tuya_cloud_push_test,omitempty"`

	// Midea SmartHome account credentials are used only for the one-time
	// bootstrap that retrieves per-device LAN token/key pairs. Normal state
	// reads and commands use the appliance LAN protocol directly.
	MideaUsername string `json:"midea_username,omitempty"`
	MideaPassword string `json:"midea_password,omitempty"`
	MideaCloud    string `json:"midea_cloud,omitempty"`

	// AutomationGraceSeconds is how long after its instant a time-triggered
	// (volatile, one-shot) automation is still allowed to run. A rule whose
	// instant passed by more than this grace is skipped and removed, so a hub
	// that boots hours late does not act on a stale "turn on at 10".
	AutomationGraceSeconds int `json:"automation_grace_seconds,omitempty"`

	// Voice. The local pt-BR rules parser always runs; the LLM fields enable an
	// optional cloud fallback consulted only when the rules cannot resolve a
	// phrase. Without them the hub's voice path is fully local.
	VoiceLLMEnabled bool   `json:"voice_llm_enabled"`
	VoiceLLMURL     string `json:"voice_llm_url"`
	VoiceLLMKey     string `json:"voice_llm_key"`
	VoiceLLMModel   string `json:"voice_llm_model"`

	// VoiceSTT turns recorded audio into text. The client captures the audio
	// (push-to-talk) and posts it to the hub, which forwards it to an
	// OpenAI-compatible /audio/transcriptions endpoint. Provider selects the
	// endpoint and model defaults; URL and Model override them, which also
	// covers a local whisper.cpp server (provider "custom"). Empty provider
	// means the hub keeps the old browser-side recognition.
	VoiceSTTProvider string `json:"voice_stt_provider,omitempty"`
	VoiceSTTURL      string `json:"voice_stt_url,omitempty"`
	VoiceSTTKey      string `json:"voice_stt_key,omitempty"`
	VoiceSTTModel    string `json:"voice_stt_model,omitempty"`
	VoiceSTTLanguage string `json:"voice_stt_language,omitempty"`
}

// STTSettings is the resolved speech-to-text backend. It is what the hub
// actually uses, after applying the provider defaults.
type STTSettings struct {
	Provider string
	URL      string
	Key      string
	Model    string
	Language string
}

// Default returns the defaults used before file/env overrides.
func Default() Config {
	return Config{
		HTTPAddr:    ":8080",
		FirmwareDir: "firmware",
		DB:          "hub.db",
	}
}

// Load reads path when it exists, then applies environment overrides.
func Load(path string) (Config, error) {
	cfg := Default()
	if path != "" {
		data, err := os.ReadFile(path)
		switch {
		case err == nil:
			if err := json.Unmarshal(data, &cfg); err != nil {
				return cfg, fmt.Errorf("config %s: %w", path, err)
			}
		case errors.Is(err, os.ErrNotExist):
			// Running with a partial config is useful for the LAN-only mode.
		default:
			return cfg, fmt.Errorf("config %s: %w", path, err)
		}
	}
	if err := applyEnv(&cfg); err != nil {
		return cfg, err
	}
	return cfg, nil
}

func env(name string, dst *string) {
	if v, ok := os.LookupEnv(name); ok {
		*dst = v
	}
}

func envBool(name string, dst *bool) {
	v, ok := os.LookupEnv(name)
	if !ok {
		return
	}
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "1", "true", "yes", "on":
		*dst = true
	case "0", "false", "no", "off":
		*dst = false
	}
}

func envInt(name string, dst *int) {
	v, ok := os.LookupEnv(name)
	if !ok {
		return
	}
	if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil {
		*dst = n
	}
}

func applyEnv(cfg *Config) error {
	env("HUB_HTTP_ADDR", &cfg.HTTPAddr)
	env("HUB_FIRMWARE_DIR", &cfg.FirmwareDir)
	env("HUB_FIRMWARE_BASE_URL", &cfg.FirmwareBaseURL)
	env("HUB_PUBLIC_URL", &cfg.PublicURL)
	env("HUB_DB", &cfg.DB)
	env("TUYA_ACCESS_ID", &cfg.TuyaAccessID)
	env("TUYA_ACCESS_SECRET", &cfg.TuyaAccessSecret)
	env("TUYA_REGION", &cfg.TuyaRegion)
	env("MIDEA_USERNAME", &cfg.MideaUsername)
	env("MIDEA_PASSWORD", &cfg.MideaPassword)
	env("MIDEA_CLOUD", &cfg.MideaCloud)
	envBool("VOICE_LLM_ENABLED", &cfg.VoiceLLMEnabled)
	env("VOICE_LLM_URL", &cfg.VoiceLLMURL)
	env("VOICE_LLM_KEY", &cfg.VoiceLLMKey)
	env("VOICE_LLM_MODEL", &cfg.VoiceLLMModel)
	env("VOICE_STT_PROVIDER", &cfg.VoiceSTTProvider)
	env("VOICE_STT_URL", &cfg.VoiceSTTURL)
	env("VOICE_STT_KEY", &cfg.VoiceSTTKey)
	env("VOICE_STT_MODEL", &cfg.VoiceSTTModel)
	env("VOICE_STT_LANGUAGE", &cfg.VoiceSTTLanguage)

	if cfg.HTTPAddr == "" {
		cfg.HTTPAddr = ":8080"
	}
	if cfg.FirmwareDir == "" {
		cfg.FirmwareDir = "firmware"
	}
	if cfg.DB == "" {
		cfg.DB = "hub.db"
	}
	if cfg.MideaCloud == "" {
		cfg.MideaCloud = "SmartHome"
	}
	if cfg.TuyaAutoListen == nil {
		enabled := true
		cfg.TuyaAutoListen = &enabled
	}
	envBool("TUYA_AUTO_LISTEN", cfg.TuyaAutoListen)
	if cfg.TuyaRefreshSeconds <= 0 {
		cfg.TuyaRefreshSeconds = 5
	}
	envInt("TUYA_REFRESH_SECONDS", &cfg.TuyaRefreshSeconds)
	if cfg.TuyaCloudPollSeconds <= 0 {
		cfg.TuyaCloudPollSeconds = 300
	}
	envInt("TUYA_CLOUD_POLL_SECONDS", &cfg.TuyaCloudPollSeconds)
	if cfg.TuyaEventPollSeconds <= 0 {
		cfg.TuyaEventPollSeconds = 60
	}
	envInt("TUYA_EVENT_POLL_SECONDS", &cfg.TuyaEventPollSeconds)
	if cfg.TuyaCloudPush == nil {
		enabled := true
		cfg.TuyaCloudPush = &enabled
	}
	envBool("TUYA_CLOUD_PUSH", cfg.TuyaCloudPush)
	envBool("TUYA_CLOUD_PUSH_TEST", &cfg.TuyaCloudPushTest)
	if cfg.AutomationGraceSeconds <= 0 {
		cfg.AutomationGraceSeconds = 120
	}
	envInt("HUB_AUTOMATION_GRACE_SECONDS", &cfg.AutomationGraceSeconds)
	return nil
}

// TuyaAutoListenEnabled reports whether the hub should auto-listen to Tuya
// devices and refresh their telemetry (default on).
func (c Config) TuyaAutoListenEnabled() bool {
	return c.TuyaAutoListen == nil || *c.TuyaAutoListen
}

// TuyaRefresh returns the telemetry refresh interval (default 5s).
func (c Config) TuyaRefresh() time.Duration {
	secs := c.TuyaRefreshSeconds
	if secs <= 0 {
		secs = 5
	}
	return time.Duration(secs) * time.Second
}

// TuyaCloudPoll returns the cloud poll interval for cloud_only devices
// (default 300s). It is the window that keeps such a device shown as online
// between polls.
func (c Config) TuyaCloudPoll() time.Duration {
	secs := c.TuyaCloudPollSeconds
	if secs <= 0 {
		secs = 300
	}
	return time.Duration(secs) * time.Second
}

// TuyaEventPoll returns the /report-logs poll interval for cloud_only devices
// (default 60s).
func (c Config) TuyaEventPoll() time.Duration {
	secs := c.TuyaEventPollSeconds
	if secs <= 0 {
		secs = 60
	}
	return time.Duration(secs) * time.Second
}

// TuyaCloudPushEnabled reports whether the Message Service push listener is on
// (default on).
func (c Config) TuyaCloudPushEnabled() bool {
	return c.TuyaCloudPush == nil || *c.TuyaCloudPush
}

// AutomationGrace returns the grace window after a scheduled automation's
// instant (default 120s).
func (c Config) AutomationGrace() time.Duration {
	secs := c.AutomationGraceSeconds
	if secs <= 0 {
		secs = 120
	}
	return time.Duration(secs) * time.Second
}

// VoiceLLMConfigured reports whether the optional cloud intent fallback is
// enabled and pointed at an endpoint.
func (c Config) VoiceLLMConfigured() bool {
	return c.VoiceLLMEnabled && c.VoiceLLMURL != ""
}

// VoiceSTTSettings resolves the speech-to-text provider defaults. It never
// mutates the loaded config. A URL/key without a provider is taken as a
// custom OpenAI-compatible endpoint (the shape a local whisper.cpp server
// serves). The result has an empty Provider when the backend is unusable,
// which VoiceSTTConfigured uses to keep the old client-side path.
func (c Config) VoiceSTTSettings() STTSettings {
	provider := strings.ToLower(strings.TrimSpace(c.VoiceSTTProvider))
	if provider == "" && (strings.TrimSpace(c.VoiceSTTURL) != "" || strings.TrimSpace(c.VoiceSTTKey) != "") {
		provider = "custom"
	}
	s := STTSettings{
		Provider: provider,
		URL:      strings.TrimRight(strings.TrimSpace(c.VoiceSTTURL), "/"),
		Key:      strings.TrimSpace(c.VoiceSTTKey),
		Model:    strings.TrimSpace(c.VoiceSTTModel),
		Language: strings.TrimSpace(c.VoiceSTTLanguage),
	}
	switch provider {
	case "groq":
		if s.URL == "" {
			s.URL = "https://api.groq.com/openai/v1"
		}
		if s.Model == "" {
			s.Model = "whisper-large-v3-turbo"
		}
	case "openai":
		if s.URL == "" {
			s.URL = "https://api.openai.com/v1"
		}
		if s.Model == "" {
			s.Model = "gpt-4o-mini-transcribe"
		}
	case "openrouter":
		if s.URL == "" {
			s.URL = "https://openrouter.ai/api/v1"
		}
		if s.Model == "" {
			s.Model = "openai/whisper-large-v3-turbo"
		}
	}
	if s.Language == "" {
		s.Language = "pt"
	}
	// The hosted providers need a key; a custom endpoint may be local and
	// unauthenticated, so only URL and model are mandatory there.
	switch s.Provider {
	case "groq", "openai", "openrouter":
		if s.Key == "" {
			s.Provider = ""
		}
	case "custom":
		// Custom endpoints are valid with the URL/model supplied below.
	default:
		s.Provider = ""
	}
	if s.URL == "" || s.Model == "" {
		s.Provider = ""
	}
	return s
}

// VoiceSTTConfigured reports whether the hub should transcribe audio itself.
func (c Config) VoiceSTTConfigured() bool {
	return c.VoiceSTTSettings().Provider != ""
}

// Port returns the TCP port from HTTPAddr.
func (c Config) Port() int {
	_, port, err := net.SplitHostPort(c.HTTPAddr)
	if err != nil {
		// Accept ":8080" and "0.0.0.0:8080"; fall back to 8080.
		if strings.HasPrefix(c.HTTPAddr, ":") {
			if p, e := strconv.Atoi(strings.TrimPrefix(c.HTTPAddr, ":")); e == nil {
				return p
			}
		}
		return 8080
	}
	p, err := strconv.Atoi(port)
	if err != nil || p <= 0 {
		return 8080
	}
	return p
}

// FirmwareURL returns the public base URL for served firmware files.
func (c Config) FirmwareURL() string {
	base := strings.TrimRight(c.FirmwareBaseURL, "/")
	if base != "" {
		return base
	}
	return c.HubURL()
}

// HubURL returns the base URL a device should use to reach this hub. It is an
// IP address, not a *.local name: the ESP32 HTTP client cannot resolve mDNS.
// PublicURL overrides it for deployments where the LAN address is not the
// address the devices should use.
func (c Config) HubURL() string {
	base := strings.TrimRight(c.PublicURL, "/")
	if base != "" {
		return base
	}
	ip := DetectLANIP()
	if ip == "" {
		return ""
	}
	return fmt.Sprintf("http://%s:%d", ip, c.Port())
}

// DetectLANIP returns the local address used to reach the internet, which is
// normally the Pi's LAN address. It does not send any packet.
func DetectLANIP() string {
	conn, err := net.Dial("udp", "8.8.8.8:80")
	if err != nil {
		return ""
	}
	defer conn.Close()
	addr, ok := conn.LocalAddr().(*net.UDPAddr)
	if !ok {
		return ""
	}
	return addr.IP.String()
}

// ResolvePath resolves a path relative to the config file directory.
func ResolvePath(configPath, value string) string {
	if value == "" || filepath.IsAbs(value) {
		return value
	}
	if configPath == "" {
		return value
	}
	return filepath.Join(filepath.Dir(configPath), value)
}
