// Command smart-home-hub runs the smart_home Raspberry Pi hub.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"smart_home/hub/internal/config"
	"smart_home/hub/internal/db"
	"smart_home/hub/internal/dispatch"
	"smart_home/hub/internal/eventbus"
	"smart_home/hub/internal/httpapi"
	"smart_home/hub/internal/hub"
	mideasvc "smart_home/hub/internal/midea"
	"smart_home/hub/internal/tuya"
	"smart_home/hub/internal/tuya/service"
	"smart_home/hub/internal/voice"
)

func main() {
	log.SetFlags(log.Ldate | log.Ltime | log.LUTC)

	configPath := flag.String("config", envOr("HUB_CONFIG", "config.json"), "path to hub config JSON")
	showVersion := flag.Bool("version", false, "print version and exit")
	flag.Parse()

	if *showVersion {
		fmt.Println(hub.Version)
		return
	}

	cfg, err := config.Load(*configPath)
	if err != nil {
		log.Fatalf("load config: %v", err)
	}
	// Paths in the JSON file are relative to the file itself; environment
	// overrides are resolved relative to the process working directory.
	if *configPath != "" {
		cfg.FirmwareDir = config.ResolvePath(*configPath, cfg.FirmwareDir)
		cfg.DB = config.ResolvePath(*configPath, cfg.DB)
	}

	// The database is the source of truth for devices, rooms, automations and
	// the Tuya registry; it is opened once and shared by the whole hub.
	store, err := db.Open(cfg.DB)
	if err != nil {
		log.Fatalf("open database: %v", err)
	}
	defer store.Close()

	// bus pushes hub events to the UI over SSE (no polling).
	bus := eventbus.New()

	log.Printf("smart-home-hub %s", hub.Version)
	log.Printf("http=%s db=%s firmware=%s", cfg.HTTPAddr, cfg.DB, cfg.FirmwareDir)

	h, err := hub.New(cfg, store)
	if err != nil {
		log.Fatalf("create hub: %v", err)
	}
	h.SetEventBus(bus)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// Seed the hot in-memory inventory from the durable replica so the UI can
	// render the last-known state immediately, before the LAN probe answers.
	if err := h.LoadState(ctx); err != nil {
		log.Printf("load cached state: %v", err)
	}
	h.Start(ctx)
	h.StartDiscovery(ctx)

	api := httpapi.New(h, cfg, store)
	api.SetEventBus(bus)

	tuyaSvc, err := service.Open(service.Config{
		Store: store,
		Cloud: tuya.CloudConfig{
			AccessID:     cfg.TuyaAccessID,
			AccessSecret: cfg.TuyaAccessSecret,
			Region:       cfg.TuyaRegion,
		},
		CloudPush:     cfg.TuyaCloudPushEnabled(),
		CloudPushTest: cfg.TuyaCloudPushTest,
	})
	if err != nil {
		log.Fatalf("tuya: %v", err)
	}
	defer tuyaSvc.Close()
	api.SetTuya(tuyaSvc)
	tuyaSvc.SetEventBus(bus)
	log.Printf("tuya: cloud=%v", tuyaSvc.CloudConfigured())

	mideaService, err := mideasvc.Open(mideasvc.Config{Store: store, Username: cfg.MideaUsername, Password: cfg.MideaPassword, Cloud: cfg.MideaCloud, StatePoll: 10 * time.Second})
	if err != nil {
		log.Fatalf("midea: %v", err)
	}
	defer mideaService.Close()
	api.SetMidea(mideaService)
	mideaService.SetEventBus(bus)
	go mideaService.RefreshIPs(ctx, time.Minute)
	go mideaService.PollStates(ctx, time.Minute)
	log.Printf("midea: SmartHome bootstrap configured=%v; appliance control is LAN-local", mideaService.CloudConfigured())

	// Auto-listen keeps a LAN connection open for every registered Tuya device
	// and refreshes telemetry periodically, so devices with telemetry update
	// without the operator pressing "Ouvir".
	if cfg.TuyaAutoListenEnabled() {
		tuyaSvc.StartAutoListen(ctx, cfg.TuyaRefresh(), cfg.TuyaCloudPoll(), cfg.TuyaEventPoll())
		log.Printf("tuya: auto-listen on, refresh=%s cloud-poll=%s event-poll=%s", cfg.TuyaRefresh(), cfg.TuyaCloudPoll(), cfg.TuyaEventPoll())
	}
	if cfg.TuyaCloudPushEnabled() {
		log.Printf("tuya: cloud push on (Message Service/Pulsar; só conecta se houver device cloud_only)")
	}

	// Automations: Tuya events trigger logical actions on devices, with the
	// hub deciding the transport.
	dispatcher := dispatch.New(store, tuyaSvc, h, tuyaSvc)
	dispatcher.SetMidea(mideaService)
	dispatcher.SetEventBus(bus)
	dispatcher.SetThingStates(h)
	dispatcher.SetGrace(cfg.AutomationGrace())
	dispatcher.Start(ctx)
	// The clock runs the volatile, time-triggered automations ("desliga às
	// 14", "desliga daqui a 10 minutos").
	dispatcher.StartClock(ctx)
	defer dispatcher.Close()
	api.SetDispatcher(dispatcher)

	// Voice: the hub resolves a transcript into a logical command and runs it
	// through the same paths (SetState, Tuya control, dispatcher). The mic stays
	// in the client; when STT is configured the hub also transcribes the
	// recorded audio (Whisper-compatible endpoint), otherwise the browser does
	// the recognition. The optional LLM is a fallback for free-form phrases.
	voiceSvc := voice.New(store, sceneSource{store: store}, h, tuyaSvc, dispatcher)
	voiceSvc.SetMidea(mideaService)
	if cfg.VoiceLLMConfigured() {
		voiceSvc.SetInterpreter(voice.NewLLM(cfg.VoiceLLMURL, cfg.VoiceLLMKey, cfg.VoiceLLMModel))
		log.Printf("voice: LLM fallback enabled (%s)", cfg.VoiceLLMModel)
	}
	if stt := cfg.VoiceSTTSettings(); cfg.VoiceSTTConfigured() {
		voiceSvc.SetTranscriber(
			voice.NewSTT(stt.URL, stt.Key, stt.Model, stt.Language),
			voice.STTInfo{Provider: stt.Provider, Model: stt.Model, Language: stt.Language, URL: stt.URL},
		)
		log.Printf("voice: STT enabled (%s / %s, lang=%s)", stt.Provider, stt.Model, stt.Language)
	}
	api.SetVoice(voiceSvc)

	srv, err := api.Serve(cfg.HTTPAddr)
	if err != nil {
		log.Fatalf("create http server: %v", err)
	}

	errCh := make(chan error, 1)
	go func() {
		log.Printf("listening on http://%s", cfg.HTTPAddr)
		errCh <- srv.ListenAndServe()
	}()

	select {
	case <-ctx.Done():
		log.Printf("shutting down")
	case err := <-errCh:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("http server: %v", err)
		}
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Printf("http shutdown: %v", err)
	}
}

func envOr(name, fallback string) string {
	if v := os.Getenv(name); v != "" {
		return v
	}
	return fallback
}

// sceneSource exposes the enabled manual automations ("scenes") to the voice
// service, keeping the voice package free of a dispatch import.
type sceneSource struct{ store *db.Store }

func (s sceneSource) Scenes(ctx context.Context) ([]voice.Scene, error) {
	list, err := dispatch.Load(ctx, s.store)
	if err != nil {
		return nil, err
	}
	out := []voice.Scene{}
	for _, a := range list {
		if a.Source.Type == dispatch.SourceManual && a.Enabled {
			out = append(out, voice.Scene{ID: a.ID, Name: a.Name})
		}
	}
	return out, nil
}
