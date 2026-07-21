// Package rooms contains pure accounting primitives used by the per-room
// energy reports. It does not read meters, persist assignments, or price
// electricity. Callers must pass room totals that already exclude submeters
// and excluded sources.
package rooms

import (
	"errors"
	"fmt"
	"sort"

	"github.com/shopspring/decimal"
)

// RoomEnergy is the already de-duplicated energy attributed to one room for an
// interval. EnergyKWh must contain only root primary sources; adding submeters
// here would intentionally double-count them.
type RoomEnergy struct {
	RoomID    int64
	EnergyKWh decimal.Decimal
}

// ReconciliationTolerance defines the accepted positive difference between
// room meters and the whole-house meter. A mismatch is accepted when it is at
// or below either limit.
type ReconciliationTolerance struct {
	AbsoluteKWh decimal.Decimal
	Relative    decimal.Decimal
}

// ReconciliationInput contains one aligned interval of whole-house and room
// energy. A nil MainEnergyKWh represents missing main-meter data, not zero.
type ReconciliationInput struct {
	MainEnergyKWh *decimal.Decimal
	Rooms         []RoomEnergy
	Tolerance     ReconciliationTolerance
}

type ReconciliationStatus string

const (
	ReconciliationMissingMain     ReconciliationStatus = "missing_main"
	ReconciliationHealthy         ReconciliationStatus = "healthy"
	ReconciliationWithinTolerance ReconciliationStatus = "within_tolerance"
	ReconciliationWarning         ReconciliationStatus = "warning"
)

// ReconciliationResult preserves measured values. In particular, it never
// subtracts a mismatch from room energy and never returns negative unmetered
// energy. DifferenceKWh is signed room energy minus main-meter energy.
type ReconciliationResult struct {
	MainEnergyKWh         *decimal.Decimal
	RoomEnergyKWh         decimal.Decimal
	UnmeteredEnergyKWh    decimal.Decimal
	DifferenceKWh         decimal.Decimal
	AbsoluteDifferenceKWh decimal.Decimal
	CoverageRatio         *decimal.Decimal
	OverMeasured          bool
	WithinTolerance       bool
	Status                ReconciliationStatus
}

// Reconcile compares de-duplicated room energy with the whole-house source of
// truth. A missing main meter produces a useful result with no invented
// coverage or difference.
func Reconcile(input ReconciliationInput) (ReconciliationResult, error) {
	roomTotal, _, err := validatedRoomEnergies(input.Rooms)
	if err != nil {
		return ReconciliationResult{}, err
	}
	if input.Tolerance.AbsoluteKWh.IsNegative() {
		return ReconciliationResult{}, errors.New("absolute reconciliation tolerance must not be negative")
	}
	if input.Tolerance.Relative.IsNegative() || input.Tolerance.Relative.GreaterThan(decimal.NewFromInt(1)) {
		return ReconciliationResult{}, errors.New("relative reconciliation tolerance must be between zero and one")
	}

	result := ReconciliationResult{
		RoomEnergyKWh:      copyRoomDecimal(roomTotal),
		UnmeteredEnergyKWh: decimal.Zero,
		Status:             ReconciliationMissingMain,
	}
	if input.MainEnergyKWh == nil {
		return result, nil
	}
	if input.MainEnergyKWh.IsNegative() {
		return ReconciliationResult{}, errors.New("main-meter energy must not be negative")
	}

	main := copyRoomDecimal(*input.MainEnergyKWh)
	result.MainEnergyKWh = decimalPointerCopy(main)
	result.DifferenceKWh = roomTotal.Sub(main)
	result.AbsoluteDifferenceKWh = result.DifferenceKWh.Abs()
	if !main.IsZero() {
		coverage := roomTotal.Div(main)
		result.CoverageRatio = decimalPointerCopy(coverage)
	}

	if !result.DifferenceKWh.IsPositive() {
		result.UnmeteredEnergyKWh = main.Sub(roomTotal)
		result.Status = ReconciliationHealthy
		return result, nil
	}

	result.OverMeasured = true
	insideAbsolute := result.DifferenceKWh.LessThanOrEqual(input.Tolerance.AbsoluteKWh)
	insideRelative := false
	if !main.IsZero() {
		insideRelative = result.DifferenceKWh.Div(main).LessThanOrEqual(input.Tolerance.Relative)
	}
	if insideAbsolute || insideRelative {
		result.WithinTolerance = true
		result.Status = ReconciliationWithinTolerance
		return result, nil
	}
	result.Status = ReconciliationWarning
	return result, nil
}

func validatedRoomEnergies(input []RoomEnergy) (decimal.Decimal, []RoomEnergy, error) {
	seen := make(map[int64]struct{}, len(input))
	rooms := make([]RoomEnergy, 0, len(input))
	total := decimal.Zero
	for _, room := range input {
		if room.RoomID < 1 {
			return decimal.Zero, nil, errors.New("room ID must be positive")
		}
		if _, duplicate := seen[room.RoomID]; duplicate {
			return decimal.Zero, nil, fmt.Errorf("room %d appears more than once", room.RoomID)
		}
		if room.EnergyKWh.IsNegative() {
			return decimal.Zero, nil, fmt.Errorf("room %d energy must not be negative", room.RoomID)
		}
		seen[room.RoomID] = struct{}{}
		energy := copyRoomDecimal(room.EnergyKWh)
		rooms = append(rooms, RoomEnergy{RoomID: room.RoomID, EnergyKWh: energy})
		total = total.Add(energy)
	}
	sort.Slice(rooms, func(left, right int) bool { return rooms[left].RoomID < rooms[right].RoomID })
	return total, rooms, nil
}

func copyRoomDecimal(value decimal.Decimal) decimal.Decimal {
	copy, err := decimal.NewFromString(value.String())
	if err != nil {
		panic("copy valid decimal: " + err.Error())
	}
	return copy
}

func decimalPointerCopy(value decimal.Decimal) *decimal.Decimal {
	copy := copyRoomDecimal(value)
	return &copy
}
