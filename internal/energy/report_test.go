package energy

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"github.com/filipegalo/shelly-inventory/internal/database"
	"github.com/filipegalo/shelly-inventory/internal/shelly"
	"github.com/shopspring/decimal"
)

func TestBuildReportUsesPricingEffectiveAtEachInterval(t *testing.T) {
	t.Parallel()
	store, err := database.Open(filepath.Join(t.TempDir(), "energy-report.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ctx := context.Background()
	network, err := store.SaveNetwork(ctx, database.Network{Name: "Energy", CIDR: "192.168.40.0/30", Enabled: true, ScanIntervalSeconds: 300, ScanConcurrency: 2, ConnectionTimeoutMS: 100, RequestTimeoutMS: 200})
	if err != nil {
		t.Fatal(err)
	}
	created, err := store.UpsertDevice(ctx, network.ID, "192.168.40.1", shelly.DeviceInfo{ShellyDeviceID: "meter", Name: "Main meter", Model: "SPEM-003CEBEU", RawDeviceInfo: json.RawMessage(`{"id":"meter"}`)}, false, "")
	if err != nil {
		t.Fatal(err)
	}
	config, _ := store.GetEnergyConfig(ctx)
	config.DeviceID, config.ComponentKey, config.ComponentType = created.Device.ID, "em:0", "em"
	if _, err := store.SaveEnergyConfig(ctx, config); err != nil {
		t.Fatal(err)
	}
	start := time.Now().UTC().Add(time.Hour).Truncate(time.Hour)
	for index, energyWh := range []float64{1000, 1000} {
		intervalStart := start.Add(time.Duration(index) * time.Hour)
		if _, err := store.UpsertEnergyAggregate(ctx, database.EnergyAggregate{IntervalType: "hour", IntervalStart: intervalStart, IntervalEnd: intervalStart.Add(time.Hour), DeviceID: config.DeviceID, ComponentKey: config.ComponentKey, EnergyConsumedWh: energyWh, SampleCount: 60, CalculationQuality: QualityMeasured}); err != nil {
			t.Fatal(err)
		}
	}
	changed := database.DefaultPricingVersion()
	changed.EnergyPricePerKWhExcludingVAT = "0.25"
	changed.PriceEffectiveFrom = start.Add(time.Hour)
	if _, err := store.CreatePricingVersion(ctx, changed); err != nil {
		t.Fatal(err)
	}
	report, err := BuildReport(ctx, store, config, start, start.Add(2*time.Hour), "hour")
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Points) != 2 || !report.Consumption.Equal(decimalFromString(t, "2")) {
		t.Fatalf("report = %+v", report)
	}
	if report.Points[0].CostEUR == nil || report.Points[1].CostEUR == nil || *report.Points[0].CostEUR == *report.Points[1].CostEUR {
		t.Fatalf("effective pricing did not change interval costs: %+v", report.Points)
	}
}

func TestPriceSegmentsUsesUnsortedVersionHistoryAndExactBoundaries(t *testing.T) {
	t.Parallel()
	start := time.Date(2026, time.July, 12, 10, 0, 0, 0, time.UTC)
	end := start.Add(time.Hour)
	versions := []database.PricingVersion{
		{ID: 4, PriceEffectiveFrom: end, EnergyPricePerKWhExcludingVAT: "4"},
		{ID: 3, PriceEffectiveFrom: start.Add(30 * time.Minute), EnergyPricePerKWhExcludingVAT: "3"},
		{ID: 1, PriceEffectiveFrom: start.Add(-time.Hour), EnergyPricePerKWhExcludingVAT: "1"},
		{ID: 2, PriceEffectiveFrom: start, EnergyPricePerKWhExcludingVAT: "2"},
	}
	segments, err := priceSegments(start, end, versions)
	if err != nil {
		t.Fatal(err)
	}
	if len(segments) != 2 {
		t.Fatalf("segment count = %d, want 2: %+v", len(segments), segments)
	}
	if segments[0].Pricing.ID != 2 || !segments[0].Start.Equal(start) || !segments[0].End.Equal(start.Add(30*time.Minute)) {
		t.Fatalf("first segment = %+v", segments[0])
	}
	if segments[1].Pricing.ID != 3 || !segments[1].Start.Equal(start.Add(30*time.Minute)) || !segments[1].End.Equal(end) {
		t.Fatalf("second segment = %+v", segments[1])
	}
	if _, err := priceSegments(start, end, []database.PricingVersion{{ID: 5, PriceEffectiveFrom: start.Add(time.Minute)}}); err == nil {
		t.Fatal("priceSegments accepted history without a version effective at range start")
	}

	selected, found := pricingAt([]database.PricingVersion{
		{ID: 10, PriceEffectiveFrom: start},
		{ID: 11, PriceEffectiveFrom: start},
	}, start)
	if !found || selected.ID != 11 {
		t.Fatalf("pricingAt tie-break = %+v found=%v, want highest ID", selected, found)
	}
}

func TestPricingConfigRejectsUnsafeOrInvalidStoredValues(t *testing.T) {
	t.Parallel()
	valid := database.DefaultPricingVersion()
	if _, err := PricingConfig(valid); err != nil {
		t.Fatalf("default pricing is invalid: %v", err)
	}

	tests := []struct {
		name   string
		change func(database.PricingVersion) database.PricingVersion
	}{
		{
			name: "exponent notation",
			change: func(value database.PricingVersion) database.PricingVersion {
				value.EnergyPricePerKWhExcludingVAT = "1e1000000"
				return value
			},
		},
		{
			name: "lowercase currency",
			change: func(value database.PricingVersion) database.PricingVersion {
				value.Currency = "eur"
				return value
			},
		},
		{
			name: "VAT over 100 percent",
			change: func(value database.PricingVersion) database.PricingVersion {
				value.StandardVATRate = "1.01"
				return value
			},
		},
		{
			name: "excessive precision",
			change: func(value database.PricingVersion) database.PricingVersion {
				value.ElectricityExciseTaxPerKWh = "0.0000000000000000001"
				return value
			},
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if _, err := PricingConfig(test.change(valid)); err == nil {
				t.Fatal("PricingConfig accepted invalid persisted pricing")
			}
		})
	}
}

