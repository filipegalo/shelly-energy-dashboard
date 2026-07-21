package rooms

import (
	"errors"
	"sort"
	"strings"
	"time"

	"github.com/shopspring/decimal"
)

type Quality string

const (
	QualityComplete              Quality = "complete"
	QualityPartial               Quality = "partial"
	QualityStale                 Quality = "stale"
	QualityMissing               Quality = "missing"
	QualityEstimated             Quality = "estimated"
	QualityReconciliationWarning Quality = "reconciliation_warning"
)

type Role string

const (
	RolePrimary  Role = "primary"
	RoleSubmeter Role = "submeter"
	RoleExcluded Role = "excluded"
)

type SourceKey struct {
	DeviceID     int64
	ComponentKey string
}

type Assignment struct {
	ID                 int64
	RoomID             int64
	Source             SourceKey
	Role               Role
	ParentAssignmentID int64
	Enabled            bool
	EffectiveFrom      time.Time
	EffectiveTo        *time.Time
}

type Bucket struct {
	Start time.Time
	End   time.Time
}

type SourceAggregate struct {
	Source        SourceKey
	Start         time.Time
	End           time.Time
	EnergyKWh     *decimal.Decimal
	AveragePowerW *decimal.Decimal
	PeakPowerW    *decimal.Decimal
	SampleCount   int
	Quality       Quality
}

type SourceContribution struct {
	AssignmentID       int64
	Source             SourceKey
	Role               Role
	ParentAssignmentID int64
	EnergyKWh          decimal.Decimal
	HasData            bool
	Quality            Quality
}

type RoomInterval struct {
	RoomID                 int64
	Start                  time.Time
	End                    time.Time
	EnergyKWh              *decimal.Decimal
	AveragePowerW          *decimal.Decimal
	PeakPowerW             *decimal.Decimal
	ExpectedSources        int
	ReportingSources       int
	ExpectedSourceSeconds  decimal.Decimal
	ReportingSourceSeconds decimal.Decimal
	CoverageRatio          *decimal.Decimal
	Quality                Quality
	Sources                []SourceContribution
	SubmeterEnergyKWh      decimal.Decimal
	OtherOnPrimaryKWh      decimal.Decimal
	SubmeterOverlap        bool
}

type LiveReading struct {
	Source       SourceKey
	PowerW       *decimal.Decimal
	RecordedAt   time.Time
	PollInterval time.Duration
}

type LiveRoomPower struct {
	RoomID          int64
	PowerW          *decimal.Decimal
	ExpectedSources int
	FreshSources    int
	StaleSources    int
	MissingSources  int
	Quality         Quality
	LastUpdate      time.Time
}

