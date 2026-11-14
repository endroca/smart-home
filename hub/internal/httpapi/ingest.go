package httpapi

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"net/http"
	"strings"

	"smart_home/hub/internal/model"
)

// deviceAuth verifies the credential a gateway uses to push its own events.
//
// The firmware stores the LAN token only as a SHA-256 digest, so it cannot send
// the plaintext back. It authenticates with the hex digest instead, and the hub
// checks it against the digest of the token it already holds for this gateway.
// The plaintext form is also accepted, so an operator can test the endpoint with
// curl using the same token the hub uses in the other direction.
func (s *Server) deviceAuth(r *http.Request, thing string) bool {
	if s.store == nil {
		return false
	}
	want := s.store.DeviceToken(thing)
	if want == "" {
		return false
	}
	header := r.Header.Get("Authorization")
	const prefix = "Bearer "
	if !strings.HasPrefix(header, prefix) {
		return false
	}
	got := strings.TrimSpace(strings.TrimPrefix(header, prefix))
	if got == "" {
		return false
	}
	if constantTimeEqual(got, want) {
		return true
	}
	sum := sha256.Sum256([]byte(want))
	return constantTimeEqual(strings.ToLower(got), hex.EncodeToString(sum[:]))
}

func constantTimeEqual(a, b string) bool {
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}

// ingest receives a push from a gateway. The device is the single source of
// truth, so a push only refreshes what the hub already mirrors: its state, its
// status (heartbeat) and its asynchronous events.
//
//	POST /api/things/{thing}/events
//	{ "type": "state",  "id": "fan0", "reported": { ... } }
//	{ "type": "status", "status": { ... } }
//	{ "type": "event",  "event": { ... } }
func (s *Server) ingest(w http.ResponseWriter, r *http.Request, thing string) {
	if !s.deviceAuth(r, thing) {
		writeError(w, http.StatusUnauthorized, "unauthorized", "invalid device token")
		return
	}
	var req struct {
		Type     string         `json:"type"`
		DeviceID string         `json:"id"`
		Reported map[string]any `json:"reported"`
		Status   *model.Status  `json:"status"`
		Event    map[string]any `json:"event"`
	}
	if err := decode(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_payload", err.Error())
		return
	}

	var err error
	switch req.Type {
	case "state":
		err = s.hub.IngestState(thing, req.DeviceID, req.Reported)
	case "status":
		err = s.hub.IngestStatus(thing, req.Status)
	case "event":
		err = s.hub.IngestEvent(thing, req.Event)
	default:
		writeError(w, http.StatusBadRequest, "invalid_payload", "type must be one of state, status, event")
		return
	}
	if err != nil {
		writeHubError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}
