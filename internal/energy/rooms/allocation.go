package rooms

import (
	"errors"
	"fmt"
	"math/big"
	"sort"

	"github.com/shopspring/decimal"
)

var ErrMainEnergyMissing = errors.New("main-meter energy is required for cost allocation")

// VariableCostAllocationInput accepts a whole-house variable cost that has
// already been calculated by the pricing/VAT engine. This package deliberately
// has no tariff or VAT inputs, so an allowance cannot accidentally be applied
// once per room.
type VariableCostAllocationInput struct {
	WholeHouseCost decimal.Decimal
	MainEnergyKWh  *decimal.Decimal
	Rooms          []RoomEnergy
}

type RoomCostAllocation struct {
	RoomID             int64
	EnergyKWh          decimal.Decimal
	Share              decimal.Decimal
	ExactAllocatedCost decimal.Decimal
	AllocatedCost      decimal.Decimal
}

// VariableCostAllocationResult contains currency-rounded allocations. Rooms
// are sorted by ID. Their allocated cost plus UnmeteredAllocatedCost is exactly
// WholeHouseCost after that total has been rounded once to cents.
type VariableCostAllocationResult struct {
	ExactWholeHouseCost         decimal.Decimal
	WholeHouseCost              decimal.Decimal
	Rooms                       []RoomCostAllocation
	UnmeteredEnergyKWh          decimal.Decimal
	UnmeteredShare              decimal.Decimal
	ExactUnmeteredAllocatedCost decimal.Decimal
	UnmeteredAllocatedCost      decimal.Decimal
	Normalized                  bool
}

// ExactRoomCost is an accumulated, unrounded room allocation. It is the input
// shape for the final report-level currency reconciliation pass.
type ExactRoomCost struct {
	RoomID int64
	Cost   decimal.Decimal
}

type FinalCostRoundingInput struct {
	WholeHouseCost decimal.Decimal
	Rooms          []ExactRoomCost
	UnmeteredCost  decimal.Decimal
}

type RoundedRoomCost struct {
	RoomID int64
	Cost   decimal.Decimal
}

type FinalCostRoundingResult struct {
	WholeHouseCost decimal.Decimal
	Rooms          []RoundedRoomCost
	UnmeteredCost  decimal.Decimal
}

type allocationBucket struct {
	roomIndex   int
	roomID      int64
	unmetered   bool
	energy      decimal.Decimal
	exactCost   decimal.Decimal
	baseCents   *big.Int
	remainder   *big.Int
	denominator *big.Int
}

// AllocateVariableCost distributes an already-calculated whole-house variable
// cost by measured energy. When room energy exceeds the main meter, room shares
// are normalized against their own sum and unmetered cost remains zero.
// Currency rounding uses the largest-remainder method; tied room remainders are
// resolved by ascending room ID, with Unmetered / Other last.
func AllocateVariableCost(input VariableCostAllocationInput) (VariableCostAllocationResult, error) {
	if input.MainEnergyKWh == nil {
		return VariableCostAllocationResult{}, ErrMainEnergyMissing
	}
	if input.WholeHouseCost.IsNegative() {
		return VariableCostAllocationResult{}, errors.New("whole-house variable cost must not be negative")
	}
	if input.MainEnergyKWh.IsNegative() {
		return VariableCostAllocationResult{}, errors.New("main-meter energy must not be negative")
	}
	roomTotal, rooms, err := validatedRoomEnergies(input.Rooms)
	if err != nil {
		return VariableCostAllocationResult{}, err
	}

	main := copyRoomDecimal(*input.MainEnergyKWh)
	unmetered := main.Sub(roomTotal)
	normalizationEnergy := main
	normalized := unmetered.IsNegative()
	if normalized {
		unmetered = decimal.Zero
		normalizationEnergy = roomTotal
	}
	roundedCost := input.WholeHouseCost.Round(2)
	result := VariableCostAllocationResult{
		ExactWholeHouseCost: copyRoomDecimal(input.WholeHouseCost),
		WholeHouseCost:      copyRoomDecimal(roundedCost),
		Rooms:               make([]RoomCostAllocation, len(rooms)),
		UnmeteredEnergyKWh:  copyRoomDecimal(unmetered),
		Normalized:          normalized,
	}
	for index, room := range rooms {
		result.Rooms[index] = RoomCostAllocation{RoomID: room.RoomID, EnergyKWh: copyRoomDecimal(room.EnergyKWh)}
	}

	if normalizationEnergy.IsZero() {
		if !input.WholeHouseCost.IsZero() {
			return VariableCostAllocationResult{}, errors.New("nonzero whole-house cost cannot be allocated across zero energy")
		}
		return result, nil
	}
	for index := range result.Rooms {
		result.Rooms[index].Share = result.Rooms[index].EnergyKWh.Div(normalizationEnergy)
	}
	result.UnmeteredShare = unmetered.Div(normalizationEnergy)

	totalCents, err := decimalCents(roundedCost)
	if err != nil {
		return VariableCostAllocationResult{}, err
	}
	buckets := make([]allocationBucket, 0, len(rooms)+1)
	for index, room := range rooms {
		buckets = append(buckets, allocationBucket{roomIndex: index, roomID: room.RoomID, energy: room.EnergyKWh})
	}
	buckets = append(buckets, allocationBucket{roomIndex: -1, unmetered: true, energy: unmetered})
	allocateExactCosts(input.WholeHouseCost, normalizationEnergy, buckets)
	if err := allocateCents(totalCents, normalizationEnergy, buckets); err != nil {
		return VariableCostAllocationResult{}, err
	}
	for _, bucket := range buckets {
		cost := centsToDecimal(bucket.baseCents)
		if bucket.unmetered {
			result.ExactUnmeteredAllocatedCost = copyRoomDecimal(bucket.exactCost)
			result.UnmeteredAllocatedCost = cost
			continue
		}
		result.Rooms[bucket.roomIndex].ExactAllocatedCost = copyRoomDecimal(bucket.exactCost)
		result.Rooms[bucket.roomIndex].AllocatedCost = cost
	}
	return result, nil
}