func TestBuildReportSplitsOneAggregateAtAPricingChange(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store, energyConfig := reportTestFixture(t)
	start := time.Date(2026, time.July, 12, 10, 0, 0, 0, time.UTC)

	first := zeroTaxPricing("1", start)
	if _, err := store.CreatePricingVersion(ctx, first); err != nil {
		t.Fatal(err)
	}
	second := zeroTaxPricing("3", start.Add(30*time.Minute))
	if _, err := store.CreatePricingVersion(ctx, second); err != nil {
		t.Fatal(err)
	}
	if _, err := store.UpsertEnergyAggregate(ctx, database.EnergyAggregate{
		IntervalType:       "hour",
		IntervalStart:      start,
		IntervalEnd:        start.Add(time.Hour),
		DeviceID:           energyConfig.DeviceID,
		ComponentKey:       energyConfig.ComponentKey,
		EnergyConsumedWh:   1000,
		SampleCount:        60,
		CalculationQuality: QualityMeasured,
	}); err != nil {
		t.Fatal(err)
	}
	for index, energyWh := range []float64{800, 200} {
		minuteStart := start.Add(time.Duration(index) * 30 * time.Minute)
		if _, err := store.UpsertEnergyAggregate(ctx, database.EnergyAggregate{IntervalType: "minute", IntervalStart: minuteStart, IntervalEnd: minuteStart.Add(30 * time.Minute), DeviceID: energyConfig.DeviceID, ComponentKey: energyConfig.ComponentKey, EnergyConsumedWh: energyWh, SampleCount: 1, CalculationQuality: QualityMeasured}); err != nil {
			t.Fatal(err)
		}
	}

	report, err := BuildReport(ctx, store, energyConfig, start, start.Add(time.Hour), "hour")
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Points) != 1 || !report.Consumption.Equal(decimal.NewFromInt(1)) || !report.Cost.TotalIncludingVAT.Equal(decimal.RequireFromString("1.4")) {
		t.Fatalf("split-price report = %+v", report)
	}
	if report.Points[0].CostEUR == nil || *report.Points[0].CostEUR != "1.4000" {
		t.Fatalf("split-price chart point = %+v", report.Points[0])
	}
}

