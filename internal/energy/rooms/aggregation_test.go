package rooms

import (
	"reflect"
	"testing"
	"time"

	"github.com/shopspring/decimal"
)

func TestAggregateSplitsAssignmentBoundaryInsideMinute(t *testing.T) {
	t.Parallel()
	start := time.Date(2026, time.July, 15, 10, 0, 0, 0, time.UTC)
	boundary := start.Add(30 * time.Second)
	end := start.Add(time.Minute)
	source := SourceKey{DeviceID: 7, ComponentKey: "switch:0"}
	assignments := []Assignment{
		{ID: 1, RoomID: 10, Source: source, Role: RolePrimary, Enabled: true, EffectiveFrom: start, EffectiveTo: timePointer(boundary)},
		{ID: 2, RoomID: 20, Source: source, Role: RolePrimary, Enabled: true, EffectiveFrom: boundary},
	}
	aggregates := []SourceAggregate{{Source: source, Start: start, End: end, EnergyKWh: decimalPointer("0.060"), AveragePowerW: decimalPointer("60"), PeakPowerW: decimalPointer("90"), SampleCount: 6, Quality: QualityComplete}}

	result, err := Aggregate([]Bucket{{Start: start, End: end}}, assignments, aggregates)
	if err != nil {
		t.Fatal(err)
	}
	if len(result) != 2 {
		t.Fatalf("room interval count = %d, want 2: %+v", len(result), result)
	}
	assertRoomIntervalEnergy(t, result[0], 10, "0.030", QualityEstimated)
	assertRoomIntervalEnergy(t, result[1], 20, "0.030", QualityEstimated)
	if !result[0].ExpectedSourceSeconds.Equal(decimal.NewFromInt(30)) || !result[0].ReportingSourceSeconds.Equal(decimal.NewFromInt(30)) {
		t.Fatalf("first coverage seconds = expected %s reporting %s", result[0].ExpectedSourceSeconds, result[0].ReportingSourceSeconds)
	}
}

func TestAggregateSumsOnlyRootPrimarySourcesAndKeepsBreakdown(t *testing.T) {
	t.Parallel()
	start := time.Date(2026, time.July, 15, 0, 0, 0, 0, time.UTC)
	end := start.Add(24 * time.Hour)
	primary := SourceKey{DeviceID: 1, ComponentKey: "em:0"}
	dishwasher := SourceKey{DeviceID: 2, ComponentKey: "switch:0"}
	fridge := SourceKey{DeviceID: 3, ComponentKey: "switch:0"}
	excluded := SourceKey{DeviceID: 4, ComponentKey: "switch:0"}
	assignments := []Assignment{
		{ID: 1, RoomID: 10, Source: primary, Role: RolePrimary, Enabled: true, EffectiveFrom: start},
		{ID: 2, RoomID: 10, Source: dishwasher, Role: RoleSubmeter, ParentAssignmentID: 1, Enabled: true, EffectiveFrom: start},
		{ID: 3, RoomID: 10, Source: fridge, Role: RoleSubmeter, ParentAssignmentID: 1, Enabled: true, EffectiveFrom: start},
		{ID: 4, RoomID: 10, Source: excluded, Role: RoleExcluded, Enabled: true, EffectiveFrom: start},
	}
	aggregates := []SourceAggregate{
		{Source: primary, Start: start, End: end, EnergyKWh: decimalPointer("10"), SampleCount: 24, Quality: QualityComplete},
		{Source: dishwasher, Start: start, End: end, EnergyKWh: decimalPointer("3"), SampleCount: 24, Quality: QualityComplete},
		{Source: fridge, Start: start, End: end, EnergyKWh: decimalPointer("2"), SampleCount: 24, Quality: QualityComplete},
		{Source: excluded, Start: start, End: end, EnergyKWh: decimalPointer("7"), SampleCount: 24, Quality: QualityComplete},
	}

	result, err := Aggregate([]Bucket{{Start: start, End: end}}, assignments, aggregates)
	if err != nil {
		t.Fatal(err)
	}
	if len(result) != 1 {
		t.Fatalf("room interval count = %d", len(result))
	}
	room := result[0]
	assertRoomIntervalEnergy(t, room, 10, "10", QualityComplete)
	if len(room.Sources) != 4 {
		t.Fatalf("source breakdown = %+v", room.Sources)
	}
	if room.Sources[1].Role != RoleSubmeter || room.Sources[1].ParentAssignmentID != 1 || !room.Sources[1].EnergyKWh.Equal(decimal.NewFromInt(3)) {
		t.Fatalf("dishwasher breakdown = %+v", room.Sources[1])
	}
	if !room.SubmeterEnergyKWh.Equal(decimal.NewFromInt(5)) || !room.OtherOnPrimaryKWh.Equal(decimal.NewFromInt(5)) {
		t.Fatalf("submeter/remainder = %s/%s", room.SubmeterEnergyKWh, room.OtherOnPrimaryKWh)
	}
}

