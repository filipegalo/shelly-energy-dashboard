package energy

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/filipegalo/shelly-energy-dashboard/internal/database"
	roomcore "github.com/filipegalo/shelly-energy-dashboard/internal/energy/rooms"
	"github.com/shopspring/decimal"
)

var ErrRoomEnergyAllocation = errors.New("room energy cost allocation failed")

type RoomEnergyPoint struct {
	Start                time.Time
	End                  time.Time
	EnergyKWh            *decimal.Decimal
	AllocatedCost        *decimal.Decimal
	AveragePowerW        *decimal.Decimal
	PeakPowerW           *decimal.Decimal
	HouseShare           *decimal.Decimal
	CoverageRatio        *decimal.Decimal
	Quality              roomcore.Quality
	ExpectedSources      int
	ReportingSources     int
	Sources              []roomcore.SourceContribution
	SubmeterEnergyKWh    decimal.Decimal
	OtherOnPrimaryKWh    decimal.Decimal
	SubmeterOverlap      bool
	AllocationNormalized bool
}

type RoomEnergySeries struct {
	RoomID        int64
	Name          string
	Icon          string
	Archived      bool
	Virtual       bool
	Points        []RoomEnergyPoint
	EnergyKWh     *decimal.Decimal
	AllocatedCost decimal.Decimal
	Quality       roomcore.Quality
}

type RoomReconciliationPoint struct {
	Start time.Time
	End   time.Time
	roomcore.ReconciliationResult
	Quality roomcore.Quality
}

type RoomEnergyReport struct {
	Start              time.Time
	End                time.Time
	Interval           string
	WholeHouse         Report
	Rooms              []RoomEnergySeries
	Unmetered          RoomEnergySeries
	Reconciliation     []RoomReconciliationPoint
	TotalAllocatedCost decimal.Decimal
	Tolerance          database.RoomEnergyConfig
}