func TestBuildReportCarriesVATAllowanceAcrossIntervals(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store, energyConfig := reportTestFixture(t)
	energyConfig.Timezone = "UTC"
	energyConfig.BillingCycleStartDay = 11
	start := time.Date(2026, time.July, 11, 0, 0, 0, 0, time.UTC)
	version := zeroTaxPricing("1", start)
	version.ReducedVATEnabled = true
	version.ReducedVATRate = "0"
	version.StandardVATRate = "1"
	version.StandardReducedVATLimitKWhPer30Days = "1"
	if _, err := store.CreatePricingVersion(ctx, version); err != nil {
		t.Fatal(err)
	}
	for index := 0; index < 2; index++ {
		intervalStart := start.Add(time.Duration(index) * time.Hour)
		if _, err := store.UpsertEnergyAggregate(ctx, database.EnergyAggregate{
			IntervalType:       "hour",
			IntervalStart:      intervalStart,
			IntervalEnd:        intervalStart.Add(time.Hour),
			DeviceID:           energyConfig.DeviceID,
			ComponentKey:       energyConfig.ComponentKey,
			EnergyConsumedWh:   1000,
			SampleCount:        60,
			CalculationQuality: QualityMeasured,
		}); err != nil {
			t.Fatal(err)
		}
	}

	report, err := BuildReport(ctx, store, energyConfig, start, start.Add(2*time.Hour), "hour")
	if err != nil {
		t.Fatal(err)
	}
	expectedReduced := decimal.NewFromInt(31).DivRound(decimal.NewFromInt(30), 24)
	expectedStandard := decimal.NewFromInt(2).Sub(expectedReduced)
	if !report.Cost.ReducedRateKWh.Equal(expectedReduced) || !report.Cost.StandardRateKWh.Equal(expectedStandard) {
		t.Fatalf("VAT allocation was reapplied per interval: reduced=%s standard=%s", report.Cost.ReducedRateKWh, report.Cost.StandardRateKWh)
	}
	expectedTotal := decimal.NewFromInt(2).Add(expectedStandard)
	if !report.Cost.TotalIncludingVAT.Equal(expectedTotal) {
		t.Fatalf("total including VAT = %s, want %s", report.Cost.TotalIncludingVAT, expectedTotal)
	}
}

func TestBuildReportPreservesMissingAggregateAsNullChartPoint(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store, energyConfig := reportTestFixture(t)
	start := time.Date(2026, time.July, 12, 10, 0, 0, 0, time.UTC)
	if _, err := store.CreatePricingVersion(ctx, zeroTaxPricing("1", start)); err != nil {
		t.Fatal(err)
	}
	if _, err := store.UpsertEnergyAggregate(ctx, database.EnergyAggregate{
		IntervalType:       "hour",
		IntervalStart:      start,
		IntervalEnd:        start.Add(time.Hour),
		DeviceID:           energyConfig.DeviceID,
		ComponentKey:       energyConfig.ComponentKey,
		MissingSampleCount: 6,
		CalculationQuality: QualityPartial,
	}); err != nil {
		t.Fatal(err)
	}
	report, err := BuildReport(ctx, store, energyConfig, start, start.Add(time.Hour), "hour")
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Points) != 1 || report.Points[0].EnergyKWh != nil || report.Points[0].CostEUR != nil || report.Points[0].Quality != QualityPartial {
		t.Fatalf("missing chart point = %+v", report.Points)
	}
}

