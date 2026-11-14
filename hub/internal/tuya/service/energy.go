package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"time"

	"smart_home/hub/internal/tuya"
)

// EnergyRange is the chart window exposed to the UI.
type EnergyRange string

const (
	EnergyToday EnergyRange = "today"
	Energy7d    EnergyRange = "7d"
	Energy30d   EnergyRange = "30d"
)

// Reasons reported when the consumption series cannot be produced.
const (
	EnergyReasonCloudNotConfigured = "cloud_not_configured"
	EnergyReasonNotSubscribed      = "not_subscribed"
	EnergyReasonCloudError         = "cloud_error"
)

// EnergyPoint is one bar of the consumption chart.
type EnergyPoint struct {
	// Time is the raw cloud bucket key (yyyyMMddHH for hours, yyyyMMdd for days).
	Time string `json:"time"`
	// Label is the short pt-BR label shown under the bar.
	Label string `json:"label"`
	// Value is the consumed energy in kWh.
	Value float64 `json:"value"`
}

// EnergySeries is the answer served to the UI for one device/range. It is
// intentionally self-describing: only Available is required for the UI to
// decide between the chart and the explanatory empty state.
type EnergySeries struct {
	Available bool          `json:"available"`
	Reason    string        `json:"reason,omitempty"`
	Message   string        `json:"message,omitempty"`
	Range     string        `json:"range"`
	Unit      string        `json:"unit"`
	Total     float64       `json:"total"`
	Points    []EnergyPoint `json:"points"`
	UpdatedAt time.Time     `json:"updated_at"`
}

type energyCacheEntry struct {
	at     time.Time
	ttl    time.Duration
	series EnergySeries
}

// EnergyTrend returns the cloud energy-consumption series for a device and
// range. Cloud failures are reported inside the series (Available=false) so the
// endpoint can always answer 200; only a missing device is a hard error.
func (s *Service) EnergyTrend(ctx context.Context, id, rangeStr string) (EnergySeries, error) {
	if _, err := s.store.GetDevice(ctx, id); err != nil {
		return EnergySeries{}, err
	}
	r := normalizeEnergyRange(rangeStr)
	now := time.Now()
	if s.now != nil {
		now = s.now()
	}

	key := id + "|" + string(r)
	s.energyMu.Lock()
	if e, ok := s.energyCache[key]; ok && now.Before(e.at.Add(e.ttl)) {
		s.energyMu.Unlock()
		return e.series, nil
	}
	s.energyMu.Unlock()

	series := s.buildEnergy(ctx, id, r, now)
	series.UpdatedAt = now
	series.Range = string(r)
	series.Unit = "kWh"

	ttl := energyTTL(r)
	if !series.Available {
		// Retry soon: the cloud may be temporarily unavailable or the trial
		// may have just been activated.
		ttl = time.Minute
	}
	s.energyMu.Lock()
	if s.energyCache == nil {
		s.energyCache = map[string]energyCacheEntry{}
	}
	s.energyCache[key] = energyCacheEntry{at: now, ttl: ttl, series: series}
	s.energyMu.Unlock()
	return series, nil
}

func (s *Service) buildEnergy(ctx context.Context, id string, r EnergyRange, now time.Time) EnergySeries {
	out := EnergySeries{Available: false, Points: []EnergyPoint{}}
	if s.cloud == nil {
		out.Reason = EnergyReasonCloudNotConfigured
		out.Message = "cloud credentials are not configured"
		return out
	}

	statType, times, labels := energyBuckets(r, now)

	// The Power Management statistics only exist when the device is bound to a
	// space, so they are frequently all zeros. The device report logs always
	// carry the metering counter, so they are tried first and the statistics
	// remain as a fallback for devices that do not report add_ele.
	if series, ok := s.energyFromLogs(ctx, id, r, now, times, labels); ok {
		return series
	}

	values := map[string]float64{}
	for _, chunk := range energyChunks(times, statType) {
		points, err := s.cloud.EnergyTrend(ctx, id, statType, chunk[0], chunk[1])
		if err != nil {
			switch {
			case errors.Is(err, tuya.ErrNotSubscribed):
				out.Reason = EnergyReasonNotSubscribed
			default:
				out.Reason = EnergyReasonCloudError
			}
			out.Message = err.Error()
			return out
		}
		for _, p := range points {
			values[p.Time] = p.Value
		}
	}

	out.Points = make([]EnergyPoint, len(times))
	total := 0.0
	for i, t := range times {
		v := values[t]
		total += v
		out.Points[i] = EnergyPoint{Time: t, Label: labels[i], Value: roundKwh(v)}
	}
	out.Available = true
	out.Total = roundKwh(total)
	return out
}

