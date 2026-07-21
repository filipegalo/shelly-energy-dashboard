package energy

import (
	"context"
	"testing"
	"time"

	"github.com/filipegalo/shelly-energy-dashboard/internal/database"
	roomcore "github.com/filipegalo/shelly-energy-dashboard/internal/energy/rooms"
	"github.com/shopspring/decimal"
)

func TestBuildRoomEnergyReportReconcilesRoomsUnmeteredAndCost(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store, config := reportTestFixture(t)
	config.Timezone = "UTC"
	config.BillingCycleStartDay = 1
	if _, err := store.SaveEnergyConfig(ctx, config); err != nil {
		t.Fatal(err)
	}
	start := time.Date(2026, time.April, 1, 0, 0, 0, 0, time.UTC)
	price := zeroTaxPricing("1", start)
	price.BillingCycleStartDay = 1
	if _, err := store.CreatePricingVersion(ctx, price); err != nil {
		t.Fatal(err)
	}
	rooms := []database.Room{
		createReportRoom(t, store, "Kitchen"),
		createReportRoom(t, store, "Office"),
		createReportRoom(t, store, "Bedroom"),
	}
	for index, energyKWh := range []float64{8, 4, 3} {
		component := "switch:" + string(rune('0'+index))
		createReportAssignment(t, store, rooms[index].ID, config.DeviceID, component, database.RoomEnergyRolePrimary, 0, start)
		upsertReportEnergy(t, store, config.DeviceID, component, start, time.Hour, energyKWh*1000)
	}
	upsertReportEnergy(t, store, config.DeviceID, config.ComponentKey, start, time.Hour, 20_000)

	report, err := BuildRoomEnergyReport(ctx, store, config, start, start.Add(time.Hour), "hour")
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Rooms) != 3 || len(report.Reconciliation) != 1 {
		t.Fatalf("room report = %+v", report)
	}
	assertSeriesTotal(t, report.Rooms[0], "8", "8")
	assertSeriesTotal(t, report.Rooms[1], "4", "4")
	assertSeriesTotal(t, report.Rooms[2], "3", "3")
	assertSeriesTotal(t, report.Unmetered, "5", "5")
	if report.Reconciliation[0].Status != roomcore.ReconciliationHealthy || report.Reconciliation[0].CoverageRatio == nil || !report.Reconciliation[0].CoverageRatio.Equal(decimal.RequireFromString("0.75")) {
		t.Fatalf("reconciliation = %+v", report.Reconciliation[0])
	}
	if !report.TotalAllocatedCost.Equal(decimal.NewFromInt(20)) || !report.TotalAllocatedCost.Equal(report.WholeHouse.Cost.TotalIncludingVAT.Round(2)) {
		t.Fatalf("allocated=%s whole=%s", report.TotalAllocatedCost, report.WholeHouse.Cost.TotalIncludingVAT)
	}
}

func TestBuildRoomEnergyReportDoesNotDoubleCountSubmeters(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store, config := reportTestFixture(t)
	config.Timezone = "UTC"
	start := time.Date(2026, time.July, 15, 10, 0, 0, 0, time.UTC)
	room := createReportRoom(t, store, "Kitchen")
	parent := createReportAssignment(t, store, room.ID, config.DeviceID, "em:1", database.RoomEnergyRolePrimary, 0, start)
	createReportAssignment(t, store, room.ID, config.DeviceID, "switch:0", database.RoomEnergyRoleSubmeter, parent.ID, start)
	createReportAssignment(t, store, room.ID, config.DeviceID, "switch:1", database.RoomEnergyRoleSubmeter, parent.ID, start)
	upsertReportEnergy(t, store, config.DeviceID, config.ComponentKey, start, time.Hour, 12_000)
	upsertReportEnergy(t, store, config.DeviceID, "em:1", start, time.Hour, 10_000)
	upsertReportEnergy(t, store, config.DeviceID, "switch:0", start, time.Hour, 3_000)
	upsertReportEnergy(t, store, config.DeviceID, "switch:1", start, time.Hour, 2_000)

	report, err := BuildRoomEnergyReport(ctx, store, config, start, start.Add(time.Hour), "hour")
	if err != nil {
		t.Fatal(err)
	}
	assertSeriesTotal(t, report.Rooms[0], "10", report.Rooms[0].AllocatedCost.String())
	if len(report.Rooms[0].Points) != 1 || !report.Rooms[0].Points[0].SubmeterEnergyKWh.Equal(decimal.NewFromInt(5)) || !report.Rooms[0].Points[0].OtherOnPrimaryKWh.Equal(decimal.NewFromInt(5)) {
		t.Fatalf("submeter breakdown = %+v", report.Rooms[0].Points)
	}
	if report.Reconciliation[0].RoomEnergyKWh.Equal(decimal.NewFromInt(15)) {
		t.Fatalf("submeters were added to room reconciliation: %+v", report.Reconciliation[0])
	}
}

