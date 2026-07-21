package rooms

import (
	"reflect"
	"testing"

	"github.com/shopspring/decimal"
)

func TestReconcileNormalInterval(t *testing.T) {
	t.Parallel()
	main := roomDecimal("20")
	result, err := Reconcile(ReconciliationInput{
		MainEnergyKWh: &main,
		Rooms: []RoomEnergy{
			{RoomID: 30, EnergyKWh: roomDecimal("3")},
			{RoomID: 10, EnergyKWh: roomDecimal("8")},
			{RoomID: 20, EnergyKWh: roomDecimal("4")},
		},
		Tolerance: ReconciliationTolerance{AbsoluteKWh: roomDecimal("0.10"), Relative: roomDecimal("0.05")},
	})
	if err != nil {
		t.Fatal(err)
	}
	assertRoomDecimal(t, "room energy", result.RoomEnergyKWh, "15")
	assertRoomDecimal(t, "unmetered energy", result.UnmeteredEnergyKWh, "5")
	assertRoomDecimal(t, "difference", result.DifferenceKWh, "-5")
	assertRoomDecimal(t, "absolute difference", result.AbsoluteDifferenceKWh, "5")
	if result.CoverageRatio == nil {
		t.Fatal("coverage ratio is nil")
	}
	assertRoomDecimal(t, "coverage", *result.CoverageRatio, "0.75")
	if result.Status != ReconciliationHealthy || result.OverMeasured || result.WithinTolerance {
		t.Fatalf("unexpected reconciliation flags: %+v", result)
	}
	if result.MainEnergyKWh == nil || result.MainEnergyKWh == &main {
		t.Fatal("result must retain a value copy rather than aliasing the input pointer")
	}
}

func TestReconcileNeverProducesNegativeUnmeteredEnergy(t *testing.T) {
	t.Parallel()
	main := roomDecimal("20")
	result, err := Reconcile(ReconciliationInput{
		MainEnergyKWh: &main,
		Rooms:         []RoomEnergy{{RoomID: 1, EnergyKWh: roomDecimal("12")}, {RoomID: 2, EnergyKWh: roomDecimal("10")}},
		Tolerance:     ReconciliationTolerance{AbsoluteKWh: roomDecimal("0.10"), Relative: roomDecimal("0.05")},
	})
	if err != nil {
		t.Fatal(err)
	}
	assertRoomDecimal(t, "unmetered energy", result.UnmeteredEnergyKWh, "0")
	assertRoomDecimal(t, "difference", result.DifferenceKWh, "2")
	assertRoomDecimal(t, "coverage", *result.CoverageRatio, "1.1")
	if !result.OverMeasured || result.WithinTolerance || result.Status != ReconciliationWarning {
		t.Fatalf("unexpected over-measured result: %+v", result)
	}
}

func TestReconcileUsesInclusiveAbsoluteOrRelativeTolerance(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		main       string
		rooms      string
		absolute   string
		relative   string
		wantStatus ReconciliationStatus
		wantWithin bool
	}{
		{name: "inside absolute", main: "20", rooms: "20.08", absolute: "0.10", relative: "0", wantStatus: ReconciliationWithinTolerance, wantWithin: true},
		{name: "on absolute boundary", main: "20", rooms: "20.10", absolute: "0.10", relative: "0", wantStatus: ReconciliationWithinTolerance, wantWithin: true},
		{name: "inside relative", main: "20", rooms: "20.8", absolute: "0.1", relative: "0.05", wantStatus: ReconciliationWithinTolerance, wantWithin: true},
		{name: "on relative boundary", main: "20", rooms: "21", absolute: "0", relative: "0.05", wantStatus: ReconciliationWithinTolerance, wantWithin: true},
		{name: "outside both", main: "20", rooms: "21.2", absolute: "0.1", relative: "0.05", wantStatus: ReconciliationWarning},
		{name: "zero main uses absolute", main: "0", rooms: "0.1", absolute: "0.1", relative: "1", wantStatus: ReconciliationWithinTolerance, wantWithin: true},
		{name: "zero main outside absolute", main: "0", rooms: "0.2", absolute: "0.1", relative: "1", wantStatus: ReconciliationWarning},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			main := roomDecimal(test.main)
			result, err := Reconcile(ReconciliationInput{
				MainEnergyKWh: &main,
				Rooms:         []RoomEnergy{{RoomID: 1, EnergyKWh: roomDecimal(test.rooms)}},
				Tolerance:     ReconciliationTolerance{AbsoluteKWh: roomDecimal(test.absolute), Relative: roomDecimal(test.relative)},
			})
			if err != nil {
				t.Fatal(err)
			}
			if result.Status != test.wantStatus || result.WithinTolerance != test.wantWithin {
				t.Fatalf("status=%q within=%v, want %q/%v: %+v", result.Status, result.WithinTolerance, test.wantStatus, test.wantWithin, result)
			}
			if result.UnmeteredEnergyKWh.IsNegative() {
				t.Fatalf("negative unmetered energy: %s", result.UnmeteredEnergyKWh)
			}
		})
	}
}

