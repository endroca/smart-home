package httpapi

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"smart_home/hub/internal/db"
	"smart_home/hub/internal/functions"
)

// offRoutes registers the generic "desligar tudo" endpoint. It needs the store
// and the function service, since what "off" means is derived per device.
func (s *Server) offRoutes(mux *http.ServeMux) {
	if s.store == nil || s.functions == nil {
		return
	}
	mux.HandleFunc("/api/off", s.turnOff)
}

// offResult is the outcome for one device of a "desligar tudo" request.
type offResult struct {
	ID      string `json:"id"`
	OK      bool   `json:"ok"`
	Skipped bool   `json:"skipped,omitempty"`
	Error   string `json:"error,omitempty"`
}

// turnOff turns a set of devices off, using each device's function catalog: the
// functions marked "off", with their configured value. It runs sequentially and
// spaces fan RF commands, which collide if sent as a burst.
func (s *Server) turnOff(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	var req struct {
		IDs []string `json:"ids"`
	}
	if err := decode(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_payload", err.Error())
		return
	}
	results := s.runOff(r.Context(), req.IDs)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "results": results})
}

// runOff turns every requested device off in order, collecting a per-device
// result so the UI can report partial failures.
func (s *Server) runOff(ctx context.Context, ids []string) []offResult {
	results := make([]offResult, 0, len(ids))
	fanPending := false
	for _, id := range ids {
		d, err := s.store.GetDevice(ctx, id)
		if err != nil {
			results = append(results, offResult{ID: id, Error: err.Error()})
			continue
		}
		cmds, err := s.functions.Off(ctx, id)
		if err != nil {
			results = append(results, offResult{ID: id, Error: err.Error()})
			continue
		}
		if len(cmds) == 0 {
			results = append(results, offResult{ID: id, Skipped: true})
			continue
		}
		if err := s.offDevice(ctx, d, cmds, &fanPending); err != nil {
			results = append(results, offResult{ID: id, Error: err.Error()})
			continue
		}
		results = append(results, offResult{ID: id, OK: true})
	}
	return results
}

// offDevice runs the off commands of one device on its transport.
func (s *Server) offDevice(ctx context.Context, d db.Device, cmds []functions.Command, fanPending *bool) error {
	switch d.Kind {
	case db.KindFan:
		if d.Owner == "" || d.DeviceID == "" {
			return errors.New("fan device has no gateway")
		}
		for _, c := range cmds {
			if *fanPending {
				select {
				case <-ctx.Done():
					return ctx.Err()
				case <-time.After(s.fanSpacing):
				}
			}
			*fanPending = true
			// force re-sends the RF frame even if the hub thinks it is already
			// off: the operator asked explicitly.
			if _, err := s.hub.SetState(ctx, d.Owner, d.DeviceID, map[string]any{c.Key: c.Value}, true); err != nil {
				return err
			}
		}
		return nil
	case db.KindTuya:
		if s.tuya == nil {
			return errors.New("tuya control is not available")
		}
		for _, c := range cmds {
			if _, err := s.tuya.Control(ctx, d.ID, c.Key, c.Value); err != nil {
				return err
			}
		}
		return nil
	case db.KindMidea:
		if s.midea == nil {
			return errors.New("midea control is not available")
		}
		state := make(map[string]any, len(cmds))
		for _, c := range cmds {
			state[c.Key] = c.Value
		}
		_, err := s.midea.Apply(ctx, d.ID, state)
		return err
	default:
		return fmt.Errorf("cannot turn off a %s device", d.Kind)
	}
}
