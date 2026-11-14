package midea

import (
	"context"
	"fmt"
	"time"
)

// EnergyRange is the consumption window exposed to the UI. The appliance only
// reports the totals (lifetime and the current run) plus the instantaneous
// power, so a range changes the reading shown, not a stored history.
type EnergyRange string

const (
	EnergyRun   EnergyRange = "run"   // since the current run started
	EnergyTotal EnergyRange = "total" // lifetime counter
)

// Reasons reported when the consumption cannot be read.
const (
	EnergyReasonLANOnly = "lan_only"
)

// EnergyPoint is one bar of the consumption chart. The appliance has two
// counters, so the series is short by nature.
type EnergyPoint struct {
	Time  string  `json:"time"`
	Label string  `json:"label"`
	Value float64 `json:"value"`
}

// EnergySeries is the answer served to the UI for one appliance/range. Only
// Available is required for the UI to decide between the chart and the
// explanatory empty state.
type EnergySeries struct {
	Available bool          `json:"available"`
	Reason    string        `json:"reason,omitempty"`
	Message   string        `json:"message,omitempty"`
	Range     string        `json:"range"`
	Unit      string        `json:"unit"`
	Total     float64       `json:"total"`
	Points    []EnergyPoint `json:"points"`
	// PowerNow is the instantaneous draw in W (0 when the appliance is off or
	// the model does not report it).
	PowerNow  float64   `json:"power_now"`
	UpdatedAt time.Time `json:"updated_at"`
}

// Energy reads the consumption counters directly from the appliance. The
// counters are cumulative, so the value is cached briefly to avoid a LAN round
// trip per render.
func (s *Service) Energy(ctx context.Context, id, rangeStr string) (EnergySeries, error) {
	if _, err := s.cfg.Store.GetDevice(ctx, id); err != nil {
		return EnergySeries{}, err
	}
	r := normalizeEnergyRange(rangeStr)
	now := time.Now()

	s.mu.Lock()
	if cached, ok := s.energyCache[id+"|"+string(r)]; ok && now.Sub(cached.at) < 30*time.Second {
		s.mu.Unlock()
		return cached.series, nil
	}
	s.mu.Unlock()

	d, found, err := s.resolve(ctx, id)
	if err != nil {
		return EnergySeries{}, err
	}
	c, err := s.client(d, found.ID)
	if err != nil {
		return EnergySeries{}, err
	}
	if err = c.Connect(ctx); err != nil {
		return EnergySeries{}, err
	}
	reading, err := c.Energy(ctx)
	if err != nil {
		return EnergySeries{}, fmt.Errorf("midea: read energy: %w", err)
	}

	series := EnergySeries{
		Available: true,
		Range:     string(r),
		Unit:      "kWh",
		PowerNow:  round(reading.RealtimeKW * 1000),
		UpdatedAt: now,
	}
	if r == EnergyTotal {
		series.Total = round(reading.TotalKWh)
	} else {
		series.Total = round(reading.CurrentRunKWh)
	}
	series.Points = []EnergyPoint{
		{Time: "run", Label: "Ciclo atual", Value: round(reading.CurrentRunKWh)},
		{Time: "total", Label: "Acumulado", Value: round(reading.TotalKWh)},
	}

	s.mu.Lock()
	if s.energyCache == nil {
		s.energyCache = map[string]energyCacheEntry{}
	}
	s.energyCache[id+"|"+string(r)] = energyCacheEntry{at: now, series: series}
	s.mu.Unlock()
	return series, nil
}

type energyCacheEntry struct {
	at     time.Time
	series EnergySeries
}

// RefreshStates reads every registered appliance once. The dispatcher calls it
// when a new rule starts listening, so an automation does not wait a whole
// polling interval for the first state.
func (s *Service) RefreshStates(ctx context.Context) {
	devices, err := s.Devices(ctx)
	if err != nil {
		return
	}
	for _, device := range devices {
		if ctx.Err() != nil {
			return
		}
		before, _ := s.cfg.Store.MideaStates(ctx, device.ID)
		if _, err := s.State(ctx, device.ID); err != nil {
			continue
		}
		after, _ := s.cfg.Store.MideaStates(ctx, device.ID)
		s.publishChange(device.ID, before, after)
	}
}

func normalizeEnergyRange(value string) EnergyRange {
	if value == string(EnergyTotal) {
		return EnergyTotal
	}
	return EnergyRun
}

func round(v float64) float64 {
	return float64(int64(v*100+0.5)) / 100
}
