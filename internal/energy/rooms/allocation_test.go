package rooms

import (
	"errors"
	"reflect"
	"testing"

	"github.com/filipegalo/shelly-energy-dashboard/internal/energy/pricing"
	"github.com/shopspring/decimal"
)

func TestAllocateWholeHouseVariableCostUsesAlreadyCalculatedVATOnce(t *testing.T) {
	t.Parallel()
	rules := pricing.DefaultConfig()
	rules.EnergyPricePerKWhExcludingVAT = roomDecimal("1")
	rules.ReducedVATRate = decimal.Zero
	rules.StandardVATRate = roomDecimal("1")
	rules.StandardReducedVATLimitKWhPer30Days = roomDecimal("200")
	rules.IncludeExciseTax = false
	wholeHouse, err := pricing.Calculate(rules, roomDecimal("300"), 30)
	if err != nil {
		t.Fatal(err)
	}
	assertRoomDecimal(t, "whole-house reduced energy", wholeHouse.ReducedRateKWh, "200")
	assertRoomDecimal(t, "whole-house standard energy", wholeHouse.StandardRateKWh, "100")
	assertRoomDecimal(t, "whole-house cost", wholeHouse.TotalIncludingVAT, "400")

	main := roomDecimal("300")
	allocation, err := AllocateVariableCost(VariableCostAllocationInput{
		WholeHouseCost: wholeHouse.TotalIncludingVAT,
		MainEnergyKWh:  &main,
		Rooms: []RoomEnergy{
			{RoomID: 2, EnergyKWh: roomDecimal("100")},
			{RoomID: 1, EnergyKWh: roomDecimal("150")},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(allocation.Rooms) != 2 || allocation.Rooms[0].RoomID != 1 || allocation.Rooms[1].RoomID != 2 {
		t.Fatalf("room output is not deterministic: %+v", allocation.Rooms)
	}
	assertRoomDecimal(t, "room A", allocation.Rooms[0].AllocatedCost, "200")
	assertRoomDecimal(t, "room B", allocation.Rooms[1].AllocatedCost, "133.33")
	assertRoomDecimal(t, "unmetered", allocation.UnmeteredAllocatedCost, "66.67")
	assertRoomDecimal(t, "exact whole-house cost", allocation.ExactWholeHouseCost, "400")
	assertRoomDecimal(t, "exact room A", allocation.Rooms[0].ExactAllocatedCost, "200")
	assertRoomDecimal(t, "exact allocation sum", exactAllocatedCostSum(allocation), "400")
	assertRoomDecimal(t, "unmetered energy", allocation.UnmeteredEnergyKWh, "50")
	assertRoomDecimal(t, "allocated sum", allocatedCostSum(allocation), "400")
	if allocation.Normalized {
		t.Fatal("normal allocation was marked normalized")
	}

	separateRoomA, _ := pricing.Calculate(rules, roomDecimal("150"), 30)
	separateRoomB, _ := pricing.Calculate(rules, roomDecimal("100"), 30)
	separateUnmetered, _ := pricing.Calculate(rules, roomDecimal("50"), 30)
	incorrectSeparateVATTotal := separateRoomA.TotalIncludingVAT.Add(separateRoomB.TotalIncludingVAT).Add(separateUnmetered.TotalIncludingVAT)
	assertRoomDecimal(t, "incorrect per-bucket VAT comparison", incorrectSeparateVATTotal, "300")
	if allocatedCostSum(allocation).Equal(incorrectSeparateVATTotal) {
		t.Fatal("allocation behaved as if VAT allowance were reapplied per room")
	}
}

func TestAllocateVariableCostNormalAndOverMeasuredCases(t *testing.T) {
	t.Parallel()
	t.Run("normal includes unmetered", func(t *testing.T) {
		main := roomDecimal("20")
		result, err := AllocateVariableCost(VariableCostAllocationInput{
			WholeHouseCost: roomDecimal("20"),
			MainEnergyKWh:  &main,
			Rooms: []RoomEnergy{
				{RoomID: 3, EnergyKWh: roomDecimal("3")},
				{RoomID: 1, EnergyKWh: roomDecimal("8")},
				{RoomID: 2, EnergyKWh: roomDecimal("4")},
			},
		})
		if err != nil {
			t.Fatal(err)
		}
		assertRoomDecimal(t, "room 1", result.Rooms[0].AllocatedCost, "8")
		assertRoomDecimal(t, "room 2", result.Rooms[1].AllocatedCost, "4")
		assertRoomDecimal(t, "room 3", result.Rooms[2].AllocatedCost, "3")
		assertRoomDecimal(t, "unmetered", result.UnmeteredAllocatedCost, "5")
		assertRoomDecimal(t, "unmetered share", result.UnmeteredShare, "0.25")
		assertRoomDecimal(t, "sum", allocatedCostSum(result), "20")
	})

	t.Run("over measured normalizes rooms", func(t *testing.T) {
		main := roomDecimal("20")
		result, err := AllocateVariableCost(VariableCostAllocationInput{
			WholeHouseCost: roomDecimal("10.01"),
			MainEnergyKWh:  &main,
			Rooms: []RoomEnergy{
				{RoomID: 2, EnergyKWh: roomDecimal("10")},
				{RoomID: 1, EnergyKWh: roomDecimal("12")},
			},
		})
		if err != nil {
			t.Fatal(err)
		}
		if !result.Normalized {
			t.Fatal("over-measured allocation was not marked normalized")
		}
		assertRoomDecimal(t, "unmetered energy", result.UnmeteredEnergyKWh, "0")
		assertRoomDecimal(t, "unmetered cost", result.UnmeteredAllocatedCost, "0")
		assertRoomDecimal(t, "room 1", result.Rooms[0].AllocatedCost, "5.46")
		assertRoomDecimal(t, "room 2", result.Rooms[1].AllocatedCost, "4.55")
		assertRoomDecimal(t, "sum", allocatedCostSum(result), "10.01")
	})
}

func TestAllocateVariableCostLargestRemainderTieBreakIsDeterministic(t *testing.T) {
	t.Parallel()
	main := roomDecimal("3")
	input := VariableCostAllocationInput{
		WholeHouseCost: roomDecimal("0.01"),
		MainEnergyKWh:  &main,
		Rooms: []RoomEnergy{
			{RoomID: 3, EnergyKWh: roomDecimal("1")},
			{RoomID: 1, EnergyKWh: roomDecimal("1")},
			{RoomID: 2, EnergyKWh: roomDecimal("1")},
		},
	}
	first, err := AllocateVariableCost(input)
	if err != nil {
		t.Fatal(err)
	}
	input.Rooms = []RoomEnergy{input.Rooms[2], input.Rooms[0], input.Rooms[1]}
	second, err := AllocateVariableCost(input)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("input ordering changed allocation:\nfirst=%+v\nsecond=%+v", first, second)
	}
	assertRoomDecimal(t, "lowest room ID remainder", first.Rooms[0].AllocatedCost, "0.01")
	assertRoomDecimal(t, "room 2", first.Rooms[1].AllocatedCost, "0")
	assertRoomDecimal(t, "room 3", first.Rooms[2].AllocatedCost, "0")

	mainWithUnmetered := roomDecimal("3")
	withUnmetered, err := AllocateVariableCost(VariableCostAllocationInput{
		WholeHouseCost: roomDecimal("0.01"),
		MainEnergyKWh:  &mainWithUnmetered,
		Rooms:          []RoomEnergy{{RoomID: 2, EnergyKWh: roomDecimal("1")}, {RoomID: 1, EnergyKWh: roomDecimal("1")}},
	})
	if err != nil {
		t.Fatal(err)
	}
	assertRoomDecimal(t, "real room wins tie", withUnmetered.Rooms[0].AllocatedCost, "0.01")
	assertRoomDecimal(t, "unmetered is last on tie", withUnmetered.UnmeteredAllocatedCost, "0")
}

func TestAllocateVariableCostRoundsOnceAndAlwaysReconcilesFinalCent(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		cost string
		main string
		room []string
		want string
	}{
		{name: "half cent rounds up", cost: "10.005", main: "3", room: []string{"1", "1", "1"}, want: "10.01"},
		{name: "unequal thirds", cost: "99.99", main: "6", room: []string{"1", "2", "3"}, want: "99.99"},
		{name: "tiny total", cost: "0.02", main: "4", room: []string{"1", "1", "1"}, want: "0.02"},
		{name: "all unmetered", cost: "12.34", main: "5", want: "12.34"},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			main := roomDecimal(test.main)
			rooms := make([]RoomEnergy, 0, len(test.room))
			for index, value := range test.room {
				rooms = append(rooms, RoomEnergy{RoomID: int64(index + 1), EnergyKWh: roomDecimal(value)})
			}
			result, err := AllocateVariableCost(VariableCostAllocationInput{WholeHouseCost: roomDecimal(test.cost), MainEnergyKWh: &main, Rooms: rooms})
			if err != nil {
				t.Fatal(err)
			}
			assertRoomDecimal(t, "rounded whole-house cost", result.WholeHouseCost, test.want)
			assertRoomDecimal(t, "allocated sum", allocatedCostSum(result), test.want)
			assertRoomDecimal(t, "original exact whole-house cost", result.ExactWholeHouseCost, test.cost)
			assertRoomDecimal(t, "exact allocated sum", exactAllocatedCostSum(result), test.cost)
		})
	}
}

