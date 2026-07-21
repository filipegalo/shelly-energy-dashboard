package energy

import (
	"errors"
	"math"
	"time"
)

const (
	QualityMeasured     = "measured"
	QualityCalculated   = "calculated"
	QualityPartial      = "partial"
	QualityStale        = "stale"
	QualityCounterReset = "counter_reset"
)

// DeltaState is an immutable baseline for deriving consumption from readings.
type DeltaState struct {
	RecordedAt      time.Time
	ImportedTotalWh *float64
	PowerW          *float64
}

type DeltaResult struct {
	EnergyWh     float64
	Quality      string
	CounterReset bool
}

// CalculateDelta prefers device cumulative energy and integrates power only
// when no counter is available. The returned state is always a new value.
func CalculateDelta(previous DeltaState, recordedAt time.Time, powerW, importedTotalWh *float64, pollInterval time.Duration, maximumPowerW float64) (DeltaState, DeltaResult, error) {
	if recordedAt.IsZero() {
		return previous, DeltaResult{}, errors.New("reading timestamp is required")
	}
	if err := validateReadingNumber("power", powerW, maximumPowerW); err != nil {
		return previous, DeltaResult{}, err
	}
	if err := validateReadingNumber("imported energy", importedTotalWh, 0); err != nil {
		return previous, DeltaResult{}, err
	}
	next := DeltaState{RecordedAt: recordedAt, PowerW: copyFloat(powerW), ImportedTotalWh: copyFloat(importedTotalWh)}
	if previous.RecordedAt.IsZero() {
		return next, DeltaResult{Quality: QualityPartial}, nil
	}
	elapsed := recordedAt.Sub(previous.RecordedAt)
	if elapsed <= 0 {
		return previous, DeltaResult{}, errors.New("reading timestamp did not advance")
	}

	if importedTotalWh != nil && previous.ImportedTotalWh != nil {
		delta := *importedTotalWh - *previous.ImportedTotalWh
		if delta < 0 {
			return next, DeltaResult{Quality: QualityCounterReset, CounterReset: true}, nil
		}
		if maximumPowerW > 0 {
			maximumExpected := maximumPowerW * elapsed.Hours() * 1.5
			if delta > maximumExpected+0.001 {
				return previous, DeltaResult{}, errors.New("cumulative energy jump exceeds configured maximum power")
			}
		}
		return next, DeltaResult{EnergyWh: delta, Quality: QualityMeasured}, nil
	}

	if powerW == nil || previous.PowerW == nil || pollInterval <= 0 || elapsed > 2*pollInterval {
		return next, DeltaResult{Quality: QualityPartial}, nil
	}
	energyWh := ((*previous.PowerW + *powerW) / 2) * elapsed.Hours()
	return next, DeltaResult{EnergyWh: energyWh, Quality: QualityCalculated}, nil
}

func validateReadingNumber(name string, value *float64, maximum float64) error {
	if value == nil {
		return nil
	}
	if math.IsNaN(*value) || math.IsInf(*value, 0) {
		return errors.New(name + " must be finite")
	}
	if *value < 0 {
		return errors.New(name + " must not be negative")
	}
	if maximum > 0 && *value > maximum*1.5 {
		return errors.New(name + " exceeds configured maximum")
	}
	return nil
}

func copyFloat(value *float64) *float64 {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}
