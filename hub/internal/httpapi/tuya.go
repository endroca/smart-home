package httpapi

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"time"

	"smart_home/hub/internal/db"
	"smart_home/hub/internal/tuya/service"
)

// tuyaDeviceView is the API representation of a registered device. It never
// exposes the local key, only whether one is stored.
type tuyaDeviceView struct {
	ID           string         `json:"id"`
	Name         string         `json:"name"`
	IP           string         `json:"ip"`
	Port         int            `json:"port"`
	Version      string         `json:"version"`
	ProductKey   string         `json:"product_key,omitempty"`
	Device22     bool           `json:"device22"`
	HasKey       bool           `json:"has_key"`
	KeySource    string         `json:"key_source,omitempty"`
	KeyUpdatedAt time.Time      `json:"key_updated_at,omitempty"`
	RoomID       string         `json:"room_id,omitempty"`
	Template     string         `json:"template,omitempty"`
	Category     string         `json:"category,omitempty"`
	CloudOnly    bool           `json:"cloud_only,omitempty"`
	Online       bool           `json:"online"`
	State        map[string]any `json:"state,omitempty"`
}

func tuyaDeviceViewOf(d db.Device, state map[string]any, online bool) tuyaDeviceView {
	return tuyaDeviceView{
		ID:           d.ID,
		Name:         d.Name,
		IP:           d.IP,
		Port:         d.Port,
		Version:      d.Version,
		ProductKey:   d.ProductKey,
		Device22:     d.Device22,
		HasKey:       d.LocalKey != "",
		KeySource:    d.KeySource,
		KeyUpdatedAt: d.KeyUpdatedAt,
		RoomID:       d.RoomID,
		Template:     d.Template,
		Category:     d.Category,
		CloudOnly:    d.CloudOnly,
		Online:       online,
		State:        state,
	}
}

func tuyaDeviceViews(devices []db.Device, states map[string]map[string]any, online func(string) bool) []tuyaDeviceView {
	out := make([]tuyaDeviceView, 0, len(devices))
	for _, d := range devices {
		var on bool
		if online != nil {
			on = online(d.ID)
		}
		out = append(out, tuyaDeviceViewOf(d, states[d.ID], on))
	}
	return out
}

// SetTuya attaches the Tuya service. When it is nil the Tuya routes are not
// registered and the UI hides the area.
func (s *Server) SetTuya(svc *service.Service) { s.tuya = svc }

func (s *Server) tuyaRoutes(mux *http.ServeMux) {
	if s.tuya == nil {
		return
	}
	mux.HandleFunc("/api/tuya/devices", s.tuyaDevices)
	mux.HandleFunc("/api/tuya/cloud", s.tuyaCloud)
	mux.HandleFunc("/api/tuya/scan", s.tuyaScan)
	mux.HandleFunc("/api/tuya/register", s.tuyaRegister)
	mux.HandleFunc("/api/tuya/forget", s.tuyaForget)
	mux.HandleFunc("/api/tuya/listen", s.tuyaListen)
	mux.HandleFunc("/api/tuya/events", s.tuyaEvents)
	mux.HandleFunc("/api/tuya/control", s.tuyaControl)
	mux.HandleFunc("/api/tuya/specs", s.tuyaSpecs)
	mux.HandleFunc("/api/tuya/state", s.tuyaState)
	mux.HandleFunc("/api/tuya/energy", s.tuyaEnergy)
	mux.HandleFunc("/api/tuya/records", s.tuyaRecords)
}

func (s *Server) tuyaDevices(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	devices, err := s.tuya.Devices(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db", err.Error())
		return
	}
	states := s.allStates(r)
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":        true,
		"devices":   tuyaDeviceViews(devices, states, s.tuya.Online),
		"cloud":     s.tuya.CloudConfigured(),
		"listening": s.tuya.Listening(),
	})
}

func (s *Server) allStates(r *http.Request) map[string]map[string]any {
	if s.store == nil {
		return nil
	}
	states, err := s.store.AllStates(r.Context())
	if err != nil {
		return nil
	}
	return states
}