// Aggregate attributes source aggregates to effective-dated room assignments.
// Intervals are half-open. A source interval crossing a room boundary is split
// proportionally and marked estimated; energy is conserved across the split.
func Aggregate(buckets []Bucket, assignments []Assignment, aggregates []SourceAggregate) ([]RoomInterval, error) {
	if err := validateAggregationInputs(buckets, assignments, aggregates); err != nil {
		return nil, err
	}
	aggregatesBySource := make(map[SourceKey][]SourceAggregate)
	for _, aggregate := range aggregates {
		aggregatesBySource[aggregate.Source] = append(aggregatesBySource[aggregate.Source], aggregate)
	}
	for source := range aggregatesBySource {
		values := aggregatesBySource[source]
		sort.Slice(values, func(left, right int) bool { return values[left].Start.Before(values[right].Start) })
		aggregatesBySource[source] = values
	}

	result := make([]RoomInterval, 0)
	for _, bucket := range buckets {
		rooms := make(map[int64]*roomAccumulator)
		for _, assignment := range assignments {
			activeStart, activeEnd, ok := assignmentBucketOverlap(assignment, bucket)
			if !ok || !assignment.Enabled {
				continue
			}
			room := rooms[assignment.RoomID]
			if room == nil {
				room = newRoomAccumulator(assignment.RoomID, bucket)
				rooms[assignment.RoomID] = room
			}
			source := room.source(assignment)
			if assignment.Role == RolePrimary {
				exposure := durationDecimal(activeEnd.Sub(activeStart))
				room.expectedSeconds = room.expectedSeconds.Add(exposure)
				room.expectedSources[assignment.Source] = struct{}{}
			}

			sourceAggregates := aggregatesBySource[assignment.Source]
			first := sort.Search(len(sourceAggregates), func(index int) bool {
				return sourceAggregates[index].End.After(activeStart)
			})
			for _, aggregate := range sourceAggregates[first:] {
				if !aggregate.Start.Before(activeEnd) {
					break
				}
				start, end, overlaps := intersect(activeStart, activeEnd, aggregate.Start, aggregate.End)
				if !overlaps || aggregate.EnergyKWh == nil || aggregate.SampleCount < 1 {
					continue
				}
				seconds := durationDecimal(end.Sub(start))
				totalSeconds := durationDecimal(aggregate.End.Sub(aggregate.Start))
				fraction := seconds.Div(totalSeconds)
				energy := aggregate.EnergyKWh.Mul(fraction)
				source.energy = source.energy.Add(energy)
				source.hasData = true
				source.quality = worseQuality(source.quality, normalizedQuality(aggregate.Quality))
				if fraction.LessThan(decimal.NewFromInt(1)) {
					source.quality = worseQuality(source.quality, QualityEstimated)
				}

				switch assignment.Role {
				case RolePrimary:
					room.energy = room.energy.Add(energy)
					room.hasPrimaryData = true
					room.reportingSeconds = room.reportingSeconds.Add(seconds)
					room.reportingSources[assignment.Source] = struct{}{}
					room.quality = worseQuality(room.quality, source.quality)
					if aggregate.AveragePowerW != nil {
						room.powerWSeconds = room.powerWSeconds.Add(aggregate.AveragePowerW.Mul(seconds))
						room.hasAveragePower = true
					}
					if aggregate.PeakPowerW != nil {
						current, found := room.peaks[assignment.Source]
						if !found || aggregate.PeakPowerW.GreaterThan(current) {
							room.peaks[assignment.Source] = *aggregate.PeakPowerW
						}
					}
				case RoleSubmeter:
					room.submeterEnergy = room.submeterEnergy.Add(energy)
				}
			}
		}

		roomIDs := make([]int64, 0, len(rooms))
		for roomID := range rooms {
			roomIDs = append(roomIDs, roomID)
		}
		sort.Slice(roomIDs, func(left, right int) bool { return roomIDs[left] < roomIDs[right] })
		for _, roomID := range roomIDs {
			result = append(result, rooms[roomID].result())
		}
	}
	return result, nil
}

func AggregateLivePower(at time.Time, assignments []Assignment, readings []LiveReading) ([]LiveRoomPower, error) {
	if at.IsZero() {
		return nil, errors.New("live room power time is required")
	}
	latest := make(map[SourceKey]LiveReading, len(readings))
	for _, reading := range readings {
		if reading.Source.DeviceID < 1 || strings.TrimSpace(reading.Source.ComponentKey) == "" || reading.RecordedAt.IsZero() || reading.RecordedAt.After(at.Add(5*time.Minute)) {
			return nil, errors.New("live room reading is invalid")
		}
		if reading.PowerW != nil && reading.PowerW.IsNegative() {
			return nil, errors.New("live room power cannot be negative")
		}
		if previous, found := latest[reading.Source]; !found || reading.RecordedAt.After(previous.RecordedAt) {
			latest[reading.Source] = reading
		}
	}

	type liveAccumulator struct {
		LiveRoomPower
		power decimal.Decimal
	}
	rooms := map[int64]*liveAccumulator{}
	seen := map[int64]map[SourceKey]struct{}{}
	for _, assignment := range assignments {
		if !assignment.Enabled || assignment.Role != RolePrimary || !assignmentActiveAt(assignment, at) {
			continue
		}
		if err := validateAssignment(assignment); err != nil {
			return nil, err
		}
		if seen[assignment.RoomID] == nil {
			seen[assignment.RoomID] = map[SourceKey]struct{}{}
		}
		if _, duplicate := seen[assignment.RoomID][assignment.Source]; duplicate {
			continue
		}
		seen[assignment.RoomID][assignment.Source] = struct{}{}
		room := rooms[assignment.RoomID]
		if room == nil {
			room = &liveAccumulator{LiveRoomPower: LiveRoomPower{RoomID: assignment.RoomID}}
			rooms[assignment.RoomID] = room
		}
		room.ExpectedSources++
		reading, found := latest[assignment.Source]
		if !found || reading.PowerW == nil {
			room.MissingSources++
			continue
		}
		poll := reading.PollInterval
		if poll <= 0 {
			poll = 10 * time.Second
		}
		if at.Sub(reading.RecordedAt) > 2*poll {
			room.StaleSources++
			if reading.RecordedAt.After(room.LastUpdate) {
				room.LastUpdate = reading.RecordedAt
			}
			continue
		}
		room.FreshSources++
		room.power = room.power.Add(*reading.PowerW)
		if reading.RecordedAt.After(room.LastUpdate) {
			room.LastUpdate = reading.RecordedAt
		}
	}

	roomIDs := make([]int64, 0, len(rooms))
	for roomID := range rooms {
		roomIDs = append(roomIDs, roomID)
	}
	sort.Slice(roomIDs, func(left, right int) bool { return roomIDs[left] < roomIDs[right] })
	result := make([]LiveRoomPower, 0, len(roomIDs))
	for _, roomID := range roomIDs {
		room := rooms[roomID]
		switch {
		case room.FreshSources > 0:
			power := room.power
			room.PowerW = &power
			if room.FreshSources == room.ExpectedSources {
				room.Quality = QualityComplete
			} else {
				room.Quality = QualityPartial
			}
		case room.StaleSources > 0:
			room.Quality = QualityStale
		default:
			room.Quality = QualityMissing
		}
		result = append(result, room.LiveRoomPower)
	}
	return result, nil
}