// BuildRoomEnergyReport attributes existing source aggregates through
// effective-dated room assignments, then allocates the already-calculated
// whole-house variable cost. It never invokes pricing or VAT per room.
func BuildRoomEnergyReport(ctx context.Context, store *database.Store, config database.EnergyConfig, start, end time.Time, intervalType string) (RoomEnergyReport, error) {
	wholeHouse, err := BuildReport(ctx, store, config, start, end, intervalType)
	if err != nil {
		return RoomEnergyReport{}, err
	}
	assignments, err := store.ListRoomEnergyAssignmentsOverlapping(ctx, start, end)
	if err != nil {
		return RoomEnergyReport{}, err
	}
	settings, err := store.GetRoomEnergyConfig(ctx)
	if err != nil {
		return RoomEnergyReport{}, err
	}

	refs := uniqueRoomEnergySourceRefs(assignments)
	buckets := make([]roomcore.Bucket, 0, len(wholeHouse.Points))
	for _, point := range wholeHouse.Points {
		buckets = append(buckets, roomcore.Bucket{Start: point.Start, End: point.End})
	}
	aggregates, err := roomSourceAggregates(ctx, store, refs, assignments, buckets, pricingBoundaries(wholeHouse.PricedIntervals), start, end, intervalType)
	if err != nil {
		return RoomEnergyReport{}, err
	}
	attributed, err := roomcore.Aggregate(buckets, coreAssignments(assignments), coreSourceAggregates(aggregates))
	if err != nil {
		return RoomEnergyReport{}, err
	}
	allocationBuckets := pricedSegmentBuckets(wholeHouse.PricedIntervals)
	allocationBySegment := make(map[string][]roomcore.RoomInterval)
	if len(allocationBuckets) > 0 {
		allocationAttributed, allocationErr := roomcore.Aggregate(allocationBuckets, coreAssignments(assignments), coreSourceAggregates(aggregates))
		if allocationErr != nil {
			return RoomEnergyReport{}, allocationErr
		}
		for _, interval := range allocationAttributed {
			key := interval.Start.UTC().Format(time.RFC3339Nano)
			allocationBySegment[key] = append(allocationBySegment[key], interval)
		}
	}

	roomRecords, err := store.ListRoomsIncludingArchived(ctx)
	if err != nil {
		return RoomEnergyReport{}, err
	}
	roomsByID := make(map[int64]database.Room, len(roomRecords))
	for _, room := range roomRecords {
		roomsByID[room.ID] = room
	}
	report := RoomEnergyReport{
		Start:          start,
		End:            end,
		Interval:       intervalType,
		WholeHouse:     wholeHouse,
		Tolerance:      settings,
		Unmetered:      RoomEnergySeries{Name: "Unmetered / Other", Virtual: true, Quality: roomcore.QualityComplete, Points: make([]RoomEnergyPoint, 0, len(buckets))},
		Reconciliation: make([]RoomReconciliationPoint, 0, len(buckets)),
	}
	seriesByRoom := make(map[int64]*RoomEnergySeries)
	for _, assignment := range assignments {
		if _, found := seriesByRoom[assignment.RoomID]; found {
			continue
		}
		room := roomsByID[assignment.RoomID]
		seriesByRoom[assignment.RoomID] = &RoomEnergySeries{RoomID: assignment.RoomID, Name: room.Name, Icon: room.Icon, Archived: room.Archived, Quality: roomcore.QualityComplete, Points: make([]RoomEnergyPoint, 0, len(buckets))}
	}

	attributedByBucket := make(map[string][]roomcore.RoomInterval)
	for _, interval := range attributed {
		key := interval.Start.UTC().Format(time.RFC3339Nano)
		attributedByBucket[key] = append(attributedByBucket[key], interval)
	}
	pricedByBucket := make(map[string]PricedInterval, len(wholeHouse.PricedIntervals))
	for _, priced := range wholeHouse.PricedIntervals {
		if priced.SampleCount > 0 {
			pricedByBucket[priced.Start.UTC().Format(time.RFC3339Nano)] = priced
		}
	}

	exactRoomCosts := make(map[int64]decimal.Decimal)
	exactUnmeteredCost := decimal.Zero
	for _, bucket := range buckets {
		key := bucket.Start.UTC().Format(time.RFC3339Nano)
		roomIntervals := attributedByBucket[key]
		roomValues := make([]roomcore.RoomEnergy, 0, len(roomIntervals))
		roomDataQuality := roomcore.QualityComplete
		for _, series := range seriesByRoom {
			series.Points = append(series.Points, RoomEnergyPoint{Start: bucket.Start, End: bucket.End, Quality: roomcore.QualityMissing})
		}
		for _, interval := range roomIntervals {
			roomDataQuality = worseRoomQuality(roomDataQuality, interval.Quality)
			series := seriesByRoom[interval.RoomID]
			if series == nil {
				continue
			}
			point := roomPoint(interval)
			series.Points[len(series.Points)-1] = point
			series.Quality = worseRoomQuality(series.Quality, interval.Quality)
			if interval.EnergyKWh != nil {
				addSeriesEnergy(series, *interval.EnergyKWh)
				roomValues = append(roomValues, roomcore.RoomEnergy{RoomID: interval.RoomID, EnergyKWh: *interval.EnergyKWh})
			}
		}

		priced, mainPresent := pricedByBucket[key]
		var mainEnergy *decimal.Decimal
		if mainPresent {
			value := priced.EnergyKWh
			mainEnergy = &value
		}
		reconciled, err := roomcore.Reconcile(roomcore.ReconciliationInput{
			MainEnergyKWh: mainEnergy,
			Rooms:         roomValues,
			Tolerance: roomcore.ReconciliationTolerance{
				AbsoluteKWh: decimal.NewFromFloat(settings.AbsoluteToleranceWh).Div(decimal.NewFromInt(1000)),
				Relative:    decimal.NewFromFloat(settings.RelativeTolerance),
			},
		})
		if err != nil {
			return RoomEnergyReport{}, err
		}
		reconciliationQuality := roomcore.QualityComplete
		if !mainPresent {
			reconciliationQuality = roomcore.QualityMissing
		} else if reconciled.Status == roomcore.ReconciliationWarning {
			reconciliationQuality = roomcore.QualityReconciliationWarning
		}
		// Missing room primaries are not zero consumption. Preserve the
		// arithmetic lower bound, but make reconciliation/unmetered allocation
		// visibly partial or missing instead of presenting the gap as complete.
		reconciliationQuality = worseRoomQuality(reconciliationQuality, roomDataQuality)
		report.Reconciliation = append(report.Reconciliation, RoomReconciliationPoint{Start: bucket.Start, End: bucket.End, ReconciliationResult: reconciled, Quality: reconciliationQuality})

		unmeteredPoint := RoomEnergyPoint{Start: bucket.Start, End: bucket.End, Quality: reconciliationQuality}
		if mainPresent {
			unmetered := reconciled.UnmeteredEnergyKWh
			unmeteredPoint.EnergyKWh = &unmetered
			addSeriesEnergy(&report.Unmetered, unmetered)
		}

		if mainPresent {
			bucketRoomCosts := make(map[int64]decimal.Decimal, len(roomValues))
			for _, room := range roomValues {
				bucketRoomCosts[room.RoomID] = decimal.Zero
				if series := seriesByRoom[room.RoomID]; series != nil && len(series.Points) > 0 && mainEnergy.IsPositive() {
					point := &series.Points[len(series.Points)-1]
					if point.EnergyKWh != nil {
						share := point.EnergyKWh.Div(*mainEnergy)
						point.HouseShare = &share
					}
				}
			}
			bucketUnmeteredCost := decimal.Zero
			bucketNormalized := false
			for _, segment := range pricedSegmentsForAllocation(priced) {
				segmentMain := segment.EnergyKWh
				segmentRooms := roomValuesForIntervals(allocationBySegment[segment.Start.UTC().Format(time.RFC3339Nano)])
				allocation, allocationErr := roomcore.AllocateVariableCost(roomcore.VariableCostAllocationInput{WholeHouseCost: segment.Cost.TotalIncludingVAT, MainEnergyKWh: &segmentMain, Rooms: segmentRooms})
				if allocationErr != nil {
					return RoomEnergyReport{}, fmt.Errorf("%w: %v", ErrRoomEnergyAllocation, allocationErr)
				}
				bucketNormalized = bucketNormalized || allocation.Normalized
				bucketUnmeteredCost = bucketUnmeteredCost.Add(allocation.ExactUnmeteredAllocatedCost)
				for _, roomAllocation := range allocation.Rooms {
					bucketRoomCosts[roomAllocation.RoomID] = bucketRoomCosts[roomAllocation.RoomID].Add(roomAllocation.ExactAllocatedCost)
				}
			}

			bucketExactCosts := make([]roomcore.ExactRoomCost, 0, len(bucketRoomCosts))
			for roomID, cost := range bucketRoomCosts {
				bucketExactCosts = append(bucketExactCosts, roomcore.ExactRoomCost{RoomID: roomID, Cost: cost})
				exactRoomCosts[roomID] = exactRoomCosts[roomID].Add(cost)
			}
			bucketRounded, roundErr := roomcore.RoundFinalCosts(roomcore.FinalCostRoundingInput{WholeHouseCost: priced.Cost.TotalIncludingVAT, Rooms: bucketExactCosts, UnmeteredCost: bucketUnmeteredCost})
			if roundErr != nil {
				return RoomEnergyReport{}, fmt.Errorf("%w: %v", ErrRoomEnergyAllocation, roundErr)
			}
			for _, roomCost := range bucketRounded.Rooms {
				if series := seriesByRoom[roomCost.RoomID]; series != nil && len(series.Points) > 0 {
					point := &series.Points[len(series.Points)-1]
					allocated := roomCost.Cost
					point.AllocatedCost = &allocated
					point.AllocationNormalized = bucketNormalized
				}
			}
			unmeteredCost := bucketRounded.UnmeteredCost
			unmeteredPoint.AllocatedCost = &unmeteredCost
			unmeteredPoint.AllocationNormalized = bucketNormalized
			exactUnmeteredCost = exactUnmeteredCost.Add(bucketUnmeteredCost)
		}
		report.Unmetered.Points = append(report.Unmetered.Points, unmeteredPoint)
		report.Unmetered.Quality = worseRoomQuality(report.Unmetered.Quality, unmeteredPoint.Quality)
	}

	roomIDs := make([]int64, 0, len(seriesByRoom))
	for roomID := range seriesByRoom {
		roomIDs = append(roomIDs, roomID)
	}
	sort.Slice(roomIDs, func(left, right int) bool { return roomIDs[left] < roomIDs[right] })
	for _, roomID := range roomIDs {
		report.Rooms = append(report.Rooms, *seriesByRoom[roomID])
	}

	exactCosts := make([]roomcore.ExactRoomCost, 0, len(exactRoomCosts))
	for _, roomID := range roomIDs {
		exactCosts = append(exactCosts, roomcore.ExactRoomCost{RoomID: roomID, Cost: exactRoomCosts[roomID]})
	}
	if !wholeHouse.Cost.TotalIncludingVAT.IsZero() || len(exactCosts) > 0 || !exactUnmeteredCost.IsZero() {
		rounded, roundErr := roomcore.RoundFinalCosts(roomcore.FinalCostRoundingInput{WholeHouseCost: wholeHouse.Cost.TotalIncludingVAT, Rooms: exactCosts, UnmeteredCost: exactUnmeteredCost})
		if roundErr != nil {
			return RoomEnergyReport{}, fmt.Errorf("%w: %v", ErrRoomEnergyAllocation, roundErr)
		}
		for _, roomCost := range rounded.Rooms {
			if series := seriesByRoom[roomCost.RoomID]; series != nil {
				series.AllocatedCost = roomCost.Cost
			}
		}
		report.Unmetered.AllocatedCost = rounded.UnmeteredCost
		report.TotalAllocatedCost = rounded.WholeHouseCost
		// Copy the final rounded values into the report slice after updating the
		// map-backed series.
		for index := range report.Rooms {
			report.Rooms[index].AllocatedCost = seriesByRoom[report.Rooms[index].RoomID].AllocatedCost
		}
	}
	return report, nil
}