// tuyaCloud lists the devices on the Tuya account, so a device that never
// appears in the LAN scan (a sleeping battery lock) can be registered.
func (s *Server) tuyaCloud(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	devices, err := s.tuya.CloudDevices(ctx)
	if err != nil {
		status := http.StatusBadGateway
		if err == service.ErrCloudNotConfigured {
			status = http.StatusServiceUnavailable
		}
		writeError(w, status, "tuya_cloud", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "devices": devices})
}

func (s *Server) tuyaScan(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost && r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	devices, err := s.tuya.Scan(r.Context())
	if err != nil {
		writeError(w, http.StatusBadGateway, "tuya_scan", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "devices": devices})
}

func (s *Server) tuyaRegister(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	var req struct {
		ID  string   `json:"id"`
		IDs []string `json:"ids"`
	}
	if err := decode(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_payload", err.Error())
		return
	}
	ids := req.IDs
	if req.ID != "" {
		ids = append(ids, req.ID)
	}
	registered, err := s.tuya.Register(r.Context(), ids)
	if err != nil {
		status := http.StatusBadGateway
		switch err {
		case service.ErrCloudNotConfigured:
			status = http.StatusServiceUnavailable
		case service.ErrNoMatch:
			status = http.StatusNotFound
		}
		writeError(w, status, "tuya_register", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "registered": tuyaDeviceViews(registered, nil, nil)})
}

func (s *Server) tuyaForget(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	var req struct {
		ID string `json:"id"`
	}
	if err := decode(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_payload", err.Error())
		return
	}
	if req.ID == "" {
		writeError(w, http.StatusBadRequest, "invalid_payload", "device id is required")
		return
	}
	if err := s.tuya.Forget(r.Context(), req.ID); err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, db.ErrNotFound) {
			status = http.StatusNotFound
		}
		writeError(w, status, "tuya_forget", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) tuyaListen(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	var req struct {
		ID     string `json:"id"`
		Action string `json:"action"`
	}
	if err := decode(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_payload", err.Error())
		return
	}
	if req.ID == "" {
		writeError(w, http.StatusBadRequest, "invalid_payload", "device id is required")
		return
	}
	switch req.Action {
	case "stop":
		s.tuya.StopListen(req.ID)
	case "start", "":
		if err := s.tuya.StartListen(r.Context(), req.ID); err != nil {
			status := http.StatusBadGateway
			switch err {
			case service.ErrNoKey, service.ErrNoIP:
				status = http.StatusBadRequest
			}
			writeError(w, status, "tuya_listen", err.Error())
			return
		}
	default:
		writeError(w, http.StatusBadRequest, "invalid_payload", "action must be start or stop")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "listening": s.tuya.Listening()})
}

func (s *Server) tuyaEvents(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	id := r.URL.Query().Get("id")
	state, _ := s.tuya.State(r.Context(), id)
	last, hasLast := s.tuya.LastEvent(id)
	var lastView any
	if hasLast {
		lastView = last
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":        true,
		"last":      lastView,
		"recent":    s.tuya.RecentEvents(id, 20),
		"listening": s.tuya.Listening(),
		"state":     state,
	})
}

// tuyaControl writes one DP on a Tuya device (LAN first, cloud fallback).
func (s *Server) tuyaControl(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	var req struct {
		ID    string `json:"id"`
		DP    string `json:"dp"`
		Value any    `json:"value"`
	}
	if err := decode(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_payload", err.Error())
		return
	}
	if req.ID == "" || req.DP == "" {
		writeError(w, http.StatusBadRequest, "invalid_payload", "id and dp are required")
		return
	}
	// Bound the control so a stale LAN address does not hang the request.
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	res, err := s.tuya.Control(ctx, req.ID, req.DP, req.Value)
	if err != nil {
		writeError(w, http.StatusBadGateway, "tuya_control", err.Error())
		return
	}
	// Refresh the resulting state in the background; the SSE stream pushes it
	// so the request can answer as soon as the device acknowledged.
	go func(id string) {
		rctx, rcancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer rcancel()
		_, _ = s.tuya.Refresh(rctx, id)
	}(req.ID)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "result": res})
}

