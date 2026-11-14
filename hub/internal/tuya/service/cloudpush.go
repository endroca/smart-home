package service

import (
	"context"
	"log"
	"time"

	"smart_home/hub/internal/db"
	"smart_home/hub/internal/tuya"
	"smart_home/hub/internal/tuya/cloudstream"
)

// startCloudPush runs the Tuya Message Service (Pulsar) listener for the life of
// the process. It is gated: it only connects while at least one cloud_only
// device exists, so a project with no sleeping device never consumes the message
// quota. It is a no-op when the feature is disabled or the cloud is not
// configured.
func (s *Service) startCloudPush(ctx context.Context) {
	if !s.cfg.CloudPush || s.cloud == nil {
		return
	}
	go s.runCloudPush(ctx)
}

// runCloudPush keeps a Pulsar consumer alive, retrying with backoff. The Pulsar
// client reconnects on its own; this loop handles the cases it gives up on (bad
// credentials, service not enabled, the subscription taken by another consumer).
func (s *Service) runCloudPush(ctx context.Context) {
	cfg := cloudstream.Config{
		AccessID:     s.cfg.Cloud.AccessID,
		AccessSecret: s.cfg.Cloud.AccessSecret,
		Region:       s.cfg.Cloud.Region,
		Test:         s.cfg.CloudPushTest,
	}
	const maxBackoff = 10 * time.Minute
	backoff := 30 * time.Second
	for {
		if ctx.Err() != nil {
			return
		}
		if !s.hasCloudOnlyDevice(ctx) {
			// Nothing to consume yet; wait so we do not burn message quota.
			if !sleepCtx(ctx, 30*time.Second) {
				return
			}
			continue
		}
		err := cloudstream.Run(ctx, cfg, s.ingestCloudReport)
		if ctx.Err() != nil {
			return
		}
		log.Printf("[tuya] cloud push indisponível: %v (nova tentativa em %s; confira o Message Service/regra em iot.tuya.com)", err, backoff)
		if !sleepCtx(ctx, backoff) {
			return
		}
		if backoff < maxBackoff {
			backoff *= 2
			if backoff > maxBackoff {
				backoff = maxBackoff
			}
		}
	}
}

func (s *Service) hasCloudOnlyDevice(ctx context.Context) bool {
	devices, err := s.store.ListDevices(ctx, db.KindTuya)
	if err != nil {
		return false
	}
	for _, d := range devices {
		if d.CloudOnly {
			return true
		}
	}
	return false
}

// ingestCloudReport turns one pushed report into events, reusing the same
// durable cursor as the report-log poll so push and poll never double-fire. It
// is the push counterpart of processReports: same seeding, same lookback, same
// "fire once per event_time" rule.
func (s *Service) ingestCloudReport(rep cloudstream.Report) {
	if rep.DeviceID == "" || len(rep.DPs) == 0 {
		return
	}
	ctx := context.Background()
	dev, err := s.store.GetDevice(ctx, rep.DeviceID)
	if err != nil || !dev.CloudOnly {
		return
	}
	cursor, haveCursor, err := s.store.ReportCursor(ctx, rep.DeviceID)
	if err != nil {
		return
	}
	byCode := s.codeToDP(ctx, rep.DeviceID)

	max := int64(0)
	if !haveCursor {
		// First contact: observe the current position and fire nothing, so
		// enabling push never replays history into automations.
		for _, dp := range rep.DPs {
			if dp.At > max {
				max = dp.At
			}
		}
		if max > 0 {
			_ = s.store.SetReportCursor(ctx, rep.DeviceID, max)
		}
		return
	}

	floor := time.Now().Add(-s.reportLookback()).UnixMilli()
	for _, dp := range rep.DPs {
		if dp.At > max {
			max = dp.At
		}
		if dp.Code == "" || dp.At <= cursor {
			continue
		}
		if dp.At < floor {
			// Older than the lookback window (a backlog after a long outage):
			// mark it seen without firing.
			continue
		}
		s.publishReport(rep.DeviceID, byCode, tuya.ReportLog{Code: dp.Code, EventTime: dp.At, Value: dp.Value})
	}
	if max > cursor {
		_ = s.store.SetReportCursor(ctx, rep.DeviceID, max)
	}
}

// sleepCtx waits for d unless ctx ends first; it reports whether the wait
// completed.
func sleepCtx(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}