func pricedSegmentsForAllocation(interval PricedInterval) []PricedSegment {
	if len(interval.Segments) > 0 {
		return interval.Segments
	}
	return []PricedSegment{{Start: interval.Start, End: interval.End, EnergyKWh: interval.EnergyKWh, Cost: interval.Cost}}
}

func roomValuesForIntervals(intervals []roomcore.RoomInterval) []roomcore.RoomEnergy {
	result := make([]roomcore.RoomEnergy, 0, len(intervals))
	for _, interval := range intervals {
		if interval.EnergyKWh == nil {
			continue
		}
		result = append(result, roomcore.RoomEnergy{RoomID: interval.RoomID, EnergyKWh: *interval.EnergyKWh})
	}
	return result
}

func uniqueRoomEnergySourceRefs(assignments []database.RoomEnergyAssignment) []database.EnergySourceRef {
	seen := make(map[database.EnergySourceRef]struct{}, len(assignments))
	result := make([]database.EnergySourceRef, 0, len(assignments))
	for _, assignment := range assignments {
		ref := database.EnergySourceRef{DeviceID: assignment.DeviceID, ComponentKey: assignment.ComponentKey}
		if _, duplicate := seen[ref]; duplicate {
			continue
		}
		seen[ref] = struct{}{}
		result = append(result, ref)
	}
	return result
}