// RoundFinalCosts performs the final currency pass after exact interval
// allocations have been accumulated. The exact buckets must reconcile before
// rounding; inconsistent inputs are rejected rather than silently corrected.
func RoundFinalCosts(input FinalCostRoundingInput) (FinalCostRoundingResult, error) {
	if input.WholeHouseCost.IsNegative() {
		return FinalCostRoundingResult{}, errors.New("whole-house variable cost must not be negative")
	}
	if input.UnmeteredCost.IsNegative() {
		return FinalCostRoundingResult{}, errors.New("unmetered variable cost must not be negative")
	}
	seen := make(map[int64]struct{}, len(input.Rooms))
	rooms := make([]ExactRoomCost, 0, len(input.Rooms))
	exactSum := copyRoomDecimal(input.UnmeteredCost)
	for _, room := range input.Rooms {
		if room.RoomID < 1 {
			return FinalCostRoundingResult{}, errors.New("room ID must be positive")
		}
		if _, duplicate := seen[room.RoomID]; duplicate {
			return FinalCostRoundingResult{}, fmt.Errorf("room %d appears more than once", room.RoomID)
		}
		if room.Cost.IsNegative() {
			return FinalCostRoundingResult{}, fmt.Errorf("room %d cost must not be negative", room.RoomID)
		}
		seen[room.RoomID] = struct{}{}
		cost := copyRoomDecimal(room.Cost)
		rooms = append(rooms, ExactRoomCost{RoomID: room.RoomID, Cost: cost})
		exactSum = exactSum.Add(cost)
	}
	if !exactSum.Equal(input.WholeHouseCost) {
		return FinalCostRoundingResult{}, errors.New("exact room and unmetered costs do not equal the whole-house variable cost")
	}
	sort.Slice(rooms, func(left, right int) bool { return rooms[left].RoomID < rooms[right].RoomID })
	result := FinalCostRoundingResult{
		WholeHouseCost: input.WholeHouseCost.Round(2),
		Rooms:          make([]RoundedRoomCost, len(rooms)),
	}
	buckets := make([]allocationBucket, 0, len(rooms)+1)
	for index, room := range rooms {
		result.Rooms[index].RoomID = room.RoomID
		buckets = append(buckets, allocationBucket{roomIndex: index, roomID: room.RoomID, exactCost: room.Cost})
	}
	buckets = append(buckets, allocationBucket{roomIndex: -1, unmetered: true, exactCost: copyRoomDecimal(input.UnmeteredCost)})
	totalCents, err := decimalCents(result.WholeHouseCost)
	if err != nil {
		return FinalCostRoundingResult{}, err
	}
	if err := roundExactCostBuckets(totalCents, buckets); err != nil {
		return FinalCostRoundingResult{}, err
	}
	for _, bucket := range buckets {
		cost := centsToDecimal(bucket.baseCents)
		if bucket.unmetered {
			result.UnmeteredCost = cost
			continue
		}
		result.Rooms[bucket.roomIndex].Cost = cost
	}
	return result, nil
}

