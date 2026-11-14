package httpapi

import (
	"context"
	"errors"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"

	"smart_home/hub/internal/db"
)

// probeAsync kicks a LAN discovery in the background so a newly registered or
// updated gateway shows its devices right away, instead of waiting for the
// periodic probe.
func (s *Server) probeAsync(thing string) {
	if s.hub == nil || strings.TrimSpace(thing) == "" {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if err := s.hub.ProbeThing(ctx, thing); err != nil {
			log.Printf("[hub] probe on register %s: %v", thing, err)
		}
	}()
}

// deviceView is the API representation of a stored device. It never exposes the
// LAN token, only whether one is set.
type deviceView struct {
	ID           string    `json:"id"`
	Kind         string    `json:"kind"`
	Name         string    `json:"name"`
	FirmwareName string    `json:"firmware_name,omitempty"`
	IP           string    `json:"ip,omitempty"`
	Port         int       `json:"port,omitempty"`
	Version      string    `json:"version,omitempty"`
	ProductKey   string    `json:"product_key,omitempty"`
	Device22     bool      `json:"device22,omitempty"`
	HasKey       bool      `json:"has_key"`
	KeySource    string    `json:"key_source,omitempty"`
	HasToken     bool      `json:"has_token"`
	URL          string    `json:"url,omitempty"`
	RoomID       string    `json:"room_id,omitempty"`
	Owner        string    `json:"owner,omitempty"`
	DeviceID     string    `json:"device_id,omitempty"`
	Template     string    `json:"template,omitempty"`
	Category     string    `json:"category,omitempty"`
	CloudOnly    bool      `json:"cloud_only,omitempty"`
	UpdatedAt    time.Time `json:"updated_at,omitempty"`
}

func deviceViewOf(d db.Device) deviceView {
	return deviceView{
		ID:           d.ID,
		Kind:         d.Kind,
		Name:         d.Name,
		FirmwareName: d.FirmwareName,
		IP:           d.IP,
		Port:         d.Port,
		Version:      d.Version,
		ProductKey:   d.ProductKey,
		Device22:     d.Device22,
		HasKey:       d.LocalKey != "",
		KeySource:    d.KeySource,
		HasToken:     d.Token != "",
		URL:          d.URL,
		RoomID:       d.RoomID,
		Owner:        d.Owner,
		DeviceID:     d.DeviceID,
		Template:     d.Template,
		Category:     d.Category,
		CloudOnly:    d.CloudOnly,
		UpdatedAt:    d.UpdatedAt,
	}
}

func deviceViews(devices []db.Device) []deviceView {
	out := make([]deviceView, 0, len(devices))
	for _, d := range devices {
		out = append(out, deviceViewOf(d))
	}
	return out
}

func (s *Server) deviceRoutes(mux *http.ServeMux) {
	if s.store == nil {
		return
	}
	mux.HandleFunc("/api/devices", s.devices)
	mux.HandleFunc("/api/devices/", s.deviceRoute)
	mux.HandleFunc("/api/rooms", s.rooms)
	mux.HandleFunc("/api/rooms/", s.roomRoute)
	mux.HandleFunc("/api/placements", s.placements)
}

func (s *Server) devices(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		kind := strings.TrimSpace(r.URL.Query().Get("kind"))
		list, err := s.store.ListDevices(r.Context(), kind)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "db", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "devices": deviceViews(list)})
	case http.MethodPost:
		var req struct {
			ID       string `json:"id"`
			Kind     string `json:"kind"`
			Name     string `json:"name"`
			Token    string `json:"token"`
			URL      string `json:"url"`
			RoomID   string `json:"room_id"`
			Template string `json:"template"`
		}
		if err := decode(r, &req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid_payload", err.Error())
			return
		}
		if strings.TrimSpace(req.ID) == "" {
			writeError(w, http.StatusBadRequest, "invalid_payload", "id is required")
			return
		}
		if req.Kind == "" {
			req.Kind = db.KindThing
		}
		dev := db.Device{ID: req.ID, Kind: req.Kind, Name: req.Name, Token: req.Token, URL: req.URL, RoomID: req.RoomID, Template: req.Template}
		if err := s.store.UpsertDevice(r.Context(), dev); err != nil {
			writeError(w, http.StatusInternalServerError, "db", err.Error())
			return
		}
		if dev.Kind == db.KindThing {
			s.probeAsync(dev.ID)
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	default:
		methodNotAllowed(w)
	}
}

