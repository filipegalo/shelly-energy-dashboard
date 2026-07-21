package database

import (
	"context"
	"testing"
	"time"
)

func TestListEnergyAggregatesForSourcesIsBatchedAndUsesOverlap(t *testing.T) {
	t.Parallel()
	store := openEnergyTestStore(t)
	ctx := context.Background()
	deviceID := createEnergyTestDevice(t, store)
	start := time.Date(2026, time.July, 21, 10, 0, 0, 0, time.UTC)

	for _, aggregate := range []EnergyAggregate{
		{IntervalType: "hour", IntervalStart: start, IntervalEnd: start.Add(time.Hour), DeviceID: deviceID, ComponentKey: "switch:0", EnergyConsumedWh: 100, SampleCount: 6, CalculationQuality: "measured"},
		{IntervalType: "hour", IntervalStart: start, IntervalEnd: start.Add(time.Hour), DeviceID: deviceID, ComponentKey: "switch:1", EnergyConsumedWh: 200, SampleCount: 6, CalculationQuality: "measured"},
		{IntervalType: "hour", IntervalStart: start, IntervalEnd: start.Add(time.Hour), DeviceID: deviceID, ComponentKey: "switch:2", EnergyConsumedWh: 300, SampleCount: 6, CalculationQuality: "measured"},
	} {
		if _, err := store.UpsertEnergyAggregate(ctx, aggregate); err != nil {
			t.Fatal(err)
		}
	}

	// The requested range begins inside the stored bucket. Overlap semantics
	// must retain it so an assignment boundary can split the bucket later.
	items, err := store.ListEnergyAggregatesForSources(ctx, []EnergySourceRef{
		{DeviceID: deviceID, ComponentKey: "switch:0"},
		{DeviceID: deviceID, ComponentKey: "switch:1"},
	}, start.Add(30*time.Minute), start.Add(90*time.Minute), "hour")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 || items[0].ComponentKey != "switch:0" || items[1].ComponentKey != "switch:1" {
		t.Fatalf("batched aggregates = %+v", items)
	}
	if items[0].EnergyConsumedWh != 100 || items[1].EnergyConsumedWh != 200 {
		t.Fatalf("batched energy = %+v", items)
	}
}

func TestListLatestEnergySamplesForSourcesReturnsOnePerSource(t *testing.T) {
	t.Parallel()
	store := openEnergyTestStore(t)
	ctx := context.Background()
	deviceID := createEnergyTestDevice(t, store)
	start := time.Date(2026, time.July, 21, 10, 0, 0, 0, time.UTC)

	for index, sample := range []EnergySample{
		{DeviceID: deviceID, ComponentKey: "switch:0", RecordedAt: start, PowerW: floatTestPointer(100)},
		{DeviceID: deviceID, ComponentKey: "switch:0", RecordedAt: start.Add(time.Minute), PowerW: floatTestPointer(150)},
		{DeviceID: deviceID, ComponentKey: "switch:1", RecordedAt: start, PowerW: floatTestPointer(200)},
	} {
		sample.ReadingQuality = "measured"
		if _, err := store.InsertEnergySample(ctx, sample); err != nil {
			t.Fatalf("insert sample %d: %v", index, err)
		}
	}

	requested := []EnergySourceRef{{DeviceID: deviceID, ComponentKey: "switch:0"}, {DeviceID: deviceID, ComponentKey: "switch:1"}, {DeviceID: deviceID, ComponentKey: "switch:2"}}
	latest, err := store.ListLatestEnergySamplesForSources(ctx, requested)
	if err != nil {
		t.Fatal(err)
	}
	if len(latest) != 2 {
		t.Fatalf("latest sample count = %d: %+v", len(latest), latest)
	}
	first := latest[EnergySourceRef{DeviceID: deviceID, ComponentKey: "switch:0"}]
	if first.PowerW == nil || *first.PowerW != 150 || !first.RecordedAt.Equal(start.Add(time.Minute)) {
		t.Fatalf("latest switch:0 = %+v", first)
	}
	if _, found := latest[EnergySourceRef{DeviceID: deviceID, ComponentKey: "switch:2"}]; found {
		t.Fatal("missing source was returned as a zero-valued sample")
	}
}

func floatTestPointer(value float64) *float64 { return &value }
