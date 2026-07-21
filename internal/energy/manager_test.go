package energy

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/netip"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/filipegalo/shelly-energy-dashboard/internal/config"
	"github.com/filipegalo/shelly-energy-dashboard/internal/credentials"
	"github.com/filipegalo/shelly-energy-dashboard/internal/database"
	"github.com/filipegalo/shelly-energy-dashboard/internal/metrics"
	"github.com/filipegalo/shelly-energy-dashboard/internal/shelly"
)

func TestCollectorValidationAndBackoff(t *testing.T) {
	t.Parallel()
	valid := database.EnergyConfig{DeviceID: 1, ComponentKey: "em:0", PollIntervalSeconds: 10, BillingCycleStartDay: 11, Timezone: "Europe/Lisbon", MaximumExpectedPowerW: 20000}
	if err := validateCollectorConfig(valid); err != nil {
		t.Fatal(err)
	}
	tests := []database.EnergyConfig{
		{ComponentKey: valid.ComponentKey, PollIntervalSeconds: valid.PollIntervalSeconds, BillingCycleStartDay: valid.BillingCycleStartDay, Timezone: valid.Timezone, MaximumExpectedPowerW: valid.MaximumExpectedPowerW},
		withPollInterval(valid, 2),
		withBillingDay(valid, 0),
		withTimezone(valid, "invalid/timezone"),
		withMaximumPower(valid, 0),
	}
	for index, config := range tests {
		if err := validateCollectorConfig(config); err == nil {
			t.Fatalf("invalid collector config %d was accepted", index)
		}
	}
	if got := boundedBackoff(10*time.Second, 1); got != 10*time.Second {
		t.Fatalf("first backoff = %s", got)
	}
	if got := boundedBackoff(time.Minute, 20); got != 5*time.Minute {
		t.Fatalf("capped backoff = %s", got)
	}
}

func TestEnergyReadingValidationAndErrorClassification(t *testing.T) {
	t.Parallel()
	power, imported := 1200.0, 10000.0
	valid := shelly.EnergyReading{Timestamp: time.Now(), PowerW: &power, ImportedEnergyWh: &imported}
	if err := validateEnergyReading(valid, 2000, time.Minute); err != nil {
		t.Fatal(err)
	}
	tooHigh := 4000.0
	invalid := []shelly.EnergyReading{
		{},
		{Timestamp: time.Now()},
		{Timestamp: time.Now().Add(10 * time.Minute), PowerW: &power},
		{Timestamp: time.Now(), PowerW: floatPointer(-1)},
		{Timestamp: time.Now(), PowerW: &tooHigh},
		{Timestamp: time.Now().Add(-2 * time.Minute), PowerW: &power},
	}
	for index, reading := range invalid {
		if err := validateEnergyReading(reading, 2000, time.Minute); err == nil {
			t.Fatalf("invalid reading %d was accepted", index)
		}
	}
	cases := []struct {
		err  error
		want string
	}{
		{shelly.ErrAuthenticationRequired, "authentication"},
		{context.DeadlineExceeded, "timeout"},
		{shelly.ErrNoUsableEnergyData, "invalid_reading"},
		{errors.New("network unavailable"), "request"},
	}
	for _, test := range cases {
		if got := classifyCollectorError(test.err); got != test.want {
			t.Fatalf("classify %v = %s, want %s", test.err, got, test.want)
		}
	}
}

func TestMeterIdentityMustMatchStableInventoryIdentity(t *testing.T) {
	t.Parallel()
	device := database.Device{ShellyDeviceID: "shellypro3em-main", MACAddress: "aabbccddeeff"}
	if err := validateMeterIdentity(device, shelly.DeviceInfo{ShellyDeviceID: "SHELLYPRO3EM-MAIN", MACAddress: "aabbccddeeff"}); err != nil {
		t.Fatal(err)
	}
	if err := validateMeterIdentity(device, shelly.DeviceInfo{ShellyDeviceID: "attacker"}); err == nil {
		t.Fatal("accepted a different device at the selected meter address")
	}
	if err := validateMeterIdentity(device, shelly.DeviceInfo{ShellyDeviceID: "SHELLYPRO3EM-MAIN"}); err == nil {
		t.Fatal("accepted a matching device ID without the stored MAC address")
	}
	if err := validateMeterIdentity(device, shelly.DeviceInfo{ShellyDeviceID: "SHELLYPRO3EM-MAIN", MACAddress: "001122334455"}); err == nil {
		t.Fatal("accepted a matching device ID with a different MAC address")
	}
}