type roomAccumulator struct {
	roomID           int64
	bucket           Bucket
	energy           decimal.Decimal
	hasPrimaryData   bool
	powerWSeconds    decimal.Decimal
	hasAveragePower  bool
	peaks            map[SourceKey]decimal.Decimal
	expectedSources  map[SourceKey]struct{}
	reportingSources map[SourceKey]struct{}
	expectedSeconds  decimal.Decimal
	reportingSeconds decimal.Decimal
	quality          Quality
	sources          map[int64]*sourceAccumulator
	submeterEnergy   decimal.Decimal
}

type sourceAccumulator struct {
	assignment Assignment
	energy     decimal.Decimal
	hasData    bool
	quality    Quality
}

func newRoomAccumulator(roomID int64, bucket Bucket) *roomAccumulator {
	return &roomAccumulator{roomID: roomID, bucket: bucket, peaks: map[SourceKey]decimal.Decimal{}, expectedSources: map[SourceKey]struct{}{}, reportingSources: map[SourceKey]struct{}{}, quality: QualityComplete, sources: map[int64]*sourceAccumulator{}}
}

func (r *roomAccumulator) source(assignment Assignment) *sourceAccumulator {
	value := r.sources[assignment.ID]
	if value == nil {
		value = &sourceAccumulator{assignment: assignment, quality: QualityComplete}
		r.sources[assignment.ID] = value
	}
	return value
}

func (r *roomAccumulator) result() RoomInterval {
	value := RoomInterval{RoomID: r.roomID, Start: r.bucket.Start, End: r.bucket.End, ExpectedSources: len(r.expectedSources), ReportingSources: len(r.reportingSources), ExpectedSourceSeconds: r.expectedSeconds, ReportingSourceSeconds: decimal.Min(r.reportingSeconds, r.expectedSeconds), Quality: r.quality, SubmeterEnergyKWh: r.submeterEnergy}
	if r.expectedSeconds.IsPositive() {
		coverage := decimal.Min(value.ReportingSourceSeconds.Div(r.expectedSeconds), decimal.NewFromInt(1))
		value.CoverageRatio = &coverage
	}
	if r.hasPrimaryData {
		energy := r.energy
		value.EnergyKWh = &energy
		if r.hasAveragePower {
			average := r.powerWSeconds.Div(durationDecimal(r.bucket.End.Sub(r.bucket.Start)))
			value.AveragePowerW = &average
		}
		if len(r.peaks) > 0 {
			peak := decimal.Zero
			for _, sourcePeak := range r.peaks {
				peak = peak.Add(sourcePeak)
			}
			value.PeakPowerW = &peak
		}
	} else {
		value.Quality = QualityMissing
	}
	if value.ExpectedSources > 0 && value.ReportingSources < value.ExpectedSources {
		value.Quality = QualityPartial
		if value.ReportingSources == 0 {
			value.Quality = QualityMissing
		}
	}
	if value.EnergyKWh != nil {
		value.OtherOnPrimaryKWh = value.EnergyKWh.Sub(value.SubmeterEnergyKWh)
		if value.OtherOnPrimaryKWh.IsNegative() {
			value.OtherOnPrimaryKWh = decimal.Zero
			value.SubmeterOverlap = true
		}
	}
	assignmentIDs := make([]int64, 0, len(r.sources))
	for assignmentID := range r.sources {
		assignmentIDs = append(assignmentIDs, assignmentID)
	}
	sort.Slice(assignmentIDs, func(left, right int) bool { return assignmentIDs[left] < assignmentIDs[right] })
	for _, assignmentID := range assignmentIDs {
		source := r.sources[assignmentID]
		quality := source.quality
		if !source.hasData {
			quality = QualityMissing
		}
		value.Sources = append(value.Sources, SourceContribution{AssignmentID: source.assignment.ID, Source: source.assignment.Source, Role: source.assignment.Role, ParentAssignmentID: source.assignment.ParentAssignmentID, EnergyKWh: source.energy, HasData: source.hasData, Quality: quality})
	}
	return value
}