func TestReconcileMissingMainPreservesMeasuredRoomsWithoutInventingCoverage(t *testing.T) {
	t.Parallel()
	result, err := Reconcile(ReconciliationInput{
		Rooms:     []RoomEnergy{{RoomID: 1, EnergyKWh: roomDecimal("5")}, {RoomID: 2, EnergyKWh: roomDecimal("2")}},
		Tolerance: ReconciliationTolerance{AbsoluteKWh: roomDecimal("0.1"), Relative: roomDecimal("0.05")},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != ReconciliationMissingMain || result.MainEnergyKWh != nil || result.CoverageRatio != nil || result.OverMeasured || result.WithinTolerance {
		t.Fatalf("missing main result = %+v", result)
	}
	assertRoomDecimal(t, "measured room energy", result.RoomEnergyKWh, "7")
	assertRoomDecimal(t, "unmetered energy", result.UnmeteredEnergyKWh, "0")
	assertRoomDecimal(t, "difference", result.DifferenceKWh, "0")
}

func TestReconcileZeroMainAndZeroRoomsIsHealthyWithUndefinedCoverage(t *testing.T) {
	t.Parallel()
	main := decimal.Zero
	result, err := Reconcile(ReconciliationInput{MainEnergyKWh: &main})
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != ReconciliationHealthy || result.CoverageRatio != nil || result.OverMeasured {
		t.Fatalf("zero result = %+v", result)
	}
}

func TestReconcileRejectsInvalidInputsAndDoesNotMutateRooms(t *testing.T) {
	t.Parallel()
	main := roomDecimal("20")
	rooms := []RoomEnergy{{RoomID: 2, EnergyKWh: roomDecimal("4")}, {RoomID: 1, EnergyKWh: roomDecimal("3")}}
	original := append([]RoomEnergy(nil), rooms...)
	if _, err := Reconcile(ReconciliationInput{MainEnergyKWh: &main, Rooms: rooms}); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(rooms, original) {
		t.Fatalf("input rooms mutated: got=%v want=%v", rooms, original)
	}

	tests := []struct {
		name  string
		input ReconciliationInput
	}{
		{name: "negative main", input: ReconciliationInput{MainEnergyKWh: roomDecimalPointer("-1")}},
		{name: "invalid room ID", input: ReconciliationInput{MainEnergyKWh: &main, Rooms: []RoomEnergy{{EnergyKWh: roomDecimal("1")}}}},
		{name: "negative room energy", input: ReconciliationInput{MainEnergyKWh: &main, Rooms: []RoomEnergy{{RoomID: 1, EnergyKWh: roomDecimal("-1")}}}},
		{name: "duplicate room", input: ReconciliationInput{MainEnergyKWh: &main, Rooms: []RoomEnergy{{RoomID: 1}, {RoomID: 1}}}},
		{name: "negative absolute tolerance", input: ReconciliationInput{MainEnergyKWh: &main, Tolerance: ReconciliationTolerance{AbsoluteKWh: roomDecimal("-0.1")}}},
		{name: "negative relative tolerance", input: ReconciliationInput{MainEnergyKWh: &main, Tolerance: ReconciliationTolerance{Relative: roomDecimal("-0.1")}}},
		{name: "relative tolerance above one", input: ReconciliationInput{MainEnergyKWh: &main, Tolerance: ReconciliationTolerance{Relative: roomDecimal("1.01")}}},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if _, err := Reconcile(test.input); err == nil {
				t.Fatal("Reconcile accepted invalid input")
			}
		})
	}
}

func roomDecimal(value string) decimal.Decimal {
	return decimal.RequireFromString(value)
}

func roomDecimalPointer(value string) *decimal.Decimal {
	parsed := roomDecimal(value)
	return &parsed
}

func assertRoomDecimal(t *testing.T, name string, got decimal.Decimal, want string) {
	t.Helper()
	expected := roomDecimal(want)
	if !got.Equal(expected) {
		t.Fatalf("%s=%s, want %s", name, got, expected)
	}
}