// energyFromLogs builds the series from the add_ele entries of the device
// report log. It returns ok=false when the cloud fails or the window has no
// records, so the caller can fall back to the statistics endpoint.
func (s *Service) energyFromLogs(ctx context.Context, id string, r EnergyRange, now time.Time, times, labels []string) (EnergySeries, bool) {
	start := energyLogStart(r, now)
	logs, err := s.cloud.EnergyLogs(ctx, id, []string{"add_ele"}, start.UnixMilli(), now.UnixMilli())
	if err != nil || len(logs) == 0 {
		return EnergySeries{}, false
	}

	// add_ele is an increment in units of 10^-scale kWh (scale 3 on the plugs
	// seen so far); without a cached spec, assume thousandths of a kWh.
	factor := math.Pow10(-s.energyScale(ctx, id))
	values := make(map[string]float64, len(times))
	for _, l := range logs {
		if l.Code != "add_ele" || l.Value <= 0 {
			continue
		}
		at := time.UnixMilli(l.EventTime).In(now.Location())
		values[energyLogBucket(r, at)] += l.Value * factor
	}

	out := EnergySeries{Available: true, Points: make([]EnergyPoint, len(times))}
	total := 0.0
	for i, key := range times {
		v := values[key]
		total += v
		out.Points[i] = EnergyPoint{Time: key, Label: labels[i], Value: roundKwh(v)}
	}
	out.Total = roundKwh(total)
	return out, true
}

// energyLogStart is the first instant of the window covered by a range.
func energyLogStart(r EnergyRange, now time.Time) time.Time {
	midnight := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	switch r {
	case Energy7d:
		return midnight.AddDate(0, 0, -6)
	case Energy30d:
		return midnight.AddDate(0, 0, -29)
	default:
		return midnight
	}
}

// energyLogBucket is the chart bucket key of one report timestamp. It matches
// the keys produced by energyBuckets.
func energyLogBucket(r EnergyRange, t time.Time) string {
	if r == EnergyToday {
		return t.Format("2006010215")
	}
	return t.Format("20060102")
}

// energyScale reads the add_ele decimal scale from the cached specifications.
func (s *Service) energyScale(ctx context.Context, id string) int {
	specs, err := s.store.ListSpecs(ctx, id)
	if err == nil {
		for _, spec := range specs {
			if spec.Code != "add_ele" || len(spec.Values) == 0 {
				continue
			}
			var values struct {
				Scale *int `json:"scale"`
			}
			if json.Unmarshal(spec.Values, &values) == nil && values.Scale != nil && *values.Scale >= 0 && *values.Scale <= 6 {
				return *values.Scale
			}
		}
	}
	return 3
}

// energyBuckets lists the expected buckets of a range, in chronological order,
// together with the label shown by the UI. The cloud only returns buckets that
// it has aggregated, so the caller zero-fills the gaps.
func energyBuckets(r EnergyRange, now time.Time) (statType string, times, labels []string) {
	switch r {
	case Energy7d:
		statType = "day"
		for i := 6; i >= 0; i-- {
			d := now.AddDate(0, 0, -i)
			times = append(times, d.Format("20060102"))
			labels = append(labels, energyDayLabel(d, now))
		}
	case Energy30d:
		statType = "day"
		for i := 29; i >= 0; i-- {
			d := now.AddDate(0, 0, -i)
			times = append(times, d.Format("20060102"))
			labels = append(labels, d.Format("02/01"))
		}
	default:
		statType = "hour"
		day := now.Format("20060102")
		for h := 0; h <= now.Hour(); h++ {
			times = append(times, fmt.Sprintf("%s%02d", day, h))
			labels = append(labels, fmt.Sprintf("%02dh", h))
		}
	}
	return statType, times, labels
}

// energyChunks splits the buckets into API calls. Day ranges are sent in
// windows of at most seven days (a safe limit for the Power Management API);
// hourly ranges fit in a single request.
func energyChunks(times []string, statType string) [][2]string {
	if len(times) == 0 {
		return nil
	}
	if statType != "day" {
		return [][2]string{{times[0], times[len(times)-1]}}
	}
	out := make([][2]string, 0, (len(times)+6)/7)
	for i := 0; i < len(times); i += 7 {
		j := i + 6
		if j >= len(times) {
			j = len(times) - 1
		}
		out = append(out, [2]string{times[i], times[j]})
	}
	return out
}

func energyDayLabel(d, now time.Time) string {
	start := func(t time.Time) time.Time {
		return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location())
	}
	days := int(start(now).Sub(start(d)).Hours() / 24)
	switch days {
	case 0:
		return "Hoje"
	case 1:
		return "Ontem"
	}
	if days > 1 && days < 7 {
		return [...]string{"dom", "seg", "ter", "qua", "qui", "sex", "sab"}[d.Weekday()]
	}
	return d.Format("02/01")
}

func normalizeEnergyRange(v string) EnergyRange {
	switch EnergyRange(v) {
	case Energy7d:
		return Energy7d
	case Energy30d:
		return Energy30d
	default:
		return EnergyToday
	}
}

func energyTTL(r EnergyRange) time.Duration {
	switch r {
	case Energy30d:
		return 6 * time.Hour
	case Energy7d:
		return 30 * time.Minute
	default:
		return 2 * time.Minute
	}
}

func roundKwh(v float64) float64 {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return 0
	}
	return math.Round(v*1000) / 1000
}