func validateAggregationInputs(buckets []Bucket, assignments []Assignment, aggregates []SourceAggregate) error {
	if len(buckets) == 0 {
		return errors.New("at least one room aggregation bucket is required")
	}
	for _, bucket := range buckets {
		if bucket.Start.IsZero() || !bucket.Start.Before(bucket.End) {
			return errors.New("room aggregation bucket is invalid")
		}
	}
	for _, assignment := range assignments {
		if err := validateAssignment(assignment); err != nil {
			return err
		}
	}
	for _, aggregate := range aggregates {
		if aggregate.Source.DeviceID < 1 || strings.TrimSpace(aggregate.Source.ComponentKey) == "" || aggregate.Start.IsZero() || !aggregate.Start.Before(aggregate.End) || aggregate.SampleCount < 0 {
			return errors.New("source aggregate is invalid")
		}
		for _, value := range []*decimal.Decimal{aggregate.EnergyKWh, aggregate.AveragePowerW, aggregate.PeakPowerW} {
			if value != nil && value.IsNegative() {
				return errors.New("source aggregate values cannot be negative")
			}
		}
	}
	return nil
}

func validateAssignment(assignment Assignment) error {
	if assignment.ID < 1 || assignment.RoomID < 1 || assignment.Source.DeviceID < 1 || strings.TrimSpace(assignment.Source.ComponentKey) == "" || assignment.EffectiveFrom.IsZero() {
		return errors.New("room energy assignment is invalid")
	}
	if assignment.Role != RolePrimary && assignment.Role != RoleSubmeter && assignment.Role != RoleExcluded {
		return errors.New("room energy assignment role is invalid")
	}
	if assignment.Role == RoleSubmeter && assignment.ParentAssignmentID < 1 {
		return errors.New("room energy submeter requires a parent")
	}
	if assignment.Role != RoleSubmeter && assignment.ParentAssignmentID != 0 {
		return errors.New("only a submeter may have a parent")
	}
	if assignment.EffectiveTo != nil && !assignment.EffectiveFrom.Before(*assignment.EffectiveTo) {
		return errors.New("room energy assignment interval is invalid")
	}
	return nil
}

func assignmentBucketOverlap(assignment Assignment, bucket Bucket) (time.Time, time.Time, bool) {
	end := bucket.End
	if assignment.EffectiveTo != nil && assignment.EffectiveTo.Before(end) {
		end = *assignment.EffectiveTo
	}
	start := bucket.Start
	if assignment.EffectiveFrom.After(start) {
		start = assignment.EffectiveFrom
	}
	return start, end, start.Before(end)
}

func assignmentActiveAt(assignment Assignment, at time.Time) bool {
	return !at.Before(assignment.EffectiveFrom) && (assignment.EffectiveTo == nil || at.Before(*assignment.EffectiveTo))
}

func intersect(leftStart, leftEnd, rightStart, rightEnd time.Time) (time.Time, time.Time, bool) {
	start := leftStart
	if rightStart.After(start) {
		start = rightStart
	}
	end := leftEnd
	if rightEnd.Before(end) {
		end = rightEnd
	}
	return start, end, start.Before(end)
}

func durationDecimal(value time.Duration) decimal.Decimal {
	return decimal.NewFromInt(value.Nanoseconds()).Div(decimal.NewFromInt(int64(time.Second)))
}

func normalizedQuality(value Quality) Quality {
	switch value {
	case QualityComplete, QualityPartial, QualityStale, QualityMissing, QualityEstimated:
		return value
	default:
		return QualityComplete
	}
}

func worseQuality(left, right Quality) Quality {
	priority := map[Quality]int{QualityComplete: 0, QualityEstimated: 1, QualityStale: 2, QualityPartial: 3, QualityMissing: 4, QualityReconciliationWarning: 5}
	if priority[right] > priority[left] {
		return right
	}
	return left
}
