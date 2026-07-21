package energy

import (
	"testing"
	"time"
)

func TestAccumulatorMeasuredAndMissingSamples(t *testing.T) {
	t.Parallel()
	start := time.Date(2026, 7, 21, 10, 0, 0, 0, time.UTC)
	accumulator, err := NewAccumulator(start, start.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	first, second := 400.0, 1000.0
	accumulator, _ = accumulator.Add(start.Add(10*time.Second), &first, 1, QualityMeasured)
	accumulator, _ = accumulator.Add(start.Add(20*time.Second), &second, 2, QualityCalculated)
	accumulator = accumulator.Missing(1)
	result := accumulator.Result()
	if result.EnergyConsumedWh != 3 || result.AveragePowerW != 700 || result.MinimumPowerW != 400 || result.MaximumPowerW != 1000 || result.SampleCount != 2 || result.MissingSampleCount != 1 || result.Quality != QualityPartial {
		t.Fatalf("aggregate = %+v", result)
	}
}

func TestIntervalBoundsHandlesLocalCivilDay(t *testing.T) {
	t.Parallel()
	location, err := time.LoadLocation("Europe/Lisbon")
	if err != nil {
		t.Fatal(err)
	}
	instant := time.Date(2026, 3, 29, 12, 0, 0, 0, location)
	start, end, err := IntervalBounds(instant, "day", location)
	if err != nil || end.Day() != 30 || start.Day() != 29 {
		t.Fatalf("day bounds = %s..%s: %v", start, end, err)
	}
	if end.Sub(start) == 24*time.Hour {
		t.Fatal("DST transition day should not be forced to 24 elapsed hours")
	}
}

func TestPruneAccumulatorsRetainsOnlyOpenIntervals(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 7, 21, 12, 1, 0, 0, time.UTC)
	closed, _ := NewAccumulator(now.Add(-time.Minute), now)
	open, _ := NewAccumulator(now, now.Add(time.Minute))
	values := map[string]Accumulator{"closed": closed, "open": open}
	pruneAccumulators(values, now)
	if len(values) != 1 {
		t.Fatalf("retained %d accumulators, want 1", len(values))
	}
	if _, exists := values["open"]; !exists {
		t.Fatal("open accumulator was pruned")
	}
}