func TestBuildReportRejectsInvalidRequestsAndMissingPriceHistory(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store, energyConfig := reportTestFixture(t)
	start := time.Date(2026, time.July, 12, 10, 0, 0, 0, time.UTC)

	requestTests := []struct {
		name     string
		config   database.EnergyConfig
		start    time.Time
		end      time.Time
		interval string
	}{
		{name: "unconfigured meter", config: database.EnergyConfig{}, start: start, end: start.Add(time.Hour), interval: "hour"},
		{name: "empty range", config: energyConfig, start: start, end: start, interval: "hour"},
		{name: "range too long", config: energyConfig, start: start, end: start.Add(367 * 24 * time.Hour), interval: "day"},
		{name: "unsupported interval", config: energyConfig, start: start, end: start.Add(time.Hour), interval: "week"},
		{name: "invalid timezone", config: withReportTimezone(energyConfig, "not/a-zone"), start: start, end: start.Add(time.Hour), interval: "hour"},
	}
	for _, test := range requestTests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if _, err := BuildReport(ctx, store, test.config, test.start, test.end, test.interval); err == nil {
				t.Fatal("BuildReport accepted invalid request")
			}
		})
	}

	beforePricing := time.Date(1960, time.January, 1, 0, 0, 0, 0, time.UTC)
	if _, err := store.UpsertEnergyAggregate(ctx, database.EnergyAggregate{
		IntervalType:       "hour",
		IntervalStart:      beforePricing,
		IntervalEnd:        beforePricing.Add(time.Hour),
		DeviceID:           energyConfig.DeviceID,
		ComponentKey:       energyConfig.ComponentKey,
		EnergyConsumedWh:   100,
		SampleCount:        1,
		CalculationQuality: QualityMeasured,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := BuildReport(ctx, store, energyConfig, beforePricing, beforePricing.Add(time.Hour), "hour"); err == nil {
		t.Fatal("BuildReport accepted aggregate history with no applicable pricing version")
	}
}

func decimalFromString(t *testing.T, value string) decimal.Decimal {
	t.Helper()
	result, err := decimal.NewFromString(value)
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func reportTestFixture(t *testing.T) (*database.Store, database.EnergyConfig) {
	t.Helper()
	ctx := context.Background()
	store, err := database.Open(filepath.Join(t.TempDir(), "energy-report-fixture.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	network, err := store.SaveNetwork(ctx, database.Network{Name: "Report test", CIDR: "192.168.90.0/30", Enabled: true, ScanIntervalSeconds: 300, ScanConcurrency: 2, ConnectionTimeoutMS: 100, RequestTimeoutMS: 200})
	if err != nil {
		t.Fatal(err)
	}
	created, err := store.UpsertDevice(ctx, network.ID, "192.168.90.1", shelly.DeviceInfo{ShellyDeviceID: "report-meter", Name: "Report meter", Model: "SPEM-003CEBEU", RawDeviceInfo: json.RawMessage(`{"id":"report-meter"}`)}, false, "")
	if err != nil {
		t.Fatal(err)
	}
	energyConfig, err := store.GetEnergyConfig(ctx)
	if err != nil {
		t.Fatal(err)
	}
	energyConfig.DeviceID = created.Device.ID
	energyConfig.ComponentKey = "em:0"
	energyConfig.ComponentType = "em"
	return store, energyConfig
}

func zeroTaxPricing(price string, effectiveFrom time.Time) database.PricingVersion {
	version := database.DefaultPricingVersion()
	version.EnergyPricePerKWhExcludingVAT = price
	version.ReducedVATRate = "0"
	version.StandardVATRate = "0"
	version.IncludeExciseTax = false
	version.PriceEffectiveFrom = effectiveFrom
	return version
}

func withReportTimezone(value database.EnergyConfig, timezone string) database.EnergyConfig {
	value.Timezone = timezone
	return value
}