func TestManagerConcurrentReconfigureAndStop(t *testing.T) {
	t.Parallel()
	store, err := database.Open(filepath.Join(t.TempDir(), "manager.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	manager := NewManager(context.Background(), store, config.Config{}, nil, nil, nil)
	var wait sync.WaitGroup
	for index := 0; index < 20; index++ {
		wait.Add(1)
		go func(stop bool) {
			defer wait.Done()
			if stop {
				manager.Stop()
				return
			}
			if err := manager.Reconfigure(context.Background()); err != nil {
				t.Errorf("reconfigure: %v", err)
			}
		}(index%3 == 0)
	}
	wait.Wait()
	state := manager.Latest()
	if state.Enabled || state.Configured {
		t.Fatalf("unexpected default state: %+v", state)
	}
}

func TestManagerPublishDeliversLatestState(t *testing.T) {
	t.Parallel()
	manager := &Manager{subscribers: map[chan LiveState]struct{}{}}
	stream, unsubscribe, err := manager.Subscribe()
	if err != nil {
		t.Fatal(err)
	}
	defer unsubscribe()
	<-stream
	for index := 1; index <= 10; index++ {
		manager.publish(LiveState{ConsecutiveFailures: index})
	}
	latest := manager.Latest()
	if latest.ConsecutiveFailures != 10 {
		t.Fatalf("latest failures = %d", latest.ConsecutiveFailures)
	}
	var delivered LiveState
	for len(stream) > 0 {
		delivered = <-stream
	}
	if delivered.ConsecutiveFailures != 10 {
		t.Fatalf("subscriber received failures = %d", delivered.ConsecutiveFailures)
	}
}

func TestManagerLatestMarksAnOldOnlineReadingStale(t *testing.T) {
	t.Parallel()
	manager := &Manager{subscribers: map[chan LiveState]struct{}{}}
	manager.publish(LiveState{
		Online:              true,
		PollIntervalSeconds: 10,
		Reading: shelly.EnergyReading{
			Timestamp: time.Now().Add(-21 * time.Second),
		},
	})
	if state := manager.Latest(); !state.Stale {
		t.Fatalf("old online reading was not marked stale: %+v", state)
	}

	manager.publish(LiveState{
		Online:              false,
		PollIntervalSeconds: 10,
		Reading: shelly.EnergyReading{
			Timestamp: time.Now().Add(-time.Hour),
		},
	})
	if state := manager.Latest(); state.Stale {
		t.Fatalf("Latest changed the explicitly non-stale offline state: %+v", state)
	}
}

func TestCollectorHelpersCoverWrappedErrorsAndBackoffBoundaries(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		err  error
		want string
	}{
		{name: "wrapped authentication", err: errors.Join(errors.New("read failed"), shelly.ErrAuthenticationRequired), want: "authentication"},
		{name: "wrapped deadline", err: errors.Join(errors.New("read failed"), context.DeadlineExceeded), want: "timeout"},
		{name: "timeout text", err: errors.New("client timeout while awaiting headers"), want: "timeout"},
		{name: "invalid text", err: errors.New("invalid voltage"), want: "invalid_reading"},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if got := classifyCollectorError(test.err); got != test.want {
				t.Fatalf("classifyCollectorError() = %q, want %q", got, test.want)
			}
		})
	}

	if got := boundedBackoff(10*time.Second, 0); got != 10*time.Second {
		t.Fatalf("zero-failure backoff = %s, want 10s", got)
	}
	if got := boundedBackoff(10*time.Second, 7); got != 5*time.Minute {
		t.Fatalf("seventh-failure backoff = %s, want 5m", got)
	}
	if got := userCollectorError(shelly.ErrAuthenticationRequired); got != "meter authentication failed" {
		t.Fatalf("authentication user error = %q", got)
	}
	if got := userCollectorError(errors.New("private device response")); got != "meter reading failed: request" {
		t.Fatalf("request user error leaks details or has wrong class: %q", got)
	}
}

