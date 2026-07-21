// Package billingcycle calculates electricity billing periods in local civil time.
// Periods are start-inclusive and end-exclusive. When a configured start day is
// absent from a month, that month's boundary is its final calendar day.
package billingcycle

import (
	"errors"
	"fmt"
	"time"
)

const maxGeneratedCycles = 2400

// Period is one complete billing cycle. Days is a count of local calendar days,
// not elapsed 24-hour durations, so it remains correct across DST transitions.
type Period struct {
	Start time.Time
	End   time.Time
	Days  int
}

// Boundary returns the local midnight boundary for a year and month.
func Boundary(year int, month time.Month, startDay int, location *time.Location) (time.Time, error) {
	if err := validate(startDay, location); err != nil {
		return time.Time{}, err
	}

	lastDay := time.Date(year, month+1, 0, 0, 0, 0, 0, location).Day()
	day := startDay
	if day > lastDay {
		day = lastDay
	}
	return time.Date(year, month, day, 0, 0, 0, 0, location), nil
}

// CycleContaining returns the complete billing cycle containing instant.
func CycleContaining(instant time.Time, startDay int, location *time.Location) (Period, error) {
	if err := validate(startDay, location); err != nil {
		return Period{}, err
	}
	if instant.IsZero() {
		return Period{}, errors.New("billing cycle instant must not be zero")
	}

	local := instant.In(location)
	start, err := Boundary(local.Year(), local.Month(), startDay, location)
	if err != nil {
		return Period{}, err
	}
	if local.Before(start) {
		previousMonth := time.Date(local.Year(), local.Month()-1, 1, 0, 0, 0, 0, location)
		start, err = Boundary(previousMonth.Year(), previousMonth.Month(), startDay, location)
		if err != nil {
			return Period{}, err
		}
	}

	nextMonth := time.Date(start.Year(), start.Month()+1, 1, 0, 0, 0, 0, location)
	end, err := Boundary(nextMonth.Year(), nextMonth.Month(), startDay, location)
	if err != nil {
		return Period{}, err
	}
	days, err := CivilDaysBetween(start, end, location)
	if err != nil {
		return Period{}, err
	}
	return Period{Start: start, End: end, Days: days}, nil
}

// CyclesOverlapping returns complete cycles that intersect [rangeStart, rangeEnd).
func CyclesOverlapping(rangeStart, rangeEnd time.Time, startDay int, location *time.Location) ([]Period, error) {
	if err := validate(startDay, location); err != nil {
		return nil, err
	}
	if rangeStart.IsZero() || rangeEnd.IsZero() {
		return nil, errors.New("billing cycle range bounds must not be zero")
	}
	if !rangeStart.Before(rangeEnd) {
		return nil, errors.New("billing cycle range end must be after start")
	}

	period, err := CycleContaining(rangeStart, startDay, location)
	if err != nil {
		return nil, err
	}
	periods := make([]Period, 0, 2)
	for period.Start.Before(rangeEnd) {
		periods = append(periods, period)
		if len(periods) >= maxGeneratedCycles {
			return nil, fmt.Errorf("billing cycle range exceeds %d cycles", maxGeneratedCycles)
		}
		period, err = CycleContaining(period.End, startDay, location)
		if err != nil {
			return nil, err
		}
	}
	return periods, nil
}

// CivilDaysBetween counts local date changes between two local-midnight bounds.
func CivilDaysBetween(start, end time.Time, location *time.Location) (int, error) {
	if location == nil {
		return 0, errors.New("billing cycle location is required")
	}
	if !start.Before(end) {
		return 0, errors.New("civil-day end must be after start")
	}

	startLocal := start.In(location)
	endLocal := end.In(location)
	startDate := time.Date(startLocal.Year(), startLocal.Month(), startLocal.Day(), 0, 0, 0, 0, time.UTC)
	endDate := time.Date(endLocal.Year(), endLocal.Month(), endLocal.Day(), 0, 0, 0, 0, time.UTC)
	days := int(endDate.Sub(startDate) / (24 * time.Hour))
	if days <= 0 {
		return 0, errors.New("civil-day bounds must span at least one local date")
	}
	return days, nil
}

func validate(startDay int, location *time.Location) error {
	if startDay < 1 || startDay > 31 {
		return errors.New("billing cycle start day must be between 1 and 31")
	}
	if location == nil {
		return errors.New("billing cycle location is required")
	}
	return nil
}