// tuyaState reads (GET) or refreshes from the device (POST) the last-known data
// points of a device.
func (s *Server) tuyaState(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("id")
	if r.Method == http.MethodPost {
		var req struct {
			ID string `json:"id"`
		}
		if err := decode(r, &req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid_payload", err.Error())
			return
		}
		if req.ID != "" {
			id = req.ID
		}
		if id == "" {
			writeError(w, http.StatusBadRequest, "invalid_payload", "id is required")
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
		defer cancel()
		state, err := s.tuya.Refresh(ctx, id)
		if err != nil {
			writeError(w, http.StatusBadGateway, "tuya_state", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "state": state})
		return
	}
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	if id == "" {
		writeError(w, http.StatusBadRequest, "invalid_payload", "id is required")
		return
	}
	state, err := s.tuya.State(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "tuya_state", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "state": state})
}

// tuyaEnergy serves the cloud energy-consumption series of a device. It always
// answers 200 for cloud failures: the body carries Available=false plus a
// machine-readable reason, so the UI can explain the state without treating it
// as a request error.
func (s *Server) tuyaEnergy(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	id := r.URL.Query().Get("id")
	if id == "" {
		writeError(w, http.StatusBadRequest, "invalid_payload", "id is required")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 25*time.Second)
	defer cancel()
	series, err := s.tuya.EnergyTrend(ctx, id, r.URL.Query().Get("range"))
	if err != nil {
		status := http.StatusBadGateway
		if errors.Is(err, db.ErrNotFound) {
			status = http.StatusNotFound
		}
		writeError(w, status, "tuya_energy", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":         true,
		"available":  series.Available,
		"reason":     series.Reason,
		"message":    series.Message,
		"range":      series.Range,
		"unit":       series.Unit,
		"total":      series.Total,
		"points":     series.Points,
		"updated_at": series.UpdatedAt,
	})
}

// tuyaRecords serves the unlocking history of a lock (cloud Smart Lock API). It
// always answers 200 for cloud failures: the body carries Available=false plus a
// machine-readable reason, mirroring the energy endpoint.
func (s *Server) tuyaRecords(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	id := r.URL.Query().Get("id")
	if id == "" {
		writeError(w, http.StatusBadRequest, "invalid_payload", "id is required")
		return
	}
	limit := 0
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			limit = n
		}
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	res, err := s.tuya.OpenRecords(ctx, id, limit)
	if err != nil {
		status := http.StatusBadGateway
		if errors.Is(err, db.ErrNotFound) {
			status = http.StatusNotFound
		}
		writeError(w, status, "tuya_records", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":         true,
		"available":  res.Available,
		"reason":     res.Reason,
		"message":    res.Message,
		"total":      res.Total,
		"records":    res.Records,
		"updated_at": res.UpdatedAt,
	})
}

// tuyaSpecs lists the cached DP specifications (GET) or refreshes them from the
// cloud (POST).
func (s *Server) tuyaSpecs(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("id")
	if r.Method == http.MethodPost {
		var req struct {
			ID string `json:"id"`
		}
		if err := decode(r, &req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid_payload", err.Error())
			return
		}
		if req.ID != "" {
			id = req.ID
		}
		if id == "" {
			writeError(w, http.StatusBadRequest, "invalid_payload", "id is required")
			return
		}
		specs, err := s.tuya.RefreshSpecs(r.Context(), id)
		if err != nil {
			status := http.StatusBadGateway
			if err == service.ErrCloudNotConfigured {
				status = http.StatusServiceUnavailable
			}
			writeError(w, status, "tuya_specs", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "specs": specs})
		return
	}
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	if id == "" {
		writeError(w, http.StatusBadRequest, "invalid_payload", "id is required")
		return
	}
	specs, err := s.tuya.Specs(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "tuya_specs", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "specs": specs})
}