func roomSourceAggregates(ctx context.Context, store *database.Store, refs []database.EnergySourceRef, assignments []database.RoomEnergyAssignment, buckets []roomcore.Bucket, priceBoundaries []time.Time, start, end time.Time, intervalType string) ([]database.EnergyAggregate, error) {
	coarse, err := store.ListEnergyAggregatesForSources(ctx, refs, start, end, intervalType)
	if err != nil || intervalType == "minute" || len(coarse) == 0 {
		return coarse, err
	}

	assignmentsBySource := make(map[database.EnergySourceRef][]database.RoomEnergyAssignment)
	for _, assignment := range assignments {
		ref := database.EnergySourceRef{DeviceID: assignment.DeviceID, ComponentKey: assignment.ComponentKey}
		assignmentsBySource[ref] = append(assignmentsBySource[ref], assignment)
	}
	replacements := make(map[int64][]database.EnergyAggregate)
	for _, bucket := range buckets {
		affected := make([]database.EnergySourceRef, 0)
		priceBoundary := timeBoundaryInside(bucket, priceBoundaries)
		for ref, sourceAssignments := range assignmentsBySource {
			if priceBoundary || sourceAssignmentBoundaryInside(bucket, sourceAssignments) {
				affected = append(affected, ref)
			}
		}
		if len(affected) == 0 {
			continue
		}
		minutes, minuteErr := store.ListEnergyAggregatesForSources(ctx, affected, bucket.Start, bucket.End, "minute")
		if minuteErr != nil {
			return nil, minuteErr
		}
		minutesBySource := make(map[database.EnergySourceRef][]database.EnergyAggregate)
		for _, minute := range minutes {
			ref := database.EnergySourceRef{DeviceID: minute.DeviceID, ComponentKey: minute.ComponentKey}
			minutesBySource[ref] = append(minutesBySource[ref], minute)
		}
		for _, aggregate := range coarse {
			if !aggregate.IntervalStart.Before(bucket.End) || !bucket.Start.Before(aggregate.IntervalEnd) {
				continue
			}
			ref := database.EnergySourceRef{DeviceID: aggregate.DeviceID, ComponentKey: aggregate.ComponentKey}
			fine := minutesBySource[ref]
			if len(fine) == 0 || !minuteEnergyMatchesCoarse(fine, aggregate.EnergyConsumedWh) {
				continue
			}
			replacements[aggregate.ID] = append([]database.EnergyAggregate(nil), fine...)
		}
	}
	if len(replacements) == 0 {
		return coarse, nil
	}
	result := make([]database.EnergyAggregate, 0, len(coarse))
	for _, aggregate := range coarse {
		if fine, replaced := replacements[aggregate.ID]; replaced {
			result = append(result, fine...)
			continue
		}
		result = append(result, aggregate)
	}
	return result, nil
}