func TestManagerUpdateAggregatesRestoresStateAndRecordsMissingSamples(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store, deviceID := managerTestStoreAndDevice(t, "192.168.88.2")
	energyConfig, err := store.GetEnergyConfig(ctx)
	if err != nil {
		t.Fatal(err)
	}
	energyConfig.DeviceID = deviceID
	energyConfig.ComponentKey = "em:0"
	energyConfig.Timezone = "Europe/Lisbon"

	manager := &Manager{store: store}
	firstAt := time.Date(2026, time.July, 12, 10, 5, 10, 0, time.UTC)
	firstPower := 1000.0
	accumulators := map[string]Accumulator{}
	accumulators, updates, err := manager.prepareAggregates(ctx, energyConfig, shelly.EnergyReading{Timestamp: firstAt, PowerW: &firstPower}, DeltaResult{EnergyWh: 10, Quality: QualityMeasured}, accumulators)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.UpsertEnergyAggregates(ctx, updates); err != nil {
		t.Fatal(err)
	}
	secondPower := 2000.0
	accumulators, updates, err = manager.prepareAggregates(ctx, energyConfig, shelly.EnergyReading{Timestamp: firstAt.Add(10 * time.Second), PowerW: &secondPower}, DeltaResult{EnergyWh: 20, Quality: QualityCalculated}, accumulators)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.UpsertEnergyAggregates(ctx, updates); err != nil {
		t.Fatal(err)
	}

	// A fresh manager has no in-memory accumulator and must restore the stored
	// values before applying another reading in the same bucket.
	restarted := &Manager{store: store}
	restartedAccumulators := map[string]Accumulator{}
	thirdPower := 1500.0
	restartedAccumulators, updates, err = restarted.prepareAggregates(ctx, energyConfig, shelly.EnergyReading{Timestamp: firstAt.Add(20 * time.Second), PowerW: &thirdPower}, DeltaResult{EnergyWh: 5, Quality: QualityMeasured}, restartedAccumulators)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.UpsertEnergyAggregates(ctx, updates); err != nil {
		t.Fatal(err)
	}
	restartedAccumulators = restarted.recordMissing(ctx, energyConfig, firstAt.Add(30*time.Second), restartedAccumulators)

	location, err := time.LoadLocation(energyConfig.Timezone)
	if err != nil {
		t.Fatal(err)
	}
	minuteStart, minuteEnd, err := IntervalBounds(firstAt, "minute", location)
	if err != nil {
		t.Fatal(err)
	}
	items, err := store.ListEnergyAggregates(ctx, deviceID, energyConfig.ComponentKey, minuteStart, minuteEnd, "minute")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 {
		t.Fatalf("minute aggregate count = %d, want 1", len(items))
	}
	item := items[0]
	if item.EnergyConsumedWh != 35 || item.SampleCount != 3 || item.MissingSampleCount != 1 || item.CalculationQuality != QualityPartial {
		t.Fatalf("restored aggregate totals are wrong: %+v", item)
	}
	if item.AveragePowerW == nil || *item.AveragePowerW != 1500 || item.MinimumPowerW == nil || *item.MinimumPowerW != 1000 || item.MaximumPowerW == nil || *item.MaximumPowerW != 2000 {
		t.Fatalf("restored aggregate power statistics are wrong: %+v", item)
	}
}

