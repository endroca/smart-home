package httpapi

import (
	"context"
	"errors"
	"net/http"
	"time"

	"smart_home/hub/internal/db"
	mideasvc "smart_home/hub/internal/midea"
)

func (s *Server) SetMidea(svc *mideasvc.Service) { s.midea = svc }

func (s *Server) mideaRoutes(mux *http.ServeMux) {
	if s.midea == nil {
		return
	}
	mux.HandleFunc("/api/midea/devices", s.mideaDevices)
	mux.HandleFunc("/api/midea/scan", s.mideaScan)
	mux.HandleFunc("/api/midea/register", s.mideaRegister)
	mux.HandleFunc("/api/midea/forget", s.mideaForget)
	mux.HandleFunc("/api/midea/state", s.mideaState)
	mux.HandleFunc("/api/midea/control", s.mideaControl)
	mux.HandleFunc("/api/midea/specs", s.mideaSpecs)
	mux.HandleFunc("/api/midea/energy", s.mideaEnergy)
}

// mideaDeviceView is the API representation of a registered appliance. It never
// exposes the LAN token or key, only whether one is stored.
type mideaDeviceView struct {
	ID         string         `json:"id"`
	Name       string         `json:"name"`
	IP         string         `json:"ip"`
	Port       int            `json:"port"`
	Model      string         `json:"model,omitempty"`
	HasKey     bool           `json:"has_key"`
	RoomID     string         `json:"room_id,omitempty"`
	Template   string         `json:"template,omitempty"`
	Online     bool           `json:"online"`
	State      map[string]any `json:"state,omitempty"`
	Actions    []db.MideaSpec `json:"actions,omitempty"`
	Registered bool           `json:"registered"`
}

func (s *Server) mideaDevices(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	devices, err := s.midea.Devices(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "midea_db", err.Error())
		return
	}
	out := make([]mideaDeviceView, 0, len(devices))
	for _, d := range devices {
		state, _ := s.midea.CachedState(r.Context(), d.ID)
		specs, _ := s.midea.Specs(r.Context(), d.ID)
		out = append(out, mideaDeviceView{
			ID:         d.ID,
			Name:       d.Name,
			IP:         d.IP,
			Port:       d.Port,
			Model:      d.ProductKey,
			HasKey:     d.LocalKey != "",
			RoomID:     d.RoomID,
			Template:   d.Template,
			Online:     s.midea.Online(d.ID),
			State:      state,
			Actions:    specs,
			Registered: d.LocalKey != "",
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":      true,
		"devices": out,
		"cloud":   s.midea.CloudConfigured(),
	})
}

func (s *Server) mideaScan(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	items, err := s.midea.Scan(r.Context(), r.URL.Query().Get("ip"))
	if err != nil {
		writeError(w, http.StatusBadGateway, "midea_scan", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "devices": items})
}

func (s *Server) mideaRegister(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	var req struct {
		ID   string `json:"id"`
		Name string `json:"name"`
		IP   string `json:"ip"`
	}
	if err := decode(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_payload", err.Error())
		return
	}
	if req.ID == "" {
		writeError(w, http.StatusBadRequest, "invalid_payload", "device id is required")
		return
	}
	d, err := s.midea.Register(r.Context(), req.ID, req.Name, req.IP)
	if err != nil {
		status := http.StatusBadGateway
		if errors.Is(err, mideasvc.ErrCloudNotConfigured) {
			status = http.StatusServiceUnavailable
		}
		writeError(w, status, "midea_register", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "device": map[string]any{
		"id": d.ID, "name": d.Name, "ip": d.IP, "port": d.Port, "model": d.ProductKey, "has_key": d.LocalKey != "",
	}})
}

// mideaForget removes an appliance from the registry and drops its cached
// capabilities and state.
func (s *Server) mideaForget(w http.ResponseWriter, r *http.Request) {
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
	if err := s.midea.Forget(r.Context(), req.ID); err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, db.ErrNotFound) {
			status = http.StatusNotFound
		}
		writeError(w, status, "midea_forget", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// mideaState reads (POST) the appliance over the LAN. GET returns the last
// persisted state without touching the network.
func (s *Server) mideaState(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("id")
	switch r.Method {
	case http.MethodGet:
		if id == "" {
			writeError(w, http.StatusBadRequest, "invalid_payload", "id is required")
			return
		}
		state, err := s.midea.CachedState(r.Context(), id)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "midea_state", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "state": state})
	case http.MethodPost:
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
		ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
		defer cancel()
		state, err := s.midea.State(ctx, id)
		if err != nil {
			writeError(w, http.StatusBadGateway, "midea_state", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "state": mideasvc.StateMap(state)})
	default:
		methodNotAllowed(w)
	}
}

// mideaControl applies a partial logical state over the LAN.
func (s *Server) mideaControl(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	var req struct {
		ID    string         `json:"id"`
		State map[string]any `json:"state"`
	}
	if err := decode(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_payload", err.Error())
		return
	}
	if req.ID == "" {
		writeError(w, http.StatusBadRequest, "invalid_payload", "device id is required")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	state, err := s.midea.Apply(ctx, req.ID, req.State)
	if err != nil {
		writeError(w, http.StatusBadGateway, "midea_control", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "state": state})
}

// mideaSpecs lists the cached capability list (GET) or refreshes it over the
// LAN (POST).
func (s *Server) mideaSpecs(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("id")
	switch r.Method {
	case http.MethodGet:
		if id == "" {
			writeError(w, http.StatusBadRequest, "invalid_payload", "id is required")
			return
		}
		specs, err := s.midea.Specs(r.Context(), id)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "midea_specs", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "actions": specs})
	case http.MethodPost:
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
		specs, err := s.midea.RefreshSpecs(ctx, id)
		if err != nil {
			writeError(w, http.StatusBadGateway, "midea_specs", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "actions": specs})
	default:
		methodNotAllowed(w)
	}
}
