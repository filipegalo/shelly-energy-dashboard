package billingcycle

import (
	"testing"
	"time"
)

func TestCycleContainingUsesConfiguredStartDay(t *testing.T) {
	t.Parallel()
	location := time.FixedZone("WEST", 60*60)

	period, err := CycleContaining(time.Date(2026, time.July, 21, 16, 0, 0, 0, location), 11, location)
	if err != nil {
		t.Fatalf("CycleContaining() error = %v", err)
	}

	wantStart := time.Date(2026, time.July, 11, 0, 0, 0, 0, location)
	wantEnd := time.Date(2026, time.August, 11, 0, 0, 0, 0, location)
	if !period.Start.Equal(wantStart) || !period.End.Equal(wantEnd) {
		t.Fatalf("CycleContaining() = [%v, %v), want [%v, %v)", period.Start, period.End, wantStart, wantEnd)
	}
	if period.Days != 31 {
		t.Fatalf("Days = %d, want 31", period.Days)
	}
}

func TestBoundaryClampsStartDayToLastDayOfShortMonth(t *testing.T) {
	t.Parallel()
	location := time.UTC

	tests := []struct {
		month time.Month
		day   int
	}{
		{month: time.February, day: 28},
		{month: time.April, day: 30},
		{month: time.May, day: 31},
	}
	for _, test := range tests {
		got, err := Boundary(2026, test.month, 31, location)
		if err != nil {
			t.Fatalf("Boundary(%s) error = %v", test.month, err)
		}
		if got.Day() != test.day || got.Hour() != 0 {
			t.Errorf("Boundary(%s) = %v, want day %d at midnight", test.month, got, test.day)
		}
	}
}

func TestCycleContainingTreatsBoundaryAsStartOfNewCycle(t *testing.T) {
	t.Parallel()
	location := time.UTC
	atBoundary := time.Date(2026, time.February, 28, 0, 0, 0, 0, location)

	period, err := CycleContaining(atBoundary, 31, location)
	if err != nil {
		t.Fatalf("CycleContaining() error = %v", err)
	}
	if !period.Start.Equal(atBoundary) {
		t.Fatalf("Start = %v, want %v", period.Start, atBoundary)
	}
	wantEnd := time.Date(2026, time.March, 31, 0, 0, 0, 0, location)
	if !period.End.Equal(wantEnd) {
		t.Fatalf("End = %v, want %v", period.End, wantEnd)
	}
}

func TestCycleContainingBeforeMonthlyBoundaryUsesPreviousMonth(t *testing.T) {
	t.Parallel()
	location := time.UTC

	period, err := CycleContaining(time.Date(2026, time.July, 5, 12, 0, 0, 0, location), 11, location)
	if err != nil {
		t.Fatalf("CycleContaining() error = %v", err)
	}
	wantStart := time.Date(2026, time.June, 11, 0, 0, 0, 0, location)
	if !period.Start.Equal(wantStart) {
		t.Fatalf("Start = %v, want %v", period.Start, wantStart)
	}
}

func TestCycleDaysUseCivilDatesAcrossDST(t *testing.T) {
	t.Parallel()
	location, err := time.LoadLocation("Europe/Lisbon")
	if err != nil {
		t.Fatalf("LoadLocation() error = %v", err)
	}

	period, err := CycleContaining(time.Date(2026, time.April, 1, 12, 0, 0, 0, location), 11, location)
	if err != nil {
		t.Fatalf("CycleContaining() error = %v", err)
	}
	if period.Days != 31 {
		t.Fatalf("Days = %d, want 31", period.Days)
	}
	if got := period.End.Sub(period.Start); got == 31*24*time.Hour {
		t.Fatalf("test cycle did not cross a DST offset change: duration = %v", got)
	}
}

func TestCyclesOverlappingReturnsClippedMonthlyCycles(t *testing.T) {
	t.Parallel()
	location := time.UTC
	rangeStart := time.Date(2026, time.January, 20, 8, 0, 0, 0, location)
	rangeEnd := time.Date(2026, time.March, 12, 9, 0, 0, 0, location)

	periods, err := CyclesOverlapping(rangeStart, rangeEnd, 11, location)
	if err != nil {
		t.Fatalf("CyclesOverlapping() error = %v", err)
	}
	if len(periods) != 3 {
		t.Fatalf("len(periods) = %d, want 3", len(periods))
	}
	wantStarts := []time.Time{
		time.Date(2026, time.January, 11, 0, 0, 0, 0, location),
		time.Date(2026, time.February, 11, 0, 0, 0, 0, location),
		time.Date(2026, time.March, 11, 0, 0, 0, 0, location),
	}
	for index, want := range wantStarts {
		if !periods[index].Start.Equal(want) {
			t.Errorf("periods[%d].Start = %v, want %v", index, periods[index].Start, want)
		}
	}
}

func TestValidation(t *testing.T) {
	t.Parallel()
	now := time.Now()
	if _, err := CycleContaining(now, 0, time.UTC); err == nil {
		t.Error("CycleContaining() accepted start day 0")
	}
	if _, err := CycleContaining(now, 32, time.UTC); err == nil {
		t.Error("CycleContaining() accepted start day 32")
	}
	if _, err := CycleContaining(now, 11, nil); err == nil {
		t.Error("CycleContaining() accepted nil location")
	}
	if _, err := CyclesOverlapping(now, now, 11, time.UTC); err == nil {
		t.Error("CyclesOverlapping() accepted empty range")
	}
	if _, err := CycleContaining(time.Time{}, 11, time.UTC); err == nil {
		t.Error("CycleContaining() accepted zero instant")
	}
	if _, err := CyclesOverlapping(time.Time{}, now, 11, time.UTC); err == nil {
		t.Error("CyclesOverlapping() accepted zero range bound")
	}
	if _, err := CivilDaysBetween(now, now, time.UTC); err == nil {
		t.Error("CivilDaysBetween() accepted non-increasing bounds")
	}
	if _, err := CivilDaysBetween(now, now.AddDate(0, 0, 1), nil); err == nil {
		t.Error("CivilDaysBetween() accepted nil location")
	}
}
