package database

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/filipegalo/shelly-energy-dashboard/internal/shelly"
)

func TestEnergyDefaultsAndMigration(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := openEnergyTestStore(t)

	config, err := store.GetEnergyConfig(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if config.ID != 1 || config.Enabled || config.PollIntervalSeconds != 10 || config.BillingCycleStartDay != 11 || config.Timezone != "Europe/Lisbon" || config.Currency != "EUR" {
		t.Fatalf("unexpected energy defaults: %+v", config)
	}

	pricing, err := store.ActivePricingVersionAt(ctx, time.Now().Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if pricing.EnergyPricePerKWhExcludingVAT != "0.137378" || pricing.ReferencePricePerKWhExcludingVAT != "0.165516" || pricing.ContractedPowerKVA != "5.75" {
		t.Fatalf("unexpected price defaults: %+v", pricing)
	}
	if !pricing.PriceEffectiveFrom.Equal(time.Unix(0, 0).UTC()) {
		t.Fatalf("default pricing must cover first partial interval: effective=%s", pricing.PriceEffectiveFrom)
	}
	if !pricing.ReducedVATEnabled || pricing.ReducedVATRate != "0.06" || pricing.StandardVATRate != "0.23" || pricing.StandardReducedVATLimitKWhPer30Days != "200" {
		t.Fatalf("unexpected VAT defaults: %+v", pricing)
	}
	if pricing.LargeFamilyEnabled || pricing.LargeFamilyReducedVATLimitKWhPer30Days != "300" || pricing.ElectricityExciseTaxPerKWh != "0.001000" || pricing.ElectricityExciseTaxVATRate != "0.23" || !pricing.IncludeExciseTax {
		t.Fatalf("unexpected tax defaults: %+v", pricing)
	}

	var applied int
	if err := store.db.QueryRowContext(ctx, `SELECT count(*) FROM schema_migrations WHERE version=2`).Scan(&applied); err != nil || applied != 1 {
		t.Fatalf("migration v2 marker: count=%d err=%v", applied, err)
	}
}

func TestEnergyConfigSampleAndEventRoundTrips(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := openEnergyTestStore(t)
	deviceID := createEnergyTestDevice(t, store)

	config, err := store.SaveEnergyConfig(ctx, EnergyConfig{
		Enabled:               true,
		DeviceID:              deviceID,
		ComponentKey:          "em:0",
		ComponentType:         "em",
		CapabilitiesJSON:      `{"power":true,"energy":true}`,
		PollIntervalSeconds:   15,
		BillingCycleStartDay:  17,
		Timezone:              "Europe/Lisbon",
		Currency:              "EUR",
		MaximumExpectedPowerW: 13800,
		SampleRetentionDays:   7,
		MinuteRetentionDays:   90,
		HourlyRetentionDays:   730,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !config.Enabled || config.DeviceID != deviceID || config.ComponentKey != "em:0" || config.MaximumExpectedPowerW != 13800 {
		t.Fatalf("config did not round-trip: %+v", config)
	}

	now := time.Now().UTC().Truncate(time.Second)
	power, imported, voltage, factor := 2345.6, 987654.321, 231.2, 0.97
	sample, err := store.InsertEnergySample(ctx, EnergySample{
		DeviceID:              deviceID,
		ComponentKey:          "em:0",
		RecordedAt:            now,
		PowerW:                &power,
		ImportedEnergyTotalWh: &imported,
		VoltageV:              &voltage,
		PowerFactor:           &factor,
		ReadingQuality:        "partial",
	})
	if err != nil {
		t.Fatal(err)
	}
	latest, err := store.LatestEnergySample(ctx, deviceID, "em:0")
	if err != nil {
		t.Fatal(err)
	}
	if latest.ID != sample.ID || latest.PowerW == nil || *latest.PowerW != power || latest.CurrentA != nil || !latest.RecordedAt.Equal(now) {
		t.Fatalf("sample did not round-trip: %+v", latest)
	}

	event, err := store.InsertMeterEvent(ctx, MeterEvent{
		DeviceID:     deviceID,
		ComponentKey: "em:0",
		EventType:    "meter_selected",
		OccurredAt:   now,
		DetailsJSON:  `{"source":"settings"}`,
	})
	if err != nil {
		t.Fatal(err)
	}
	events, err := store.ListMeterEvents(ctx, deviceID, "em:0", 10)
	if err != nil || len(events) != 1 || events[0].ID != event.ID || events[0].DetailsJSON != `{"source":"settings"}` {
		t.Fatalf("events did not round-trip: %+v err=%v", events, err)
	}

	if err := store.DisableEnergyMonitoring(ctx); err != nil {
		t.Fatal(err)
	}
	disabled, err := store.GetEnergyConfig(ctx)
	if err != nil || disabled.Enabled {
		t.Fatalf("disable monitoring: %+v err=%v", disabled, err)
	}
}

func TestPricingVersionsPreserveHistory(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := openEnergyTestStore(t)
	base := time.Now().UTC().Add(time.Hour).Truncate(time.Second)

	first := DefaultPricingVersion()
	first.EnergyPricePerKWhExcludingVAT = "0.140001"
	first.PriceEffectiveFrom = base
	createdFirst, err := store.CreatePricingVersion(ctx, first)
	if err != nil {
		t.Fatal(err)
	}
	second := first
	second.ID = 0
	second.EnergyPricePerKWhExcludingVAT = "0.150002"
	second.PriceEffectiveFrom = base.Add(time.Hour)
	createdSecond, err := store.CreatePricingVersion(ctx, second)
	if err != nil {
		t.Fatal(err)
	}

	active, err := store.ActivePricingVersionAt(ctx, base.Add(30*time.Minute))
	if err != nil || active.ID != createdFirst.ID || active.EnergyPricePerKWhExcludingVAT != "0.140001" {
		t.Fatalf("first active version: %+v err=%v", active, err)
	}
	active, err = store.ActivePricingVersionAt(ctx, base.Add(2*time.Hour))
	if err != nil || active.ID != createdSecond.ID || active.EnergyPricePerKWhExcludingVAT != "0.150002" {
		t.Fatalf("second active version: %+v err=%v", active, err)
	}
	versions, err := store.ListPricingVersions(ctx)
	if err != nil || len(versions) != 3 {
		t.Fatalf("pricing history: %+v err=%v", versions, err)
	}
}

func TestEnergyAggregateUpsertIsUniqueAndQueryable(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := openEnergyTestStore(t)
	deviceID := createEnergyTestDevice(t, store)
	start := time.Now().UTC().Truncate(time.Minute)
	end := start.Add(time.Minute)
	average, minimum, maximum := 1200.0, 800.0, 1700.0

	first, err := store.UpsertEnergyAggregate(ctx, EnergyAggregate{
		IntervalType:       "minute",
		IntervalStart:      start,
		IntervalEnd:        end,
		DeviceID:           deviceID,
		ComponentKey:       "em:0",
		EnergyConsumedWh:   20,
		AveragePowerW:      &average,
		MinimumPowerW:      &minimum,
		MaximumPowerW:      &maximum,
		SampleCount:        5,
		MissingSampleCount: 1,
		CalculationQuality: "partial",
	})
	if err != nil {
		t.Fatal(err)
	}
	average = 1500
	second, err := store.UpsertEnergyAggregate(ctx, EnergyAggregate{
		IntervalType:       "minute",
		IntervalStart:      start,
		IntervalEnd:        end,
		DeviceID:           deviceID,
		ComponentKey:       "em:0",
		EnergyConsumedWh:   25,
		AveragePowerW:      &average,
		MinimumPowerW:      &minimum,
		MaximumPowerW:      &maximum,
		SampleCount:        6,
		MissingSampleCount: 0,
		CalculationQuality: "measured",
	})
	if err != nil {
		t.Fatal(err)
	}
	if first.ID != second.ID {
		t.Fatalf("upsert duplicated aggregate: first=%d second=%d", first.ID, second.ID)
	}

	aggregates, err := store.ListEnergyAggregates(ctx, deviceID, "em:0", start, end, "minute")
	if err != nil || len(aggregates) != 1 || aggregates[0].EnergyConsumedWh != 25 || aggregates[0].AveragePowerW == nil || *aggregates[0].AveragePowerW != 1500 {
		t.Fatalf("aggregates: %+v err=%v", aggregates, err)
	}
	total, err := store.SumEnergyWh(ctx, deviceID, "em:0", start, end)
	if err != nil || total != 25 {
		t.Fatalf("sum: %v err=%v", total, err)
	}
	var count int
	if err := store.db.QueryRowContext(ctx, `SELECT count(*) FROM energy_interval_aggregates`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("aggregate row count=%d err=%v", count, err)
	}
}

func TestPersistEnergyReadingRollsBackSampleAndAggregatesTogether(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := openEnergyTestStore(t)
	deviceID := createEnergyTestDevice(t, store)
	at := time.Now().UTC().Truncate(time.Minute)
	sample := EnergySample{DeviceID: deviceID, ComponentKey: "em:0", RecordedAt: at, ReadingQuality: "measured"}
	aggregates := []EnergyAggregate{
		{IntervalType: "minute", IntervalStart: at, IntervalEnd: at.Add(time.Minute), DeviceID: deviceID, ComponentKey: "em:0", SampleCount: 1},
		{IntervalType: "invalid", IntervalStart: at, IntervalEnd: at.Add(time.Hour), DeviceID: deviceID, ComponentKey: "em:0", SampleCount: 1},
	}
	if _, err := store.PersistEnergyReading(ctx, sample, aggregates); err == nil {
		t.Fatal("expected invalid aggregate to roll back transaction")
	}
	var samples, savedAggregates int
	if err := store.db.QueryRowContext(ctx, `SELECT count(*) FROM energy_samples`).Scan(&samples); err != nil {
		t.Fatal(err)
	}
	if err := store.db.QueryRowContext(ctx, `SELECT count(*) FROM energy_interval_aggregates`).Scan(&savedAggregates); err != nil {
		t.Fatal(err)
	}
	if samples != 0 || savedAggregates != 0 {
		t.Fatalf("partial transaction persisted samples=%d aggregates=%d", samples, savedAggregates)
	}
}

func TestSaveEnergyBillingSettingsIsAtomic(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := openEnergyTestStore(t)
	invalid := DefaultPricingVersion()
	invalid.Currency = "EU"
	invalid.BillingCycleStartDay = 17
	invalid.PriceEffectiveFrom = time.Now().Add(time.Second)
	current, _ := store.GetEnergyConfig(ctx)
	current.BillingCycleStartDay, current.Timezone = 17, "UTC"
	if err := store.SaveEnergyBillingSettings(ctx, current, &invalid); err == nil {
		t.Fatal("expected pricing constraint failure")
	}
	config, err := store.GetEnergyConfig(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if config.BillingCycleStartDay != 11 || config.Timezone != "Europe/Lisbon" {
		t.Fatalf("billing settings changed despite rollback: %+v", config)
	}
}

func openEnergyTestStore(t *testing.T) *Store {
	t.Helper()
	store, err := Open(filepath.Join(t.TempDir(), "energy.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}

func createEnergyTestDevice(t *testing.T, store *Store) int64 {
	t.Helper()
	ctx := context.Background()
	network, err := store.SaveNetwork(ctx, Network{Name: "Energy", CIDR: "192.168.201.0/30", Enabled: true, ScanIntervalSeconds: 300, ScanConcurrency: 2, ConnectionTimeoutMS: 100, RequestTimeoutMS: 200})
	if err != nil {
		t.Fatal(err)
	}
	result, err := store.UpsertDevice(ctx, network.ID, "192.168.201.1", shelly.DeviceInfo{ShellyDeviceID: "energy-meter", Model: "SPEM-003CEBEU", RawDeviceInfo: []byte(`{"id":"energy-meter"}`)}, false, "")
	if err != nil {
		t.Fatal(err)
	}
	return result.Device.ID
}
