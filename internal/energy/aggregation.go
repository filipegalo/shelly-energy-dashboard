package energy

import (
	"errors"
	"math"
	"time"
)

type Aggregate struct {
	Start              time.Time
	End                time.Time
	EnergyConsumedWh   float64
	AveragePowerW      float64
	MinimumPowerW      float64
	MaximumPowerW      float64
	SampleCount        int
	MissingSampleCount int
	Quality            string
}

type Accumulator struct {
	start        time.Time
	end          time.Time
	energyWh     float64
	powerSum     float64
	minimumPower float64
	maximumPower float64
	samples      int
	missing      int
	quality      string
}

func NewAccumulator(start, end time.Time) (Accumulator, error) {
	if start.IsZero() || !start.Before(end) {
		return Accumulator{}, errors.New("aggregate interval must have valid increasing bounds")
	}
	return Accumulator{start: start, end: end, minimumPower: math.Inf(1), maximumPower: math.Inf(-1), quality: QualityMeasured}, nil
}

func (a Accumulator) Add(recordedAt time.Time, powerW *float64, energyWh float64, quality string) (Accumulator, error) {
	if recordedAt.Before(a.start) || !recordedAt.Before(a.end) {
		return a, errors.New("reading is outside aggregate interval")
	}
	if energyWh < 0 || math.IsNaN(energyWh) || math.IsInf(energyWh, 0) {
		return a, errors.New("aggregate energy must be finite and non-negative")
	}
	next := a
	next.energyWh += energyWh
	if powerW == nil || math.IsNaN(*powerW) || math.IsInf(*powerW, 0) || *powerW < 0 {
		next.missing++
		next.quality = QualityPartial
		return next, nil
	}
	next.samples++
	next.powerSum += *powerW
	next.minimumPower = math.Min(next.minimumPower, *powerW)
	next.maximumPower = math.Max(next.maximumPower, *powerW)
	if quality != "" && quality != QualityMeasured {
		next.quality = quality
	}
	return next, nil
}

func (a Accumulator) Missing(count int) Accumulator {
	if count <= 0 {
		return a
	}
	next := a
	next.missing += count
	next.quality = QualityPartial
	return next
}

func (a Accumulator) Result() Aggregate {
	result := Aggregate{Start: a.start, End: a.end, EnergyConsumedWh: a.energyWh, SampleCount: a.samples, MissingSampleCount: a.missing, Quality: a.quality}
	if a.samples > 0 {
		result.AveragePowerW = a.powerSum / float64(a.samples)
		result.MinimumPowerW = a.minimumPower
		result.MaximumPowerW = a.maximumPower
	} else {
		result.Quality = QualityPartial
	}
	return result
}

func IntervalBounds(recordedAt time.Time, interval string, location *time.Location) (time.Time, time.Time, error) {
	if recordedAt.IsZero() || location == nil {
		return time.Time{}, time.Time{}, errors.New("recording time and location are required")
	}
	local := recordedAt.In(location)
	switch interval {
	case "minute":
		start := local.Truncate(time.Minute)
		return start, start.Add(time.Minute), nil
	case "hour":
		start := local.Truncate(time.Hour)
		return start, start.Add(time.Hour), nil
	case "day":
		start := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, location)
		return start, start.AddDate(0, 0, 1), nil
	default:
		return time.Time{}, time.Time{}, errors.New("unsupported aggregate interval")
	}
}
