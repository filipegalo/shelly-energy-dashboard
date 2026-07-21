package energy

import (
	"context"
	"net/netip"
	"sync"
	"testing"
	"time"

	"github.com/filipegalo/shelly-energy-dashboard/internal/database"
	"github.com/filipegalo/shelly-energy-dashboard/internal/shelly"
)

func TestSelectCollectorSourcesIncludesRoomsAndDeduplicatesMain(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.July, 21, 20, 0, 0, 0, time.UTC)
	future := now.Add(time.Hour)
	mainConfig := database.EnergyConfig{Enabled: true, DeviceID: 10, ComponentKey: "em:0", ComponentType: "em", PollIntervalSeconds: 10, BillingCycleStartDay: 11, Timezone: "Europe/Lisbon", MaximumExpectedPowerW: 30000}
	assignments := []database.RoomEnergyAssignment{
		{ID: 1, RoomID: 2, DeviceID: 10, ComponentKey: "em:0", ComponentType: "em", DisplayName: "Deliberate main reuse", Enabled: true, AggregationRole: database.RoomEnergyRolePrimary, PollIntervalSeconds: 15, MainMeterAcknowledged: true, EffectiveFrom: now.Add(-time.Hour)},
		{ID: 2, RoomID: 3, DeviceID: 10, ComponentKey: "switch:1", ComponentType: "switch", DisplayName: "Office", Enabled: true, AggregationRole: database.RoomEnergyRoleSubmeter, PollIntervalSeconds: 5, EffectiveFrom: now.Add(-time.Hour)},
		{ID: 3, RoomID: 4, DeviceID: 10, ComponentKey: "switch:2", ComponentType: "switch", Enabled: true, AggregationRole: database.RoomEnergyRoleExcluded, EffectiveFrom: now.Add(-time.Hour)},
		{ID: 4, RoomID: 5, DeviceID: 10, ComponentKey: "switch:3", ComponentType: "switch", Enabled: true, AggregationRole: database.RoomEnergyRolePrimary, EffectiveFrom: future},
	}

	selection, err := selectCollectorSources(mainConfig, database.RoomEnergyConfig{}, assignments, now)
	if err != nil {
		t.Fatal(err)
	}
	if len(selection.Sources) != 2 {
		t.Fatalf("selected sources = %+v", selection.Sources)
	}
	main := selection.Sources[SourceKey{DeviceID: 10, ComponentKey: "em:0"}]
	if !main.MainMeter || main.PollIntervalSeconds != 10 || len(main.Assignments) != 1 || main.Assignments[0].RoomID != 2 {
		t.Fatalf("deduplicated main source = %+v", main)
	}
	office := selection.Sources[SourceKey{DeviceID: 10, ComponentKey: "switch:1"}]
	if office.MainMeter || office.PollIntervalSeconds != 5 || len(office.Assignments) != 1 {
		t.Fatalf("room source = %+v", office)
	}
	if !selection.NextBoundary.Equal(future) {
		t.Fatalf("next boundary = %s, want %s", selection.NextBoundary, future)
	}

	diagnostic, err := selectCollectorSources(mainConfig, database.RoomEnergyConfig{DiagnosticCollection: true}, assignments, now)
	if err != nil {
		t.Fatal(err)
	}
	if _, found := diagnostic.Sources[SourceKey{DeviceID: 10, ComponentKey: "switch:2"}]; !found {
		t.Fatal("diagnostic collection did not include the excluded source")
	}
}

func TestManagerWorkerDiffAndFutureBoundaryLifecycle(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store, deviceID := managerTestStoreAndDevice(t, "192.168.89.2")
	config, err := store.GetEnergyConfig(ctx)
	if err != nil {
		t.Fatal(err)
	}
	config.Enabled = true
	config.DeviceID = deviceID
	config.ComponentKey = "em:0"
	config.ComponentType = "em"
	config.PollIntervalSeconds = 5
	if _, err := store.SaveEnergyConfig(ctx, config); err != nil {
		t.Fatal(err)
	}
	room, err := store.SaveRoom(ctx, database.Room{Name: "Office", Icon: "desk", SortOrder: 10})
	if err != nil {
		t.Fatal(err)
	}
	ends := time.Now().UTC().Add(750 * time.Millisecond)
	if _, err := store.CreateRoomEnergyAssignment(ctx, database.RoomEnergyAssignment{RoomID: room.ID, DeviceID: deviceID, ComponentKey: "switch:1", ComponentType: "switch", DisplayName: "Office channel", Enabled: true, AggregationRole: database.RoomEnergyRolePrimary, EffectiveFrom: time.Now().UTC().Add(-time.Second), EffectiveTo: &ends}); err != nil {
		t.Fatal(err)
	}

	client := newSupervisorEnergyClient()
	manager := newTestManager(store)
	manager.newClient = func() energyClient { return client }
	var hookMu sync.Mutex
	starts, stops := map[SourceKey]int{}, map[SourceKey]int{}
	manager.onWorkerStart = func(key SourceKey) { hookMu.Lock(); starts[key]++; hookMu.Unlock() }
	manager.onWorkerStop = func(key SourceKey) { hookMu.Lock(); stops[key]++; hookMu.Unlock() }
	if err := manager.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer manager.Stop()

	waitFor(t, func() bool { return len(manager.RunningSourceKeys()) == 2 })
	if err := manager.Reconfigure(ctx); err != nil {
		t.Fatal(err)
	}
	hookMu.Lock()
	if len(starts) != 2 || starts[SourceKey{DeviceID: deviceID, ComponentKey: "em:0"}] != 1 || starts[SourceKey{DeviceID: deviceID, ComponentKey: "switch:1"}] != 1 {
		t.Fatalf("unchanged reconfigure restarted workers: %+v", starts)
	}
	hookMu.Unlock()

	waitFor(t, func() bool {
		keys := manager.RunningSourceKeys()
		return len(keys) == 1 && keys[0] == (SourceKey{DeviceID: deviceID, ComponentKey: "em:0"})
	})
	hookMu.Lock()
	if stops[SourceKey{DeviceID: deviceID, ComponentKey: "switch:1"}] != 1 {
		t.Fatalf("expired room worker stops = %+v", stops)
	}
	hookMu.Unlock()
	if client.maximumOverlap(SourceKey{DeviceID: deviceID, ComponentKey: "switch:1"}) > 1 {
		t.Fatal("same component was collected concurrently")
	}
}