func TestAggregateMultiplePrimariesChannelsAndPartialData(t *testing.T) {
	t.Parallel()
	start := time.Date(2026, time.July, 15, 10, 0, 0, 0, time.UTC)
	end := start.Add(time.Hour)
	channel0 := SourceKey{DeviceID: 1, ComponentKey: "switch:0"}
	channel1 := SourceKey{DeviceID: 1, ComponentKey: "switch:1"}
	channel2 := SourceKey{DeviceID: 1, ComponentKey: "switch:2"}
	assignments := []Assignment{
		{ID: 1, RoomID: 10, Source: channel0, Role: RolePrimary, Enabled: true, EffectiveFrom: start},
		{ID: 2, RoomID: 10, Source: channel1, Role: RolePrimary, Enabled: true, EffectiveFrom: start},
		{ID: 3, RoomID: 20, Source: channel2, Role: RolePrimary, Enabled: true, EffectiveFrom: start},
	}
	aggregates := []SourceAggregate{
		{Source: channel0, Start: start, End: end, EnergyKWh: decimalPointer("5"), SampleCount: 60, Quality: QualityComplete},
		// Channel 1 is missing. A missing source must not turn the available
		// Channel 0 measurement into zero or complete data.
		{Source: channel2, Start: start, End: end, EnergyKWh: decimalPointer("2"), SampleCount: 60, Quality: QualityComplete},
	}

	result, err := Aggregate([]Bucket{{Start: start, End: end}}, assignments, aggregates)
	if err != nil {
		t.Fatal(err)
	}
	if len(result) != 2 {
		t.Fatalf("room interval count = %d: %+v", len(result), result)
	}
	assertRoomIntervalEnergy(t, result[0], 10, "5", QualityPartial)
	if result[0].ExpectedSources != 2 || result[0].ReportingSources != 1 || result[0].CoverageRatio == nil || !result[0].CoverageRatio.Equal(decimal.RequireFromString("0.5")) {
		t.Fatalf("partial coverage = %+v", result[0])
	}
	assertRoomIntervalEnergy(t, result[1], 20, "2", QualityComplete)
}

func TestAggregateDistinguishesSampledZeroFromMissing(t *testing.T) {
	t.Parallel()
	start := time.Date(2026, time.July, 15, 10, 0, 0, 0, time.UTC)
	end := start.Add(time.Hour)
	zeroSource := SourceKey{DeviceID: 1, ComponentKey: "switch:0"}
	missingSource := SourceKey{DeviceID: 2, ComponentKey: "switch:0"}
	assignments := []Assignment{
		{ID: 1, RoomID: 10, Source: zeroSource, Role: RolePrimary, Enabled: true, EffectiveFrom: start},
		{ID: 2, RoomID: 20, Source: missingSource, Role: RolePrimary, Enabled: true, EffectiveFrom: start},
	}
	aggregates := []SourceAggregate{{Source: zeroSource, Start: start, End: end, EnergyKWh: decimalPointer("0"), SampleCount: 60, Quality: QualityComplete}}

	result, err := Aggregate([]Bucket{{Start: start, End: end}}, assignments, aggregates)
	if err != nil {
		t.Fatal(err)
	}
	if len(result) != 2 || result[0].EnergyKWh == nil || !result[0].EnergyKWh.IsZero() || result[0].Quality != QualityComplete {
		t.Fatalf("sampled zero = %+v", result)
	}
	if result[1].EnergyKWh != nil || result[1].Quality != QualityMissing || result[1].ReportingSources != 0 {
		t.Fatalf("missing room = %+v", result[1])
	}
}