func TestManagerDeviceAccessValidatesAddressAndDecryptsAssignedCredentials(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store, deviceID := managerTestStoreAndDevice(t, "192.168.89.2")
	manager := &Manager{store: store}

	device, address, credential, err := manager.deviceAccess(ctx, deviceID)
	if err != nil {
		t.Fatal(err)
	}
	if device.ID != deviceID || address.String() != "192.168.89.2" || credential != nil {
		t.Fatalf("unexpected credential-free device access: device=%+v address=%s credential=%+v", device, address, credential)
	}

	cipher, err := credentials.NewCipher([]byte("0123456789abcdef0123456789abcdef"))
	if err != nil {
		t.Fatal(err)
	}
	encrypted, err := cipher.Encrypt("meter-password")
	if err != nil {
		t.Fatal(err)
	}
	credentialID, err := store.CreateCredential(ctx, "Meter", "admin", encrypted)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.UpdateDeviceCredential(ctx, deviceID, credentialID); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := manager.deviceAccess(ctx, deviceID); err == nil || err.Error() != "selected meter credentials cannot be decrypted" {
		t.Fatalf("missing cipher error = %v", err)
	}

	manager.cipher = cipher
	_, _, credential, err = manager.deviceAccess(ctx, deviceID)
	if err != nil {
		t.Fatal(err)
	}
	if credential == nil || credential.Username != "admin" || credential.Password != "meter-password" {
		t.Fatalf("decrypted credential = %+v", credential)
	}

	invalidStore, invalidDeviceID := managerTestStoreAndDevice(t, "not-an-ip-address")
	invalidManager := &Manager{store: invalidStore}
	if _, _, _, err := invalidManager.deviceAccess(ctx, invalidDeviceID); err == nil || err.Error() != "selected meter has an invalid IPv4 address" {
		t.Fatalf("invalid address error = %v", err)
	}
}