func allocateExactCosts(totalCost, totalEnergy decimal.Decimal, buckets []allocationBucket) {
	lastNonzero := -1
	for index := range buckets {
		if buckets[index].energy.IsPositive() {
			lastNonzero = index
		}
	}
	remaining := copyRoomDecimal(totalCost)
	for index := range buckets {
		switch {
		case buckets[index].energy.IsZero():
			buckets[index].exactCost = decimal.Zero
		case index == lastNonzero:
			buckets[index].exactCost = remaining
		default:
			candidate := totalCost.Mul(buckets[index].energy).Div(totalEnergy)
			// Decimal division of a repeating share is necessarily finite. Keep
			// any accumulated rounding residue from making the final bucket
			// microscopically negative; the final bucket receives the remainder.
			if candidate.GreaterThan(remaining) {
				candidate = remaining
			}
			buckets[index].exactCost = candidate
			remaining = remaining.Sub(buckets[index].exactCost)
		}
	}
}

func allocateCents(totalCents *big.Int, totalEnergy decimal.Decimal, buckets []allocationBucket) error {
	totalWeight, err := decimalRat(totalEnergy)
	if err != nil {
		return err
	}
	totalCentsRat := new(big.Rat).SetInt(totalCents)
	allocated := new(big.Int)
	for index := range buckets {
		weight, conversionErr := decimalRat(buckets[index].energy)
		if conversionErr != nil {
			return conversionErr
		}
		exact := new(big.Rat).Mul(totalCentsRat, weight)
		exact.Quo(exact, totalWeight)
		base, remainder := new(big.Int), new(big.Int)
		base.QuoRem(exact.Num(), exact.Denom(), remainder)
		buckets[index].baseCents = base
		buckets[index].remainder = new(big.Int).Set(remainder)
		buckets[index].denominator = new(big.Int).Set(exact.Denom())
		allocated.Add(allocated, base)
	}

	return distributeRoundingRemainder(totalCents, allocated, buckets)
}

func roundExactCostBuckets(totalCents *big.Int, buckets []allocationBucket) error {
	allocated := new(big.Int)
	hundred := big.NewRat(100, 1)
	for index := range buckets {
		cost, err := decimalRat(buckets[index].exactCost)
		if err != nil {
			return err
		}
		exactCents := new(big.Rat).Mul(cost, hundred)
		base, remainder := new(big.Int), new(big.Int)
		base.QuoRem(exactCents.Num(), exactCents.Denom(), remainder)
		buckets[index].baseCents = base
		buckets[index].remainder = new(big.Int).Set(remainder)
		buckets[index].denominator = new(big.Int).Set(exactCents.Denom())
		allocated.Add(allocated, base)
	}
	return distributeRoundingRemainder(totalCents, allocated, buckets)
}

func distributeRoundingRemainder(totalCents, allocated *big.Int, buckets []allocationBucket) error {
	remaining := new(big.Int).Sub(new(big.Int).Set(totalCents), allocated)
	if remaining.Sign() < 0 || !remaining.IsInt64() || remaining.Int64() > int64(len(buckets)) {
		return errors.New("cost allocation produced an invalid rounding remainder")
	}
	ranking := make([]int, len(buckets))
	for index := range ranking {
		ranking[index] = index
	}
	sort.SliceStable(ranking, func(left, right int) bool {
		leftBucket := buckets[ranking[left]]
		rightBucket := buckets[ranking[right]]
		leftScaled := new(big.Int).Mul(leftBucket.remainder, rightBucket.denominator)
		rightScaled := new(big.Int).Mul(rightBucket.remainder, leftBucket.denominator)
		if compared := leftScaled.Cmp(rightScaled); compared != 0 {
			return compared > 0
		}
		if leftBucket.unmetered != rightBucket.unmetered {
			return !leftBucket.unmetered
		}
		return leftBucket.roomID < rightBucket.roomID
	})
	for index := int64(0); index < remaining.Int64(); index++ {
		bucket := &buckets[ranking[index]]
		bucket.baseCents.Add(bucket.baseCents, big.NewInt(1))
	}
	return nil
}

func decimalCents(value decimal.Decimal) (*big.Int, error) {
	integer := value.Mul(decimal.NewFromInt(100)).StringFixed(0)
	result, ok := new(big.Int).SetString(integer, 10)
	if !ok {
		return nil, fmt.Errorf("convert rounded currency amount %q to cents", integer)
	}
	return result, nil
}

func decimalRat(value decimal.Decimal) (*big.Rat, error) {
	result, ok := new(big.Rat).SetString(value.String())
	if !ok {
		return nil, fmt.Errorf("convert decimal %q to an exact ratio", value.String())
	}
	return result, nil
}

func centsToDecimal(value *big.Int) decimal.Decimal {
	parsed, err := decimal.NewFromString(value.String())
	if err != nil {
		panic("convert integer cents: " + err.Error())
	}
	return parsed.Div(decimal.NewFromInt(100))
}
