package web

import (
	"testing"
	"time"

	"github.com/filipegalo/shelly-energy-dashboard/internal/database"
)

func TestDetectHourlyAnomaliesRequiresCompleteComparableIncrease(t *testing.T) {
	location := time.UTC
	recentStart := time.Date(2026, time.September, 14, 0, 0, 0, 0, location)
	candidate := recentStart.Add(13 * time.Hour)
	aggregates := make([]database.EnergyAggregate, 0, 7)
	for week := 1; week <= 4; week++ {
		at := candidate.AddDate(0, 0, -7*week)
		aggregates = append(aggregates, database.EnergyAggregate{IntervalStart: at, EnergyConsumedWh: 1000, SampleCount: 4, CalculationQuality: "measured"})
	}
	aggregates = append(aggregates,
		database.EnergyAggregate{IntervalStart: candidate, EnergyConsumedWh: 1800, SampleCount: 4, CalculationQuality: "measured"},
		database.EnergyAggregate{IntervalStart: candidate.Add(time.Hour), EnergyConsumedWh: 3000, SampleCount: 4, MissingSampleCount: 1, CalculationQuality: "partial"},
	)

	items := detectHourlyAnomalies(aggregates, recentStart, recentStart.AddDate(0, 0, 7), location)
	if len(items) != 1 {
		t.Fatalf("anomalies = %#v, want one complete high-consumption hour", items)
	}
	if items[0].Actual != "1.80 kWh" || items[0].Typical != "1.00 kWh" || items[0].Increase != "+0.80 kWh" {
		t.Fatalf("anomaly = %#v", items[0])
	}
}