func TestPruneAccumulatorsKeepsCurrentIntervalsOnly(t *testing.T) {
	t.Parallel()
	start := time.Date(2026, time.July, 12, 10, 0, 0, 0, time.UTC)
	expired, err := NewAccumulator(start, start.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	current, err := NewAccumulator(start, start.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	accumulators := map[string]Accumulator{"minute": expired, "hour": current}
	pruneAccumulators(accumulators, start.Add(time.Minute))
	if _, ok := accumulators["minute"]; ok {
		t.Fatal("expired accumulator was not pruned")
	}
	if _, ok := accumulators["hour"]; !ok {
		t.Fatal("current accumulator was pruned")
	}
}

func TestManagerSubscriberLimit(t *testing.T) {
	t.Parallel()
	manager := &Manager{subscribers: map[chan LiveState]struct{}{}}
	unsubscribers := make([]func(), 0, maxSubscribers)
	for index := 0; index < maxSubscribers; index++ {
		_, unsubscribe, err := manager.Subscribe()
		if err != nil {
			t.Fatal(err)
		}
		unsubscribers = append(unsubscribers, unsubscribe)
	}
	if _, _, err := manager.Subscribe(); !errors.Is(err, ErrTooManySubscribers) {
		t.Fatalf("subscriber overflow error = %v", err)
	}
	for _, unsubscribe := range unsubscribers {
		unsubscribe()
	}
}

func TestManagerCollectsAndPersistsWithInjectedShellyClient(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store, deviceID := managerTestStoreAndDevice(t, "192.168.89.2")
	energyConfig, _ := store.GetEnergyConfig(ctx)
	energyConfig.Enabled = true
	energyConfig.DeviceID = deviceID
	energyConfig.ComponentKey = "em:0"
	energyConfig.ComponentType = "em"
	energyConfig.PollIntervalSeconds = 5
	if _, err := store.SaveEnergyConfig(ctx, energyConfig); err != nil {
		t.Fatal(err)
	}
	power, imported := 1200.0, 100000.0
	client := &fakeEnergyClient{reading: shelly.EnergyReading{Timestamp: time.Now(), PowerW: &power, ImportedEnergyWh: &imported}, called: make(chan struct{})}
	manager := newTestManager(store)
	manager.newClient = func() energyClient { return client }
	if err := manager.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer manager.Stop()
	select {
	case <-client.called:
	case <-time.After(2 * time.Second):
		t.Fatal("collector did not call fake meter")
	}
	waitFor(t, func() bool {
		_, err := store.LatestEnergySample(ctx, deviceID, "em:0")
		state := manager.Latest()
		return err == nil && state.Online && !state.Stale && state.DeviceID == deviceID
	})
	state := manager.Latest()
	if !state.Online || state.Stale || state.DeviceID != deviceID {
		t.Fatalf("collector state = %+v", state)
	}
	aggregates, err := store.ListEnergyAggregates(ctx, deviceID, "em:0", time.Now().Add(-time.Hour), time.Now().Add(time.Hour), "minute")
	if err != nil || len(aggregates) != 1 {
		t.Fatalf("minute aggregates = %+v: %v", aggregates, err)
	}
}

func TestManagerAuthenticationFailureUpdatesInventoryWithoutCredentials(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store, deviceID := managerTestStoreAndDevice(t, "192.168.89.2")
	energyConfig, _ := store.GetEnergyConfig(ctx)
	energyConfig.Enabled = true
	energyConfig.DeviceID = deviceID
	energyConfig.ComponentKey = "em:0"
	energyConfig.ComponentType = "em"
	energyConfig.PollIntervalSeconds = 5
	if _, err := store.SaveEnergyConfig(ctx, energyConfig); err != nil {
		t.Fatal(err)
	}
	client := &fakeEnergyClient{identifyErr: shelly.ErrAuthenticationRequired, called: make(chan struct{})}
	manager := newTestManager(store)
	manager.newClient = func() energyClient { return client }
	if err := manager.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer manager.Stop()
	select {
	case <-client.called:
	case <-time.After(2 * time.Second):
		t.Fatal("collector did not call fake meter")
	}
	waitFor(t, func() bool {
		device, err := store.GetDevice(ctx, deviceID)
		return err == nil && device.AuthenticationRequired && device.OnlineStatus == "authentication_required"
	})
}

type fakeEnergyClient struct {
	reading     shelly.EnergyReading
	identifyErr error
	readErr     error
	called      chan struct{}
	once        sync.Once
}

func (f *fakeEnergyClient) signal() { f.once.Do(func() { close(f.called) }) }

func (f *fakeEnergyClient) Identify(context.Context, netip.Addr) (shelly.DeviceInfo, error) {
	if f.identifyErr != nil {
		f.signal()
		return shelly.DeviceInfo{}, f.identifyErr
	}
	return shelly.DeviceInfo{ShellyDeviceID: "manager-meter"}, nil
}

func (f *fakeEnergyClient) DiscoverEnergyComponents(context.Context, netip.Addr, *shelly.Credential) ([]shelly.EnergyComponent, error) {
	return []shelly.EnergyComponent{{Key: "em:0", Type: "em"}}, nil
}

func (f *fakeEnergyClient) ReadEnergy(context.Context, netip.Addr, string, *shelly.Credential) (shelly.EnergyReading, error) {
	f.signal()
	return f.reading, f.readErr
}

func newTestManager(store *database.Store) *Manager {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	return NewManager(context.Background(), store, config.Config{DefaultConnectionTimeout: time.Second, DefaultRequestTimeout: time.Second, OfflineFailureThreshold: 1}, logger, metrics.New(), nil)
}

func waitFor(t *testing.T, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("condition was not reached before timeout")
}

func withPollInterval(value database.EnergyConfig, interval int) database.EnergyConfig {
	value.PollIntervalSeconds = interval
	return value
}

func withBillingDay(value database.EnergyConfig, day int) database.EnergyConfig {
	value.BillingCycleStartDay = day
	return value
}

func withTimezone(value database.EnergyConfig, timezone string) database.EnergyConfig {
	value.Timezone = timezone
	return value
}

func withMaximumPower(value database.EnergyConfig, power float64) database.EnergyConfig {
	value.MaximumExpectedPowerW = power
	return value
}

func floatPointer(value float64) *float64 { return &value }

func managerTestStoreAndDevice(t *testing.T, address string) (*database.Store, int64) {
	t.Helper()
	ctx := context.Background()
	store, err := database.Open(filepath.Join(t.TempDir(), "manager-helper.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	network, err := store.SaveNetwork(ctx, database.Network{Name: "Manager test", CIDR: "192.168.89.0/24", Enabled: true, ScanIntervalSeconds: 300, ScanConcurrency: 2, ConnectionTimeoutMS: 100, RequestTimeoutMS: 200})
	if err != nil {
		t.Fatal(err)
	}
	created, err := store.UpsertDevice(ctx, network.ID, address, shelly.DeviceInfo{ShellyDeviceID: "manager-meter", Name: "Manager meter", Model: "SPEM-003CEBEU", RawDeviceInfo: []byte(`{"id":"manager-meter"}`)}, false, "")
	if err != nil {
		t.Fatal(err)
	}
	return store, created.Device.ID
}