func (s *Server) deviceRoute(w http.ResponseWriter, r *http.Request) {
	id, err := url.PathUnescape(strings.TrimPrefix(r.URL.Path, "/api/devices/"))
	if err != nil || id == "" {
		writeError(w, http.StatusBadRequest, "invalid_id", "invalid device id")
		return
	}
	switch r.Method {
	case http.MethodGet:
		d, err := s.store.GetDevice(r.Context(), id)
		if err != nil {
			writeDBError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "device": deviceViewOf(d)})
	case http.MethodPost, http.MethodPut:
		var req struct {
			Name      *string `json:"name"`
			Token     *string `json:"token"`
			URL       *string `json:"url"`
			RoomID    *string `json:"room_id"`
			Template  *string `json:"template"`
			CloudOnly *bool   `json:"cloud_only"`
			Kind      string  `json:"kind"`
		}
		if err := decode(r, &req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid_payload", err.Error())
			return
		}
		// Ensure the row exists without changing the kind of an existing
		// device (a Tuya device must not turn into a thing on a rename).
		if _, err := s.store.GetDevice(r.Context(), id); errors.Is(err, db.ErrNotFound) {
			kind := req.Kind
			if kind == "" {
				kind = db.KindThing
			}
			if err := s.store.UpsertDevice(r.Context(), db.Device{ID: id, Kind: kind}); err != nil {
				writeDBError(w, err)
				return
			}
		}
		if req.Name != nil {
			if err := s.store.SetDeviceName(r.Context(), id, *req.Name); err != nil {
				writeDBError(w, err)
				return
			}
		}
		if req.Token != nil {
			if err := s.store.SetDeviceToken(r.Context(), id, *req.Token); err != nil {
				writeDBError(w, err)
				return
			}
		}
		if req.URL != nil {
			if err := s.store.SetDeviceURL(r.Context(), id, *req.URL); err != nil {
				writeDBError(w, err)
				return
			}
		}
		if req.RoomID != nil {
			if err := s.store.SetDeviceRoom(r.Context(), id, *req.RoomID); err != nil {
				writeDBError(w, err)
				return
			}
		}
		if req.Template != nil {
			if err := s.store.SetDeviceTemplate(r.Context(), id, *req.Template); err != nil {
				writeDBError(w, err)
				return
			}
		}
		if req.CloudOnly != nil {
			if err := s.store.SetDeviceCloudOnly(r.Context(), id, *req.CloudOnly); err != nil {
				writeDBError(w, err)
				return
			}
		}
		d, err := s.store.GetDevice(r.Context(), id)
		if err != nil {
			writeDBError(w, err)
			return
		}
		// A gateway update (token/URL) should discover its devices right away.
		if d.Kind == db.KindThing && (req.Token != nil || req.URL != nil) {
			s.probeAsync(id)
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "device": deviceViewOf(d)})
	case http.MethodDelete:
		if err := s.store.DeleteDevice(r.Context(), id); err != nil {
			writeDBError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	default:
		methodNotAllowed(w)
	}
}

func (s *Server) rooms(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		list, err := s.store.ListRooms(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, "db", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "rooms": list})
	case http.MethodPost:
		var room db.Room
		if err := decode(r, &room); err != nil {
			writeError(w, http.StatusBadRequest, "invalid_payload", err.Error())
			return
		}
		if err := s.store.UpsertRoom(r.Context(), room); err != nil {
			writeError(w, http.StatusBadRequest, "invalid_room", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	default:
		methodNotAllowed(w)
	}
}

func (s *Server) roomRoute(w http.ResponseWriter, r *http.Request) {
	id, err := url.PathUnescape(strings.TrimPrefix(r.URL.Path, "/api/rooms/"))
	if err != nil || id == "" {
		writeError(w, http.StatusBadRequest, "invalid_id", "invalid room id")
		return
	}
	switch r.Method {
	case http.MethodGet:
		room, err := s.store.GetRoom(r.Context(), id)
		if err != nil {
			writeDBError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "room": room})
	case http.MethodPost, http.MethodPut:
		var room db.Room
		if err := decode(r, &room); err != nil {
			writeError(w, http.StatusBadRequest, "invalid_payload", err.Error())
			return
		}
		room.ID = id
		if err := s.store.UpsertRoom(r.Context(), room); err != nil {
			writeError(w, http.StatusBadRequest, "invalid_room", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	case http.MethodDelete:
		if err := s.store.DeleteRoom(r.Context(), id); err != nil {
			writeDBError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	default:
		methodNotAllowed(w)
	}
}

func (s *Server) placements(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		list, err := s.store.Placements(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, "db", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "placements": list})
	case http.MethodPost:
		var p db.Placement
		if err := decode(r, &p); err != nil {
			writeError(w, http.StatusBadRequest, "invalid_payload", err.Error())
			return
		}
		if p.Kind == "" {
			p.Kind = db.PlacementFan
		}
		if err := s.store.SetPlacement(r.Context(), p); err != nil {
			writeError(w, http.StatusBadRequest, "invalid_placement", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	default:
		methodNotAllowed(w)
	}
}

func writeDBError(w http.ResponseWriter, err error) {
	if errors.Is(err, db.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", err.Error())
		return
	}
	writeError(w, http.StatusInternalServerError, "db", err.Error())
}
