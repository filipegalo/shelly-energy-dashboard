package energy

import (
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/filipegalo/shelly-energy-dashboard/internal/database"
	"github.com/filipegalo/shelly-energy-dashboard/internal/shelly"
)

// The fixture spans Lisbon's 2026-10-25 DST fall-back, so the second day has
// 25 hours and the 01:00 local hour occurs twice.
func newExportFixture(t *testing.T) (Export, *time.Location) {
	t.Helper()
	store, err := database.Open(filepath.Join(t.TempDir(), "export.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	ctx := context.Background()
	location, _ := time.LoadLocation("Europe/Lisbon")
	network, err := store.SaveNetwork(ctx, database.Network{Name: "Energy", CIDR: "192.168.40.0/29", Enabled: true, ScanIntervalSeconds: 300, ScanConcurrency: 2, ConnectionTimeoutMS: 100, RequestTimeoutMS: 200})
	if err != nil {
		t.Fatal(err)
	}
	device := func(ip, id, name string) int64 {
		created, err := store.UpsertDevice(ctx, network.ID, ip, shelly.DeviceInfo{ShellyDeviceID: id, Name: name, Model: "S3SW-001P16EU", RawDeviceInfo: json.RawMessage(`{"id":"` + id + `"}`)}, false, "")
		if err != nil {
			t.Fatal(err)
		}
		return created.Device.ID
	}
	mainID := device("192.168.40.1", "meter", "Entrance Power Monitoring")
	evID := device("192.168.40.2", "ev", "Shelly - Garage Car Charger")
	ovenID := device("192.168.40.3", "oven", "Shelly - Kitchen Oven")
	config, _ := store.GetEnergyConfig(ctx)
	config.DeviceID, config.ComponentKey, config.ComponentType, config.Timezone, config.PollIntervalSeconds = mainID, "em1:0", "em1", "Europe/Lisbon", 10
	if config, err = store.SaveEnergyConfig(ctx, config); err != nil {
		t.Fatal(err)
	}
	garage, err := store.SaveRoom(ctx, database.Room{Name: "Garage", Icon: "home", SortOrder: 10})
	if err != nil {
		t.Fatal(err)
	}
	kitchen, err := store.SaveRoom(ctx, database.Room{Name: "Kitchen", Icon: "home", SortOrder: 11})
	if err != nil {
		t.Fatal(err)
	}
	assigned := time.Date(2026, time.October, 1, 0, 0, 0, 0, time.UTC)
	for _, assignment := range []database.RoomEnergyAssignment{
		{RoomID: garage.ID, DeviceID: evID, ComponentKey: "switch:0", ComponentType: "switch"},
		{RoomID: kitchen.ID, DeviceID: ovenID, ComponentKey: "switch:0", ComponentType: "switch"},
		// The main meter assigned to a room must not become a device column.
		{RoomID: kitchen.ID, DeviceID: mainID, ComponentKey: "em1:0", ComponentType: "em1", MainMeterAcknowledged: true},
	} {
		assignment.Enabled, assignment.AggregationRole, assignment.EffectiveFrom = true, database.RoomEnergyRolePrimary, assigned
		if _, err := store.CreateRoomEnergyAssignment(ctx, assignment); err != nil {
			t.Fatal(err)
		}
	}

	power := func(value float64) *float64 { return &value }
	var aggregates []database.EnergyAggregate
	add := func(interval string, deviceID int64, key string, start time.Time, length time.Duration, wh, averageW float64, samples int) {
		aggregates = append(aggregates, database.EnergyAggregate{IntervalType: interval, IntervalStart: start, IntervalEnd: start.Add(length), DeviceID: deviceID, ComponentKey: key, EnergyConsumedWh: wh, AveragePowerW: power(averageW), MinimumPowerW: power(averageW), MaximumPowerW: power(averageW + 1000), SampleCount: samples, CalculationQuality: QualityMeasured})
	}
	// Main meter: first 15 minutes of 25 Oct and both occurrences of 01:00.
	for minute := 0; minute < 15; minute++ {
		add("minute", mainID, "em1:0", time.Date(2026, time.October, 24, 23, minute, 0, 0, time.UTC), time.Minute, 10, 600, 6)
	}
	add("minute", mainID, "em1:0", time.Date(2026, time.October, 25, 0, 0, 0, 0, time.UTC), time.Minute, 7, 420, 6)
	add("minute", mainID, "em1:0", time.Date(2026, time.October, 25, 1, 0, 0, 0, time.UTC), time.Minute, 9, 540, 6)
	// EV: a session crossing midnight, then a second session on 25 Oct.
	for minute := 50; minute < 60; minute++ {
		add("minute", evID, "switch:0", time.Date(2026, time.October, 24, 22, minute, 0, 0, time.UTC), time.Minute, 38, 2300, 2)
	}
	for minute := 0; minute <= 20; minute++ {
		add("minute", evID, "switch:0", time.Date(2026, time.October, 24, 23, minute, 0, 0, time.UTC), time.Minute, 38, 2300, 2)
	}
	for minute := 0; minute < 10; minute++ {
		add("minute", evID, "switch:0", time.Date(2026, time.October, 25, 10, minute, 0, 0, time.UTC), time.Minute, 38, 2300, 2)
	}
	// Hourly data only for 24 Oct (local 03:00 and 12:00 are UTC+1).
	add("hour", mainID, "em1:0", time.Date(2026, time.October, 24, 2, 0, 0, 0, time.UTC), time.Hour, 500, 500, 360)
	add("hour", mainID, "em1:0", time.Date(2026, time.October, 24, 11, 0, 0, 0, time.UTC), time.Hour, 2000, 2000, 360)
	add("hour", evID, "switch:0", time.Date(2026, time.October, 24, 22, 0, 0, 0, time.UTC), time.Hour, 383, 2300, 20)
	add("hour", evID, "switch:0", time.Date(2026, time.October, 24, 13, 0, 0, 0, time.UTC), time.Hour, 1000, 1000, 120)
	add("hour", ovenID, "switch:0", time.Date(2026, time.October, 24, 11, 0, 0, 0, time.UTC), time.Hour, 0, 0, 120)
	add("day", mainID, "em1:0", time.Date(2026, time.October, 23, 23, 0, 0, 0, time.UTC), 24*time.Hour, 2500, 104, 720)
	if err := store.UpsertEnergyAggregates(ctx, aggregates); err != nil {
		t.Fatal(err)
	}

	export, err := NewExport(ctx, store, config, time.Date(2026, time.October, 24, 0, 0, 0, 0, location), time.Date(2026, time.October, 26, 0, 0, 0, 0, location))
	if err != nil {
		t.Fatal(err)
	}
	return export, location
}

func exportRows(t *testing.T, write func(context.Context, *bytes.Buffer) error) []map[string]string {
	t.Helper()
	var buffer bytes.Buffer
	if err := write(context.Background(), &buffer); err != nil {
		t.Fatal(err)
	}
	records, err := csv.NewReader(&buffer).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	rows := make([]map[string]string, 0, len(records)-1)
	for _, record := range records[1:] {
		row := map[string]string{}
		for index, name := range records[0] {
			row[name] = record[index]
		}
		rows = append(rows, row)
	}
	return rows
}

func TestExportDevicesExcludeMainMeterAndIdentifyEV(t *testing.T) {
	t.Parallel()
	export, _ := newExportFixture(t)
	if len(export.Devices) != 2 || export.Devices[0].Column != "device_shelly_garage_car_charger" || export.Devices[1].Column != "device_shelly_kitchen_oven" {
		t.Fatalf("devices = %+v", export.Devices)
	}
	if ev, ok := export.EVDevice(); !ok || ev.Room != "Garage" {
		t.Fatalf("ev = %+v, %v", ev, ok)
	}
	if _, err := NewExport(context.Background(), export.store, export.config, export.Start, export.Start.AddDate(0, 0, MaximumExportDays+1)); err == nil {
		t.Fatal("expected export longer than the maximum to be rejected")
	}
}

func TestIntervalExportUsesRegularLocalGridAcrossDST(t *testing.T) {
	t.Parallel()
	export, _ := newExportFixture(t)
	rows := exportRows(t, func(ctx context.Context, buffer *bytes.Buffer) error { return export.WriteIntervalCSV(ctx, buffer) })
	if len(rows) != 96+100 {
		t.Fatalf("rows = %d, want 96 + 100 for the 25-hour fall-back day", len(rows))
	}
	first := rows[96]
	if first["interval_start"] != "2026-10-25T00:00:00+01:00" || first["household_consumption_wh"] != "150.0" || first["grid_import_wh"] != "150.0" || first["grid_avg_power_w"] != "600.0" || first["grid_max_power_w"] != "1600.0" || first["data_completeness_pct"] != "100.0" || first["household_quality"] != QualityMeasured {
		t.Fatalf("first interval = %+v", first)
	}
	if first["device_shelly_garage_car_charger_wh"] != "570.0" || first["device_shelly_kitchen_oven_wh"] != "" {
		t.Fatalf("device columns = %+v", first)
	}
	summer, winter := rows[96+4], rows[96+8]
	if summer["interval_start"] != "2026-10-25T01:00:00+01:00" || summer["household_consumption_wh"] != "7.0" || winter["interval_start"] != "2026-10-25T01:00:00+00:00" || winter["household_consumption_wh"] != "9.0" {
		t.Fatalf("repeated 01:00 hour = %+v / %+v", summer, winter)
	}
	if missing := rows[96+1]; missing["household_consumption_wh"] != "" || missing["household_quality"] != "missing" || missing["data_completeness_pct"] != "0.0" {
		t.Fatalf("missing interval must stay blank, got %+v", missing)
	}
}

func TestHourlyExportCoversDSTDayFromHourlyData(t *testing.T) {
	t.Parallel()
	export, _ := newExportFixture(t)
	rows := exportRows(t, func(ctx context.Context, buffer *bytes.Buffer) error { return export.WriteHourlyCSV(ctx, buffer) })
	if len(rows) != 24+25 {
		t.Fatalf("rows = %d, want 24 + 25 for the 25-hour fall-back day", len(rows))
	}
	noon := rows[12]
	if noon["interval_start"] != "2026-10-24T12:00:00+01:00" || noon["interval_end"] != "2026-10-24T13:00:00+01:00" || noon["household_consumption_wh"] != "2000.0" || noon["data_completeness_pct"] != "100.0" || noon["device_shelly_kitchen_oven_wh"] != "0.0" || noon["device_shelly_garage_car_charger_wh"] != "" {
		t.Fatalf("noon = %+v", noon)
	}
	if repeated := rows[24+2]; repeated["interval_start"] != "2026-10-25T01:00:00+00:00" || repeated["household_quality"] != "missing" {
		t.Fatalf("second 01:00 hour = %+v", repeated)
	}
}

func TestDailyAndMonthlyExportsSplitDayNightWithoutInventingData(t *testing.T) {
	t.Parallel()
	export, _ := newExportFixture(t)
	daily := exportRows(t, func(ctx context.Context, buffer *bytes.Buffer) error { return export.WriteDailyCSV(ctx, buffer) })
	if len(daily) != 2 {
		t.Fatalf("daily rows = %d", len(daily))
	}
	day := daily[0]
	if day["date"] != "2026-10-24" || day["weekday"] != "Saturday" || day["household_consumption_kwh"] != "2.500" || day["daytime_consumption_kwh"] != "2.000" || day["nighttime_consumption_kwh"] != "0.500" || day["night_min_hourly_avg_power_w"] != "500.0" || day["peak_power_w"] != "3000.0" || day["data_completeness_pct"] != "8.3" {
		t.Fatalf("day = %+v", day)
	}
	if day["ev_charging_kwh"] != "1.383" || day["ev_charging_daytime_kwh"] != "1.000" || day["ev_sessions_started"] != "1" || day["ev_charging_hours"] != "0.17" || day["device_shelly_kitchen_oven_kwh"] != "0.000" {
		t.Fatalf("ev/device = %+v", day)
	}
	next := daily[1]
	if next["household_consumption_kwh"] != "" || next["peak_power_w"] != "" || next["device_shelly_kitchen_oven_kwh"] != "" || next["ev_charging_daytime_kwh"] != "" {
		t.Fatalf("day without hourly data must be blank, got %+v", next)
	}
	if next["ev_sessions_started"] != "1" || next["ev_charging_hours"] != "0.52" {
		t.Fatalf("session crossing midnight must not be counted twice: %+v", next)
	}

	monthly := exportRows(t, func(ctx context.Context, buffer *bytes.Buffer) error { return export.WriteMonthlyCSV(ctx, buffer) })
	if len(monthly) != 1 {
		t.Fatalf("monthly rows = %d", len(monthly))
	}
	month := monthly[0]
	if month["month"] != "2026-10" || month["days_in_period"] != "2" || month["complete_days"] != "0" || month["household_consumption_kwh"] != "2.500" || month["avg_daily_consumption_kwh"] != "" || month["ev_sessions_started"] != "2" || month["peak_power_w"] != "3000.0" {
		t.Fatalf("month = %+v", month)
	}
	if month["ev_charging_daytime_kwh"] != "1.000" || month["device_shelly_garage_car_charger_daytime_kwh"] != "1.000" || month["device_shelly_kitchen_oven_kwh"] != "0.000" || month["device_shelly_kitchen_oven_daytime_kwh"] != "0.000" {
		t.Fatalf("month daytime split = %+v", month)
	}
}

func TestCostExportUsesConfiguredTariff(t *testing.T) {
	t.Parallel()
	export, _ := newExportFixture(t)
	rows := exportRows(t, func(ctx context.Context, buffer *bytes.Buffer) error { return export.WriteCostCSV(ctx, buffer) })
	if len(rows) != 2 || rows[1]["month"] != "TOTAL" {
		t.Fatalf("cost rows = %+v", rows)
	}
	for _, row := range rows {
		// 2.5 kWh × 0.137378 + 6% VAT + 0.001/kWh excise tax + 23% VAT.
		if row["grid_import_kwh"] != "2.500" || row["variable_cost_incl_vat_eur"] != "0.3671" || row["avg_import_price_eur_per_kwh"] != "0.146851" {
			t.Fatalf("cost row = %+v", row)
		}
	}
}

func TestExportReadmeDocumentsLimitations(t *testing.T) {
	t.Parallel()
	export, _ := newExportFixture(t)
	var buffer bytes.Buffer
	if err := export.WriteReadme(context.Background(), &buffer); err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{"Europe/Lisbon", "device_shelly_garage_car_charger", "Grid EXPORT is not available", "EXCLUDED: fixed charges", "0.137378"} {
		if !strings.Contains(buffer.String(), expected) {
			t.Fatalf("README missing %q:\n%s", expected, buffer.String())
		}
	}
}