func TestAggregateLivePowerUsesOnlyFreshRootPrimaryReadings(t *testing.T) {
	t.Parallel()
	at := time.Date(2026, time.July, 15, 10, 0, 30, 0, time.UTC)
	primary := SourceKey{DeviceID: 1, ComponentKey: "em:0"}
	stalePrimary := SourceKey{DeviceID: 2, ComponentKey: "switch:0"}
	submeter := SourceKey{DeviceID: 3, ComponentKey: "switch:0"}
	excluded := SourceKey{DeviceID: 4, ComponentKey: "switch:0"}
	assignments := []Assignment{
		{ID: 1, RoomID: 10, Source: primary, Role: RolePrimary, Enabled: true, EffectiveFrom: at.Add(-time.Hour)},
		{ID: 2, RoomID: 10, Source: stalePrimary, Role: RolePrimary, Enabled: true, EffectiveFrom: at.Add(-time.Hour)},
		{ID: 3, RoomID: 10, Source: submeter, Role: RoleSubmeter, ParentAssignmentID: 1, Enabled: true, EffectiveFrom: at.Add(-time.Hour)},
		{ID: 4, RoomID: 10, Source: excluded, Role: RoleExcluded, Enabled: true, EffectiveFrom: at.Add(-time.Hour)},
	}
	readings := []LiveReading{
		{Source: primary, PowerW: decimalPointer("1000"), RecordedAt: at.Add(-5 * time.Second), PollInterval: 10 * time.Second},
		{Source: stalePrimary, PowerW: decimalPointer("500"), RecordedAt: at.Add(-30 * time.Second), PollInterval: 10 * time.Second},
		{Source: submeter, PowerW: decimalPointer("200"), RecordedAt: at.Add(-time.Second), PollInterval: 10 * time.Second},
		{Source: excluded, PowerW: decimalPointer("300"), RecordedAt: at.Add(-time.Second), PollInterval: 10 * time.Second},
	}

	result, err := AggregateLivePower(at, assignments, readings)
	if err != nil {
		t.Fatal(err)
	}
	if len(result) != 1 || result[0].PowerW == nil || !result[0].PowerW.Equal(decimal.NewFromInt(1000)) {
		t.Fatalf("live power = %+v", result)
	}
	if result[0].ExpectedSources != 2 || result[0].FreshSources != 1 || result[0].StaleSources != 1 || result[0].Quality != QualityPartial {
		t.Fatalf("live health = %+v", result[0])
	}
}

func TestAggregateDoesNotMutateInputs(t *testing.T) {
	t.Parallel()
	start := time.Date(2026, time.July, 15, 10, 0, 0, 0, time.UTC)
	end := start.Add(time.Hour)
	source := SourceKey{DeviceID: 1, ComponentKey: "switch:0"}
	assignments := []Assignment{{ID: 1, RoomID: 10, Source: source, Role: RolePrimary, Enabled: true, EffectiveFrom: start}}
	aggregates := []SourceAggregate{{Source: source, Start: start, End: end, EnergyKWh: decimalPointer("1"), SampleCount: 60, Quality: QualityComplete}}
	wantAssignments := append([]Assignment(nil), assignments...)
	wantAggregates := append([]SourceAggregate(nil), aggregates...)

	if _, err := Aggregate([]Bucket{{Start: start, End: end}}, assignments, aggregates); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(assignments, wantAssignments) || !reflect.DeepEqual(aggregates, wantAggregates) {
		t.Fatal("Aggregate mutated its input slices")
	}
}

func assertRoomIntervalEnergy(t *testing.T, interval RoomInterval, roomID int64, want string, quality Quality) {
	t.Helper()
	if interval.RoomID != roomID || interval.EnergyKWh == nil || !interval.EnergyKWh.Equal(decimal.RequireFromString(want)) || interval.Quality != quality {
		t.Fatalf("room interval = %+v, want room=%d energy=%s quality=%s", interval, roomID, want, quality)
	}
}

func decimalPointer(value string) *decimal.Decimal {
	parsed := decimal.RequireFromString(value)
	return &parsed
}

func timePointer(value time.Time) *time.Time { return &value }