func TestBuildRoomEnergyReportMarksReconciliationPartialWhenPrimaryIsMissing(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store, config := reportTestFixture(t)
	config.Timezone = "UTC"
	start := time.Date(2026, time.July, 15, 10, 0, 0, 0, time.UTC)
	room := createReportRoom(t, store, "Kitchen")
	createReportAssignment(t, store, room.ID, config.DeviceID, "switch:0", database.RoomEnergyRolePrimary, 0, start)
	createReportAssignment(t, store, room.ID, config.DeviceID, "switch:1", database.RoomEnergyRolePrimary, 0, start)
	upsertReportEnergy(t, store, config.DeviceID, config.ComponentKey, start, time.Hour, 20_000)
	upsertReportEnergy(t, store, config.DeviceID, "switch:0", start, time.Hour, 5_000)

	report, err := BuildRoomEnergyReport(ctx, store, config, start, start.Add(time.Hour), "hour")
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Rooms) != 1 || report.Rooms[0].Quality != roomcore.QualityPartial {
		t.Fatalf("partial room = %+v", report.Rooms)
	}
	if len(report.Reconciliation) != 1 || report.Reconciliation[0].Quality != roomcore.QualityPartial {
		t.Fatalf("partial reconciliation = %+v", report.Reconciliation)
	}
	if report.Unmetered.Quality != roomcore.QualityPartial || report.Unmetered.EnergyKWh == nil || !report.Unmetered.EnergyKWh.Equal(decimal.NewFromInt(15)) {
		t.Fatalf("partial unmetered = %+v", report.Unmetered)
	}
}

func TestBuildRoomEnergyReportUsesEffectiveAssignmentAtConsumptionTime(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store, config := reportTestFixture(t)
	config.Timezone = "UTC"
	start := time.Date(2026, time.July, 15, 10, 0, 0, 0, time.UTC)
	boundary := start.Add(30 * time.Minute)
	office := createReportRoom(t, store, "Office")
	bedroom := createReportRoom(t, store, "Bedroom")
	assignment := createReportAssignment(t, store, office.ID, config.DeviceID, "switch:0", database.RoomEnergyRolePrimary, 0, start)
	if _, err := store.MoveRoomEnergyAssignment(ctx, assignment.ID, bedroom.ID, boundary); err != nil {
		t.Fatal(err)
	}
	upsertReportEnergy(t, store, config.DeviceID, config.ComponentKey, start, time.Hour, 100)
	upsertReportEnergy(t, store, config.DeviceID, "switch:0", start, time.Hour, 60)

	report, err := BuildRoomEnergyReport(ctx, store, config, start, start.Add(time.Hour), "hour")
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Rooms) != 2 {
		t.Fatalf("room count = %d: %+v", len(report.Rooms), report.Rooms)
	}
	assertSeriesEnergy(t, report.Rooms[0], "0.03")
	assertSeriesEnergy(t, report.Rooms[1], "0.03")
	if report.Rooms[0].Points[0].Quality != roomcore.QualityEstimated || report.Rooms[1].Points[0].Quality != roomcore.QualityEstimated {
		t.Fatalf("boundary split quality = %+v %+v", report.Rooms[0].Points[0], report.Rooms[1].Points[0])
	}
}