func TestAllocateVariableCostHandlesZeroEnergyAndRejectsInvalidInputs(t *testing.T) {
	t.Parallel()
	zero := decimal.Zero
	result, err := AllocateVariableCost(VariableCostAllocationInput{
		WholeHouseCost: decimal.Zero,
		MainEnergyKWh:  &zero,
		Rooms:          []RoomEnergy{{RoomID: 2}, {RoomID: 1}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Rooms) != 2 || result.Rooms[0].RoomID != 1 || result.Rooms[1].RoomID != 2 {
		t.Fatalf("zero-energy rooms not retained deterministically: %+v", result.Rooms)
	}
	assertRoomDecimal(t, "zero allocation", allocatedCostSum(result), "0")

	main := roomDecimal("10")
	tests := []struct {
		name  string
		input VariableCostAllocationInput
		is    error
	}{
		{name: "missing main", input: VariableCostAllocationInput{WholeHouseCost: roomDecimal("1")}, is: ErrMainEnergyMissing},
		{name: "negative main", input: VariableCostAllocationInput{MainEnergyKWh: roomDecimalPointer("-1")}},
		{name: "nonzero cost with zero energy", input: VariableCostAllocationInput{WholeHouseCost: roomDecimal("1"), MainEnergyKWh: &zero}},
		{name: "negative cost", input: VariableCostAllocationInput{WholeHouseCost: roomDecimal("-1"), MainEnergyKWh: &main}},
		{name: "invalid room ID", input: VariableCostAllocationInput{MainEnergyKWh: &main, Rooms: []RoomEnergy{{EnergyKWh: roomDecimal("1")}}}},
		{name: "negative room energy", input: VariableCostAllocationInput{MainEnergyKWh: &main, Rooms: []RoomEnergy{{RoomID: 1, EnergyKWh: roomDecimal("-1")}}}},
		{name: "duplicate room", input: VariableCostAllocationInput{MainEnergyKWh: &main, Rooms: []RoomEnergy{{RoomID: 1}, {RoomID: 1}}}},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			_, err := AllocateVariableCost(test.input)
			if err == nil {
				t.Fatal("AllocateVariableCost accepted invalid input")
			}
			if test.is != nil && !errors.Is(err, test.is) {
				t.Fatalf("error=%v, want errors.Is(_, %v)", err, test.is)
			}
		})
	}
}

