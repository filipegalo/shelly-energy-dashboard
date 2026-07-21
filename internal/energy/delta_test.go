package energy

import (
	"math"
	"testing"
	"time"
)

func TestCalculateDeltaCumulativeAndReset(t *testing.T) {
	t.Parallel()
	start := time.Date(2026, 7, 21, 10, 0, 0, 0, time.UTC)
	previousTotal, currentTotal := 1000.0, 1002.5
	state := DeltaState{RecordedAt: start, ImportedTotalWh: &previousTotal}
	next, result, err := CalculateDelta(state, start.Add(10*time.Second), nil, &currentTotal, 10*time.Second, 5000)
	if err != nil || result.EnergyWh != 2.5 || result.Quality != QualityMeasured || next.RecordedAt.IsZero() {
		t.Fatalf("measured delta = %+v, %+v, %v", next, result, err)
	}
	resetTotal := 2.0
	_, reset, err := CalculateDelta(next, start.Add(20*time.Second), nil, &resetTotal, 10*time.Second, 5000)
	if err != nil || reset.EnergyWh != 0 || !reset.CounterReset || reset.Quality != QualityCounterReset {
		t.Fatalf("reset delta = %+v, %v", reset, err)
	}
}

func TestCalculateDeltaPowerFallbackAndMissingGap(t *testing.T) {
	t.Parallel()
	start := time.Date(2026, 7, 21, 10, 0, 0, 0, time.UTC)
	before, after := 360.0, 720.0
	state := DeltaState{RecordedAt: start, PowerW: &before}
	_, result, err := CalculateDelta(state, start.Add(10*time.Second), &after, nil, 10*time.Second, 5000)
	want := 1.5
	if err != nil || math.Abs(result.EnergyWh-want) > 0.000001 || result.Quality != QualityCalculated {
		t.Fatalf("integrated delta = %+v, want %v: %v", result, want, err)
	}
	_, missing, err := CalculateDelta(state, start.Add(30*time.Second), &after, nil, 10*time.Second, 5000)
	if err != nil || missing.EnergyWh != 0 || missing.Quality != QualityPartial {
		t.Fatalf("missing delta = %+v: %v", missing, err)
	}
}

func TestCalculateDeltaRejectsInvalidReadings(t *testing.T) {
	t.Parallel()
	now := time.Now()
	nan, negative, huge := math.NaN(), -1.0, 9000.0
	for name, power := range map[string]*float64{"non-finite": &nan, "negative": &negative, "too large": &huge} {
		if _, _, err := CalculateDelta(DeltaState{}, now, power, nil, 10*time.Second, 5000); err == nil {
			t.Fatalf("%s reading was accepted", name)
		}
	}
}
