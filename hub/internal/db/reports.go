package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

// ReportCursor returns the last report event_time already processed for a
// cloud_only device. ok is false when the device has no cursor yet (the caller
// must seed it instead of firing automations for old events).
func (s *Store) ReportCursor(ctx context.Context, deviceID string) (int64, bool, error) {
	if strings.TrimSpace(deviceID) == "" {
		return 0, false, errors.New("db: device id is required")
	}
	var ts int64
	err := s.db.QueryRowContext(ctx,
		`SELECT last_event_time FROM tuya_report_cursor WHERE device_id = ?`, deviceID).Scan(&ts)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, fmt.Errorf("db: report cursor %s: %w", deviceID, err)
	}
	return ts, true, nil
}

// SetReportCursor stores the last report event_time processed for a device.
func (s *Store) SetReportCursor(ctx context.Context, deviceID string, eventTime int64) error {
	if strings.TrimSpace(deviceID) == "" {
		return errors.New("db: device id is required")
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO tuya_report_cursor (device_id, last_event_time, updated_at)
		 VALUES (?, ?, ?)
		 ON CONFLICT(device_id) DO UPDATE SET
		     last_event_time = excluded.last_event_time,
		     updated_at      = excluded.updated_at`,
		deviceID, eventTime, formatTime(time.Now().UTC()))
	if err != nil {
		return fmt.Errorf("db: set report cursor %s: %w", deviceID, err)
	}
	return nil
}