func TestAllocateVariableCostDoesNotMutateInputs(t *testing.T) {
	t.Parallel()
	main := roomDecimal("20")
	rooms := []RoomEnergy{{RoomID: 2, EnergyKWh: roomDecimal("4")}, {RoomID: 1, EnergyKWh: roomDecimal("3")}}
	original := append([]RoomEnergy(nil), rooms...)
	if _, err := AllocateVariableCost(VariableCostAllocationInput{WholeHouseCost: roomDecimal("10"), MainEnergyKWh: &main, Rooms: rooms}); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(rooms, original) {
		t.Fatalf("input rooms mutated: got=%v want=%v", rooms, original)
	}
}

func TestRoundFinalCostsUsesAccumulatedExactCostsAndStableTieBreak(t *testing.T) {
	t.Parallel()
	input := FinalCostRoundingInput{
		WholeHouseCost: roomDecimal("10.005"),
		Rooms: []ExactRoomCost{
			{RoomID: 2, Cost: roomDecimal("3.335")},
			{RoomID: 1, Cost: roomDecimal("3.335")},
		},
		UnmeteredCost: roomDecimal("3.335"),
	}
	originalRooms := append([]ExactRoomCost(nil), input.Rooms...)
	result, err := RoundFinalCosts(input)
	if err != nil {
		t.Fatal(err)
	}
	assertRoomDecimal(t, "rounded whole-house cost", result.WholeHouseCost, "10.01")
	if len(result.Rooms) != 2 || result.Rooms[0].RoomID != 1 || result.Rooms[1].RoomID != 2 {
		t.Fatalf("rounded rooms are not sorted: %+v", result.Rooms)
	}
	assertRoomDecimal(t, "room 1 tied remainder", result.Rooms[0].Cost, "3.34")
	assertRoomDecimal(t, "room 2 tied remainder", result.Rooms[1].Cost, "3.34")
	assertRoomDecimal(t, "unmetered comes last", result.UnmeteredCost, "3.33")
	if !reflect.DeepEqual(input.Rooms, originalRooms) {
		t.Fatalf("RoundFinalCosts mutated input: got=%v want=%v", input.Rooms, originalRooms)
	}
}