func pricingBoundaries(intervals []PricedInterval) []time.Time {
	seen := make(map[string]struct{})
	result := make([]time.Time, 0)
	for _, interval := range intervals {
		for _, segment := range interval.Segments {
			if !segment.Start.After(interval.Start) || !segment.Start.Before(interval.End) {
				continue
			}
			key := segment.Start.UTC().Format(time.RFC3339Nano)
			if _, duplicate := seen[key]; duplicate {
				continue
			}
			seen[key] = struct{}{}
			result = append(result, segment.Start)
		}
	}
	return result
}

func timeBoundaryInside(bucket roomcore.Bucket, boundaries []time.Time) bool {
	for _, boundary := range boundaries {
		if boundary.After(bucket.Start) && boundary.Before(bucket.End) {
			return true
		}
	}
	return false
}

func pricedSegmentBuckets(intervals []PricedInterval) []roomcore.Bucket {
	result := make([]roomcore.Bucket, 0)
	for _, interval := range intervals {
		if interval.SampleCount < 1 {
			continue
		}
		if len(interval.Segments) == 0 {
			result = append(result, roomcore.Bucket{Start: interval.Start, End: interval.End})
			continue
		}
		for _, segment := range interval.Segments {
			result = append(result, roomcore.Bucket{Start: segment.Start, End: segment.End})
		}
	}
	return result
}

func sourceAssignmentBoundaryInside(bucket roomcore.Bucket, assignments []database.RoomEnergyAssignment) bool {
	for _, assignment := range assignments {
		if assignment.EffectiveFrom.After(bucket.Start) && assignment.EffectiveFrom.Before(bucket.End) {
			return true
		}
		if assignment.EffectiveTo != nil && assignment.EffectiveTo.After(bucket.Start) && assignment.EffectiveTo.Before(bucket.End) {
			return true
		}
	}
	return false
}

func minuteEnergyMatchesCoarse(minutes []database.EnergyAggregate, coarseWh float64) bool {
	total := decimal.Zero
	for _, minute := range minutes {
		if minute.SampleCount < 1 || minute.CalculationQuality == "missing" {
			return false
		}
		total = total.Add(decimal.NewFromFloat(minute.EnergyConsumedWh))
	}
	difference := total.Sub(decimal.NewFromFloat(coarseWh)).Abs()
	return difference.LessThanOrEqual(decimal.RequireFromString("0.000001"))
}