func TestBuildRoomEnergyReportUsesRetainedMinutesAtHourlyMoveBoundary(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store, config := reportTestFixture(t)
	config.Timezone = "UTC"
	start := time.Date(2026, time.July, 15, 10, 0, 0, 0, time.UTC)
	boundary := start.Add(30 * time.Minute)
	office := createReportRoom(t, store, "Office")
	bedroom := createReportRoom(t, store, "Bedroom")
	assignment := createReportAssignment(t, store, office.ID, config.DeviceID, "switch:0", database.RoomEnergyRolePrimary, 0, start)
	if _, err := store.MoveRoomEnergyAssignment(ctx, assignment.ID, bedroom.ID, boundary); err != nil {
		t.Fatal(err)
	}
	upsertReportEnergy(t, store, config.DeviceID, config.ComponentKey, start, time.Hour, 100)
	upsertReportEnergy(t, store, config.DeviceID, "switch:0", start, time.Hour, 60)
	for minute := 0; minute < 60; minute++ {
		minuteStart := start.Add(time.Duration(minute) * time.Minute)
		if _, err := store.UpsertEnergyAggregate(ctx, database.EnergyAggregate{IntervalType: "minute", IntervalStart: minuteStart, IntervalEnd: minuteStart.Add(time.Minute), DeviceID: config.DeviceID, ComponentKey: "switch:0", EnergyConsumedWh: 1, AveragePowerW: floatTestValue(60), MaximumPowerW: floatTestValue(60), SampleCount: 6, CalculationQuality: QualityMeasured}); err != nil {
			t.Fatal(err)
		}
	}

	report, err := BuildRoomEnergyReport(ctx, store, config, start, start.Add(time.Hour), "hour")
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Rooms) != 2 {
		t.Fatalf("room count = %d: %+v", len(report.Rooms), report.Rooms)
	}
	assertSeriesEnergy(t, report.Rooms[0], "0.03")
	assertSeriesEnergy(t, report.Rooms[1], "0.03")
	if report.Rooms[0].Points[0].Quality != roomcore.QualityComplete || report.Rooms[1].Points[0].Quality != roomcore.QualityComplete {
		t.Fatalf("retained-minute quality = %+v %+v", report.Rooms[0].Points[0], report.Rooms[1].Points[0])
	}
}

func TestBuildRoomEnergyReportCalculatesVATOnceThenAllocates(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store, config := reportTestFixture(t)
	config.Timezone = "UTC"
	config.BillingCycleStartDay = 1
	if _, err := store.SaveEnergyConfig(ctx, config); err != nil {
		t.Fatal(err)
	}
	start := time.Date(2026, time.April, 1, 0, 0, 0, 0, time.UTC)
	price := zeroTaxPricing("1", start)
	price.BillingCycleStartDay = 1
	price.ReducedVATEnabled = true
	price.ReducedVATRate = "0"
	price.StandardVATRate = "1"
	price.StandardReducedVATLimitKWhPer30Days = "200"
	if _, err := store.CreatePricingVersion(ctx, price); err != nil {
		t.Fatal(err)
	}
	roomA := createReportRoom(t, store, "Room A")
	roomB := createReportRoom(t, store, "Room B")
	createReportAssignment(t, store, roomA.ID, config.DeviceID, "switch:0", database.RoomEnergyRolePrimary, 0, start)
	createReportAssignment(t, store, roomB.ID, config.DeviceID, "switch:1", database.RoomEnergyRolePrimary, 0, start)
	upsertReportEnergy(t, store, config.DeviceID, config.ComponentKey, start, 24*time.Hour, 300_000)
	upsertReportEnergy(t, store, config.DeviceID, "switch:0", start, 24*time.Hour, 150_000)
	upsertReportEnergy(t, store, config.DeviceID, "switch:1", start, 24*time.Hour, 100_000)

	report, err := BuildRoomEnergyReport(ctx, store, config, start, start.Add(24*time.Hour), "day")
	if err != nil {
		t.Fatal(err)
	}
	if !report.WholeHouse.Cost.ReducedRateKWh.Equal(decimal.NewFromInt(200)) || !report.WholeHouse.Cost.StandardRateKWh.Equal(decimal.NewFromInt(100)) || !report.WholeHouse.Cost.TotalIncludingVAT.Equal(decimal.NewFromInt(400)) {
		t.Fatalf("whole-house VAT breakdown = %+v", report.WholeHouse.Cost)
	}
	assertSeriesTotal(t, report.Rooms[0], "150", "200")
	assertSeriesTotal(t, report.Rooms[1], "100", "133.33")
	assertSeriesTotal(t, report.Unmetered, "50", "66.67")
	if !report.TotalAllocatedCost.Equal(decimal.NewFromInt(400)) {
		t.Fatalf("allocated total = %s", report.TotalAllocatedCost)
	}
}