func TestRoomLiveSubscriptionPublishesImmutableSnapshots(t *testing.T) {
	t.Parallel()
	manager := &Manager{
		subscribers:     map[chan LiveState]struct{}{},
		roomSubscribers: map[chan RoomLiveEvent]struct{}{},
		sourceStates:    map[SourceKey]SourceLiveState{},
	}
	stream, unsubscribe, err := manager.SubscribeRoomEnergy()
	if err != nil {
		t.Fatal(err)
	}
	defer unsubscribe()
	initial := <-stream
	if initial.Type != RoomLiveEventSnapshot {
		t.Fatalf("initial room event = %+v", initial)
	}

	power := 875.0
	source := collectorSource{
		Key:                 SourceKey{DeviceID: 7, ComponentKey: "switch:0"},
		PollIntervalSeconds: 10,
		Assignments:         []RoomSourceAssignment{{AssignmentID: 11, RoomID: 3, DisplayName: "Dishwasher", AggregationRole: database.RoomEnergyRoleSubmeter}},
	}
	manager.publishSource(source, LiveState{Enabled: true, Online: true, DeviceID: 7, ComponentKey: "switch:0", PollIntervalSeconds: 10, Reading: shelly.EnergyReading{Timestamp: time.Now(), PowerW: &power}})
	event := <-stream
	if event.Type != RoomLiveEventReading || event.Source.Key != source.Key || len(event.Snapshot.Sources) != 1 {
		t.Fatalf("room live event = %+v", event)
	}

	snapshot := manager.RoomLiveSnapshot()
	snapshot.Sources[0].Assignments[0].DisplayName = "mutated by caller"
	*snapshot.Sources[0].Live.Reading.PowerW = 0
	fresh := manager.RoomLiveSnapshot()
	if fresh.Sources[0].Assignments[0].DisplayName != "Dishwasher" || fresh.Sources[0].Live.Reading.PowerW == nil || *fresh.Sources[0].Live.Reading.PowerW != power {
		t.Fatalf("manager snapshot was mutable through caller copy: %+v", fresh)
	}
}

type supervisorEnergyClient struct {
	mu         sync.Mutex
	active     map[SourceKey]int
	maximum    map[SourceKey]int
	cumulative map[SourceKey]float64
}

func newSupervisorEnergyClient() *supervisorEnergyClient {
	return &supervisorEnergyClient{active: map[SourceKey]int{}, maximum: map[SourceKey]int{}, cumulative: map[SourceKey]float64{}}
}

func (f *supervisorEnergyClient) Identify(context.Context, netip.Addr) (shelly.DeviceInfo, error) {
	return shelly.DeviceInfo{ShellyDeviceID: "manager-meter"}, nil
}

func (f *supervisorEnergyClient) DiscoverEnergyComponents(context.Context, netip.Addr, *shelly.Credential) ([]shelly.EnergyComponent, error) {
	return nil, nil
}

func (f *supervisorEnergyClient) ReadEnergy(_ context.Context, _ netip.Addr, componentKey string, _ *shelly.Credential) (shelly.EnergyReading, error) {
	key := SourceKey{DeviceID: 0, ComponentKey: componentKey}
	f.mu.Lock()
	f.active[key]++
	if f.active[key] > f.maximum[key] {
		f.maximum[key] = f.active[key]
	}
	f.cumulative[key]++
	energy := f.cumulative[key]
	f.mu.Unlock()
	power := 100.0
	f.mu.Lock()
	f.active[key]--
	f.mu.Unlock()
	return shelly.EnergyReading{Timestamp: time.Now().UTC(), PowerW: &power, ImportedEnergyWh: &energy}, nil
}

func (f *supervisorEnergyClient) maximumOverlap(key SourceKey) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.maximum[SourceKey{ComponentKey: key.ComponentKey}]
}
