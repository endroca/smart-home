package httpapi

import (
	"context"
	"errors"
	"net/http"
	"time"

	"smart_home/hub/internal/db"
)

// mideaEnergy serves the consumption counters read from the appliance. Like
// the Tuya energy route it always answers 200 for device failures: the body
// carries Available=false plus a machine-readable reason.
func (s *Server) mideaEnergy(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	id := r.URL.Query().Get("id")
	if id == "" {
		writeError(w, http.StatusBadRequest, "invalid_payload", "id is required")
		return
	}
	if s.midea == nil {
		writeError(w, http.StatusServiceUnavailable, "midea_energy", "midea is not configured")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	series, err := s.midea.Energy(ctx, id, r.URL.Query().Get("range"))
	if err != nil {
		status := http.StatusBadGateway
		if errors.Is(err, db.ErrNotFound) {
			status = http.StatusNotFound
		}
		writeError(w, status, "midea_energy", err.Error())
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
		"power_now":  series.PowerNow,
		"points":     series.Points,
		"updated_at": series.UpdatedAt,
	})
}
