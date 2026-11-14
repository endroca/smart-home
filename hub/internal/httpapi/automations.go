package httpapi

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"

	"smart_home/hub/internal/db"
	"smart_home/hub/internal/dispatch"
)

// SetDispatcher attaches the automation dispatcher. When it is nil the
// automations are still stored but never fired.
func (s *Server) SetDispatcher(d *dispatch.Dispatcher) { s.dispatcher = d }

func (s *Server) automationRoutes(mux *http.ServeMux) {
	if s.store == nil {
		return
	}
	mux.HandleFunc("/api/automations", s.automations)
	mux.HandleFunc("/api/automations/", s.automationRoute)
}

func (s *Server) automations(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		list, err := dispatch.Load(r.Context(), s.store)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "db", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "automations": list})
	case http.MethodPost:
		var a dispatch.Automation
		if err := decode(r, &a); err != nil {
			writeError(w, http.StatusBadRequest, "invalid_payload", err.Error())
			return
		}
		if err := s.saveAutomation(r, &a); err != nil {
			writeError(w, http.StatusBadRequest, "invalid_automation", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "automation": a})
	default:
		methodNotAllowed(w)
	}
}

func (s *Server) automationRoute(w http.ResponseWriter, r *http.Request) {
	rest := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/automations/"), "/")
	parts := strings.Split(rest, "/")
	id, err := url.PathUnescape(parts[0])
	if err != nil || id == "" {
		writeError(w, http.StatusBadRequest, "invalid_id", "invalid automation id")
		return
	}

	if len(parts) == 2 && parts[1] == "run" {
		if r.Method != http.MethodPost {
			methodNotAllowed(w)
			return
		}
		if s.dispatcher == nil {
			writeError(w, http.StatusServiceUnavailable, "dispatch_unavailable", "the dispatcher is not available")
			return
		}
		n, err := s.dispatcher.Run(r.Context(), id)
		if err != nil {
			if errors.Is(err, db.ErrNotFound) {
				writeError(w, http.StatusNotFound, "not_found", err.Error())
				return
			}
			writeError(w, http.StatusBadRequest, "invalid_automation", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "actions": n})
		return
	}

	switch r.Method {
	case http.MethodGet:
		list, err := dispatch.Load(r.Context(), s.store)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "db", err.Error())
			return
		}
		for _, a := range list {
			if a.ID == id {
				writeJSON(w, http.StatusOK, map[string]any{"ok": true, "automation": a})
				return
			}
		}
		writeError(w, http.StatusNotFound, "not_found", "automation not found")
	case http.MethodPost, http.MethodPut:
		var a dispatch.Automation
		if err := decode(r, &a); err != nil {
			writeError(w, http.StatusBadRequest, "invalid_payload", err.Error())
			return
		}
		a.ID = id
		if err := s.saveAutomation(r, &a); err != nil {
			writeError(w, http.StatusBadRequest, "invalid_automation", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "automation": a})
	case http.MethodDelete:
		if err := s.store.DeleteAutomation(r.Context(), id); err != nil {
			writeError(w, http.StatusInternalServerError, "db", err.Error())
			return
		}
		s.reloadDispatcher(r)
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	default:
		methodNotAllowed(w)
	}
}

func (s *Server) saveAutomation(r *http.Request, a *dispatch.Automation) error {
	if a.Source.Type == "" {
		a.Source.Type = dispatch.SourceManual
	}
	if a.ID == "" {
		a.ID = newAutomationID()
	}
	if err := dispatch.Validate(*a); err != nil {
		return err
	}
	// Resolve a time trigger's instant here so the response the UI stores already
	// carries fire_at (Save resolves it again, which is a no-op).
	if err := dispatch.Resolve(a, time.Now()); err != nil {
		return err
	}
	if err := dispatch.Save(r.Context(), s.store, *a); err != nil {
		return err
	}
	s.reloadDispatcher(r)
	return nil
}

func (s *Server) reloadDispatcher(r *http.Request) {
	if s.dispatcher != nil {
		s.dispatcher.Reload(r.Context())
	}
}

func newAutomationID() string {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "automation"
	}
	return "auto-" + hex.EncodeToString(b[:])
}
