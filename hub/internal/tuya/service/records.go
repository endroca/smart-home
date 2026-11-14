package service

import (
	"context"
	"errors"
	"time"

	"smart_home/hub/internal/tuya"
)

// Reasons reported when the unlocking history cannot be produced. They mirror
// the energy reasons so the UI can explain the empty state the same way.
const (
	RecordsReasonCloudNotConfigured = "cloud_not_configured"
	RecordsReasonNotSubscribed      = "not_subscribed"
	RecordsReasonCloudError         = "cloud_error"
)

// OpenRecord is one unlocking record served to the UI. It is the cloud type
// (tuya.OpenRecord) so the HTTP layer serializes it directly.
type OpenRecord = tuya.OpenRecord

// OpenRecordsResult is the answer served to the UI for one device. Only
// Available is required for the UI to decide between the list and the
// explanatory empty state.
type OpenRecordsResult struct {
	Available bool              `json:"available"`
	Reason    string            `json:"reason,omitempty"`
	Message   string            `json:"message,omitempty"`
	Total     int               `json:"total"`
	Records   []tuya.OpenRecord `json:"records"`
	UpdatedAt time.Time         `json:"updated_at"`
}

const (
	openRecordsDefaultLimit = 30
	openRecordsMaxLimit     = 100
	// openRecordsWindow is how far back the history looks. The Tuya logs are
	// short-lived (a few days), so a wider window would only cost a bigger
	// (mostly empty) response.
	openRecordsWindow = 30 * 24 * time.Hour
)

// OpenRecords returns the recent unlocking records of a lock using the Smart
// Lock Open Service. Cloud failures are reported inside the result
// (Available=false) so the endpoint can always answer 200; only a missing
// device is a hard error.
func (s *Service) OpenRecords(ctx context.Context, id string, limit int) (OpenRecordsResult, error) {
	if _, err := s.store.GetDevice(ctx, id); err != nil {
		return OpenRecordsResult{}, err
	}
	if limit <= 0 {
		limit = openRecordsDefaultLimit
	}
	if limit > openRecordsMaxLimit {
		limit = openRecordsMaxLimit
	}

	out := OpenRecordsResult{Records: []tuya.OpenRecord{}, UpdatedAt: time.Now()}
	if s.cloud == nil {
		out.Reason = RecordsReasonCloudNotConfigured
		out.Message = "cloud credentials are not configured"
		return out, nil
	}

	now := time.Now()
	records, total, err := s.cloud.OpenLogs(ctx, id, now.Add(-openRecordsWindow).UnixMilli(), now.UnixMilli(), 1, limit)
	if err != nil {
		switch {
		case errors.Is(err, tuya.ErrNotSubscribed):
			out.Reason = RecordsReasonNotSubscribed
		default:
			out.Reason = RecordsReasonCloudError
		}
		out.Message = err.Error()
		return out, nil
	}
	out.Available = true
	out.Total = total
	out.Records = records
	return out, nil
}