func coreAssignments(assignments []database.RoomEnergyAssignment) []roomcore.Assignment {
	result := make([]roomcore.Assignment, 0, len(assignments))
	for _, assignment := range assignments {
		result = append(result, roomcore.Assignment{
			ID:                 assignment.ID,
			RoomID:             assignment.RoomID,
			Source:             roomcore.SourceKey{DeviceID: assignment.DeviceID, ComponentKey: assignment.ComponentKey},
			Role:               roomcore.Role(assignment.AggregationRole),
			ParentAssignmentID: assignment.ParentAssignmentID,
			// The half-open effective interval preserves historical attribution.
			// Enabled controls current collection; a closed archived assignment
			// remains authoritative for its historical interval.
			Enabled:       assignment.Enabled,
			EffectiveFrom: assignment.EffectiveFrom,
			EffectiveTo:   copyTimePointer(assignment.EffectiveTo),
		})
	}
	return result
}

func coreSourceAggregates(aggregates []database.EnergyAggregate) []roomcore.SourceAggregate {
	result := make([]roomcore.SourceAggregate, 0, len(aggregates))
	for _, aggregate := range aggregates {
		value := roomcore.SourceAggregate{
			Source:        roomcore.SourceKey{DeviceID: aggregate.DeviceID, ComponentKey: aggregate.ComponentKey},
			Start:         aggregate.IntervalStart,
			End:           aggregate.IntervalEnd,
			AveragePowerW: decimalFloatPointer(aggregate.AveragePowerW),
			PeakPowerW:    decimalFloatPointer(aggregate.MaximumPowerW),
			SampleCount:   aggregate.SampleCount,
			Quality:       databaseAggregateQuality(aggregate.CalculationQuality),
		}
		if aggregate.SampleCount > 0 && aggregate.CalculationQuality != "missing" {
			energy := decimal.NewFromFloat(aggregate.EnergyConsumedWh).Div(decimal.NewFromInt(1000))
			value.EnergyKWh = &energy
		}
		result = append(result, value)
	}
	return result
}

func databaseAggregateQuality(value string) roomcore.Quality {
	switch value {
	case "partial":
		return roomcore.QualityPartial
	case "stale":
		return roomcore.QualityStale
	case "missing":
		return roomcore.QualityMissing
	case "calculated", "counter_reset":
		return roomcore.QualityEstimated
	default:
		return roomcore.QualityComplete
	}
}

func roomPoint(interval roomcore.RoomInterval) RoomEnergyPoint {
	return RoomEnergyPoint{
		Start:             interval.Start,
		End:               interval.End,
		EnergyKWh:         copyDecimalPointer(interval.EnergyKWh),
		AveragePowerW:     copyDecimalPointer(interval.AveragePowerW),
		PeakPowerW:        copyDecimalPointer(interval.PeakPowerW),
		CoverageRatio:     copyDecimalPointer(interval.CoverageRatio),
		Quality:           interval.Quality,
		ExpectedSources:   interval.ExpectedSources,
		ReportingSources:  interval.ReportingSources,
		Sources:           append([]roomcore.SourceContribution(nil), interval.Sources...),
		SubmeterEnergyKWh: interval.SubmeterEnergyKWh,
		OtherOnPrimaryKWh: interval.OtherOnPrimaryKWh,
		SubmeterOverlap:   interval.SubmeterOverlap,
	}
}

func addSeriesEnergy(series *RoomEnergySeries, energy decimal.Decimal) {
	if series.EnergyKWh == nil {
		value := energy
		series.EnergyKWh = &value
		return
	}
	value := series.EnergyKWh.Add(energy)
	series.EnergyKWh = &value
}

func worseRoomQuality(left, right roomcore.Quality) roomcore.Quality {
	priority := map[roomcore.Quality]int{
		roomcore.QualityComplete:              0,
		roomcore.QualityEstimated:             1,
		roomcore.QualityStale:                 2,
		roomcore.QualityPartial:               3,
		roomcore.QualityMissing:               4,
		roomcore.QualityReconciliationWarning: 5,
	}
	if priority[right] > priority[left] {
		return right
	}
	return left
}

func decimalFloatPointer(value *float64) *decimal.Decimal {
	if value == nil {
		return nil
	}
	result := decimal.NewFromFloat(*value)
	return &result
}

func copyDecimalPointer(value *decimal.Decimal) *decimal.Decimal {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}

func copyTimePointer(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}