func TestBuildRoomEnergyReportAllocatesEachPriceSegmentUsingItsRoomMix(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store, config := reportTestFixture(t)
	config.Timezone = "UTC"
	start := time.Date(2026, time.April, 1, 10, 0, 0, 0, time.UTC)
	boundary := start.Add(30 * time.Minute)
	firstPrice := zeroTaxPricing("1", start)
	secondPrice := zeroTaxPricing("2", boundary)
	if _, err := store.CreatePricingVersion(ctx, firstPrice); err != nil {
		t.Fatal(err)
	}
	if _, err := store.CreatePricingVersion(ctx, secondPrice); err != nil {
		t.Fatal(err)
	}
	roomA := createReportRoom(t, store, "Room A")
	roomB := createReportRoom(t, store, "Room B")
	createReportAssignment(t, store, roomA.ID, config.DeviceID, "switch:0", database.RoomEnergyRolePrimary, 0, start)
	createReportAssignment(t, store, roomB.ID, config.DeviceID, "switch:1", database.RoomEnergyRolePrimary, 0, start)
	upsertReportEnergy(t, store, config.DeviceID, config.ComponentKey, start, time.Hour, 12_000)
	upsertReportEnergy(t, store, config.DeviceID, "switch:0", start, time.Hour, 6_000)
	upsertReportEnergy(t, store, config.DeviceID, "switch:1", start, time.Hour, 6_000)
	for minute := 0; minute < 60; minute++ {
		minuteStart := start.Add(time.Duration(minute) * time.Minute)
		upsertReportMinute(t, store, config.DeviceID, config.ComponentKey, minuteStart, 200)
		roomAEnergy, roomBEnergy := 0.0, 0.0
		if minute < 30 {
			roomAEnergy = 200
		} else {
			roomBEnergy = 200
		}
		upsertReportMinute(t, store, config.DeviceID, "switch:0", minuteStart, roomAEnergy)
		upsertReportMinute(t, store, config.DeviceID, "switch:1", minuteStart, roomBEnergy)
	}

	report, err := BuildRoomEnergyReport(ctx, store, config, start, start.Add(time.Hour), "hour")
	if err != nil {
		t.Fatal(err)
	}
	assertSeriesTotal(t, report.Rooms[0], "6", "6")
	assertSeriesTotal(t, report.Rooms[1], "6", "12")
	if !report.TotalAllocatedCost.Equal(decimal.NewFromInt(18)) {
		t.Fatalf("segmented allocation total = %s", report.TotalAllocatedCost)
	}
}

func createReportRoom(t *testing.T, store *database.Store, name string) database.Room {
	t.Helper()
	room, err := store.SaveRoom(context.Background(), database.Room{Name: name, Icon: "home", SortOrder: 10})
	if err != nil {
		t.Fatal(err)
	}
	return room
}

func createReportAssignment(t *testing.T, store *database.Store, roomID, deviceID int64, component, role string, parentID int64, start time.Time) database.RoomEnergyAssignment {
	t.Helper()
	assignment, err := store.CreateRoomEnergyAssignment(context.Background(), database.RoomEnergyAssignment{RoomID: roomID, DeviceID: deviceID, ComponentKey: component, ComponentType: component[:len(component)-2], DisplayName: component, Enabled: true, AggregationRole: role, ParentAssignmentID: parentID, EffectiveFrom: start})
	if err != nil {
		t.Fatal(err)
	}
	return assignment
}

func upsertReportEnergy(t *testing.T, store *database.Store, deviceID int64, component string, start time.Time, duration time.Duration, energyWh float64) {
	t.Helper()
	interval := "hour"
	if duration >= 24*time.Hour {
		interval = "day"
	}
	average := energyWh / duration.Hours()
	if _, err := store.UpsertEnergyAggregate(context.Background(), database.EnergyAggregate{IntervalType: interval, IntervalStart: start, IntervalEnd: start.Add(duration), DeviceID: deviceID, ComponentKey: component, EnergyConsumedWh: energyWh, AveragePowerW: &average, MaximumPowerW: &average, SampleCount: max(1, int(duration.Minutes())), CalculationQuality: QualityMeasured}); err != nil {
		t.Fatal(err)
	}
}

func upsertReportMinute(t *testing.T, store *database.Store, deviceID int64, component string, start time.Time, energyWh float64) {
	t.Helper()
	average := energyWh * 60
	if _, err := store.UpsertEnergyAggregate(context.Background(), database.EnergyAggregate{IntervalType: "minute", IntervalStart: start, IntervalEnd: start.Add(time.Minute), DeviceID: deviceID, ComponentKey: component, EnergyConsumedWh: energyWh, AveragePowerW: &average, MaximumPowerW: &average, SampleCount: 1, CalculationQuality: QualityMeasured}); err != nil {
		t.Fatal(err)
	}
}

func floatTestValue(value float64) *float64 { return &value }

func assertSeriesEnergy(t *testing.T, series RoomEnergySeries, want string) {
	t.Helper()
	if series.EnergyKWh == nil || !series.EnergyKWh.Equal(decimal.RequireFromString(want)) {
		t.Fatalf("series %s energy = %v, want %s", series.Name, series.EnergyKWh, want)
	}
}

func assertSeriesTotal(t *testing.T, series RoomEnergySeries, energy, cost string) {
	t.Helper()
	assertSeriesEnergy(t, series, energy)
	if !series.AllocatedCost.Equal(decimal.RequireFromString(cost)) {
		t.Fatalf("series %s allocated cost = %s, want %s", series.Name, series.AllocatedCost, cost)
	}
}