func TestRoundFinalCostsRejectsInconsistentOrInvalidExactBuckets(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		input FinalCostRoundingInput
	}{
		{name: "sum mismatch", input: FinalCostRoundingInput{WholeHouseCost: roomDecimal("2"), Rooms: []ExactRoomCost{{RoomID: 1, Cost: roomDecimal("1")}}}},
		{name: "negative whole", input: FinalCostRoundingInput{WholeHouseCost: roomDecimal("-1"), UnmeteredCost: roomDecimal("-1")}},
		{name: "negative room", input: FinalCostRoundingInput{WholeHouseCost: roomDecimal("1"), Rooms: []ExactRoomCost{{RoomID: 1, Cost: roomDecimal("-1")}}, UnmeteredCost: roomDecimal("2")}},
		{name: "negative unmetered", input: FinalCostRoundingInput{WholeHouseCost: roomDecimal("1"), Rooms: []ExactRoomCost{{RoomID: 1, Cost: roomDecimal("2")}}, UnmeteredCost: roomDecimal("-1")}},
		{name: "invalid room ID", input: FinalCostRoundingInput{WholeHouseCost: roomDecimal("1"), Rooms: []ExactRoomCost{{Cost: roomDecimal("1")}}}},
		{name: "duplicate room", input: FinalCostRoundingInput{WholeHouseCost: roomDecimal("1"), Rooms: []ExactRoomCost{{RoomID: 1, Cost: roomDecimal("0.5")}, {RoomID: 1, Cost: roomDecimal("0.5")}}}},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if _, err := RoundFinalCosts(test.input); err == nil {
				t.Fatal("RoundFinalCosts accepted invalid exact buckets")
			}
		})
	}
}

func allocatedCostSum(result VariableCostAllocationResult) decimal.Decimal {
	total := result.UnmeteredAllocatedCost
	for _, room := range result.Rooms {
		total = total.Add(room.AllocatedCost)
	}
	return total
}

func exactAllocatedCostSum(result VariableCostAllocationResult) decimal.Decimal {
	total := result.ExactUnmeteredAllocatedCost
	for _, room := range result.Rooms {
		total = total.Add(room.ExactAllocatedCost)
	}
	return total
}
