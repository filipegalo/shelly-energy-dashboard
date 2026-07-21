package scans

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/netip"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/filipegalo/shelly-energy-dashboard/internal/config"
	"github.com/filipegalo/shelly-energy-dashboard/internal/credentials"
	"github.com/filipegalo/shelly-energy-dashboard/internal/database"
	"github.com/filipegalo/shelly-energy-dashboard/internal/metrics"
	"github.com/filipegalo/shelly-energy-dashboard/internal/shelly"
)

func TestRunProbePoolVisitsEveryAddressWithinConcurrency(t *testing.T) {
	t.Parallel()
	hosts := testHosts(12)
	release := make(chan struct{})
	started := make(chan struct{}, len(hosts))
	var active atomic.Int64
	var maximum atomic.Int64
	results := runProbePool(context.Background(), hosts, 3, func(_ context.Context, address netip.Addr) (shelly.DeviceInfo, error) {
		current := active.Add(1)
		for {
			previous := maximum.Load()
			if current <= previous || maximum.CompareAndSwap(previous, current) {
				break
			}
		}
		started <- struct{}{}
		<-release
		active.Add(-1)
		return shelly.DeviceInfo{ShellyDeviceID: address.String()}, nil
	})
	for range 3 {
		<-started
	}
	if got := maximum.Load(); got != 3 {
		t.Fatalf("maximum concurrency = %d, want 3", got)
	}
	close(release)
	count := 0
	seen := map[netip.Addr]bool{}
	for result := range results {
		count++
		seen[result.address] = true
	}
	if count != len(hosts) || len(seen) != len(hosts) {
		t.Fatalf("visited %d/%d unique=%d", count, len(hosts), len(seen))
	}
}

func TestRunProbePoolCancellationStopsInflightAndScheduling(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	started := make(chan struct{}, 20)
	results := runProbePool(ctx, testHosts(20), 2, func(probeCtx context.Context, _ netip.Addr) (shelly.DeviceInfo, error) {
		started <- struct{}{}
		<-probeCtx.Done()
		return shelly.DeviceInfo{}, probeCtx.Err()
	})
	<-started
	<-started
	cancel()
	for range results {
	}
	if got := len(started); got >= 20 {
		t.Fatalf("cancellation did not stop scheduling; started=%d", got)
	}
}

func testHosts(count int) []netip.Addr {
	result := make([]netip.Addr, 0, count)
	address := netip.MustParseAddr("192.168.80.1")
	for range count {
		result = append(result, address)
		address = address.Next()
	}
	return result
}

func TestManagerDiscoversDeduplicatesMovesIPAndStreamsProgress(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	manager, store, network := testManager(t, ctx)
	defer store.Close()
	var phase atomic.Int64
	phase.Store(1)
	manager.probe = func(_ context.Context, _ database.Network, address netip.Addr) (shelly.DeviceInfo, error) {
		found := (phase.Load() == 1 && address.String() == "192.168.100.1") || (phase.Load() == 2 && address.String() == "192.168.100.2")
		if !found {
			return shelly.DeviceInfo{}, shelly.ErrNotShelly
		}
		return shelly.DeviceInfo{ShellyDeviceID: "shelly-stable", Name: "Relay", Model: "mock", Generation: 2, MACAddress: "aabbccddeeff"}, nil
	}
	first, err := manager.Start(ctx, network.ID, "manual")
	if err != nil {
		t.Fatal(err)
	}
	terminal := waitTerminal(t, manager, first.ID)
	if terminal.Status != "completed_with_errors" || terminal.DevicesFound != 1 || terminal.NewDevicesFound != 1 {
		t.Fatalf("first scan = %+v", terminal)
	}
	devices, err := store.ListDevices(ctx, database.DeviceFilter{})
	if err != nil || len(devices) != 1 {
		t.Fatalf("devices: %v %v", devices, err)
	}
	room, err := store.SaveRoom(ctx, database.Room{Name: "Garage", Icon: "garage", SortOrder: 10})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.UpdateDeviceRoom(ctx, devices[0].ID, room.ID); err != nil {
		t.Fatal(err)
	}
	if err := store.UpdateDeviceName(ctx, devices[0].ID, "Garage relay"); err != nil {
		t.Fatal(err)
	}
	phase.Store(2)
	second, err := manager.Start(ctx, network.ID, "manual")
	if err != nil {
		t.Fatal(err)
	}
	terminal = waitTerminal(t, manager, second.ID)
	if terminal.UpdatedDevices != 1 {
		t.Fatalf("second scan = %+v", terminal)
	}
	devices, err = store.ListDevices(ctx, database.DeviceFilter{})
	if err != nil || len(devices) != 1 {
		t.Fatalf("dedupe failed: %v %v", devices, err)
	}
	if devices[0].IPAddress != "192.168.100.2" || devices[0].RoomID != room.ID || devices[0].CustomName != "Garage relay" {
		t.Fatalf("IP move lost user data: %+v", devices[0])
	}
}

func TestManagerRejectsOverlapAndCancelsScan(t *testing.T) {
	t.Parallel()
	ctx, cancelApp := context.WithCancel(context.Background())
	defer cancelApp()
	manager, store, network := testManager(t, ctx)
	defer store.Close()
	started := make(chan struct{}, 2)
	manager.probe = func(probeCtx context.Context, _ database.Network, _ netip.Addr) (shelly.DeviceInfo, error) {
		started <- struct{}{}
		<-probeCtx.Done()
		return shelly.DeviceInfo{}, probeCtx.Err()
	}
	scan, err := manager.Start(ctx, network.ID, "manual")
	if err != nil {
		t.Fatal(err)
	}
	<-started
	if _, err := manager.Start(ctx, network.ID, "manual"); err == nil {
		t.Fatal("expected overlapping scan rejection")
	}
	if err := manager.Cancel(scan.ID); err != nil {
		t.Fatal(err)
	}
	terminal := waitTerminal(t, manager, scan.ID)
	if terminal.Status != "cancelled" {
		t.Fatalf("terminal = %+v", terminal)
	}
	if err := manager.Cancel(scan.ID); err == nil {
		t.Fatal("expected completed scan cancellation rejection")
	}
	manager.Wait()
}

func testManager(t *testing.T, ctx context.Context) (*Manager, *database.Store, database.Network) {
	t.Helper()
	store, err := database.Open(filepath.Join(t.TempDir(), "scan.db"))
	if err != nil {
		t.Fatal(err)
	}
	network, err := store.SaveNetwork(ctx, database.Network{Name: "IoT", CIDR: "192.168.100.0/30", Enabled: true, ScanIntervalSeconds: 300, ScanConcurrency: 2, ConnectionTimeoutMS: 50, RequestTimeoutMS: 100})
	if err != nil {
		t.Fatal(err)
	}
	cfg := config.Config{MaxSubnetPrefix: 16, MaxHosts: 65534, DefaultScanConcurrency: 2, DefaultConnectionTimeout: 50 * time.Millisecond, DefaultRequestTimeout: 100 * time.Millisecond, OfflineFailureThreshold: 3}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	return NewManager(ctx, store, cfg, logger, metrics.New(), nil), store, network
}

func waitTerminal(t *testing.T, manager *Manager, scanID int64) database.Scan {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	stream, unsubscribe, err := manager.Subscribe(ctx, scanID)
	if err != nil {
		t.Fatal(err)
	}
	defer unsubscribe()
	for {
		select {
		case scan := <-stream:
			if scan.Status != "queued" && scan.Status != "running" {
				return scan
			}
		case <-ctx.Done():
			t.Fatal("timed out waiting for terminal scan")
		}
	}
}

func TestManagerValidationAndErrorClassification(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	manager, store, network := testManager(t, ctx)
	defer store.Close()
	network.Enabled = false
	if _, err := store.SaveNetwork(ctx, network); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Start(ctx, network.ID, "manual"); err == nil {
		t.Fatal("started disabled network")
	}
	if _, err := manager.StartManual(ctx, network.ID, netip.MustParseAddr("192.168.100.1")); err == nil {
		t.Fatal("manually scanned disabled network")
	}
	network.Enabled = true
	if _, err := store.SaveNetwork(ctx, network); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.StartManual(ctx, network.ID, netip.MustParseAddr("192.168.101.1")); err == nil {
		t.Fatal("accepted target outside network")
	}
	if err := manager.Cancel(999); err == nil {
		t.Fatal("cancelled unknown scan")
	}
	if classifyError(shelly.ErrResponseTooLarge) != "oversized" || classifyError(shelly.ErrNotShelly) != "not_shelly" || classifyError(context.DeadlineExceeded) != "timeout" || classifyError(errors.New("boom")) != "request" {
		t.Fatal("unexpected error classification")
	}
	if errorText(nil) != "" || errorText(shelly.ErrAuthenticationRequired) != "authentication required" {
		t.Fatal("unexpected error text")
	}
}

func TestManagerManualDiscoveryCredentialResolutionAndStoredSubscription(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	manager, store, network := testManager(t, ctx)
	defer store.Close()
	cipher, _ := credentials.NewCipher([]byte("0123456789abcdef0123456789abcdef"))
	manager.cipher = cipher
	encrypted, _ := cipher.Encrypt("secret")
	credentialID, err := store.CreateCredential(ctx, "Admin", "admin", encrypted)
	if err != nil {
		t.Fatal(err)
	}
	device, err := store.UpsertDevice(ctx, network.ID, "192.168.100.1", shelly.DeviceInfo{ShellyDeviceID: "credential-device", Model: "mock"}, true, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := store.UpdateDeviceCredential(ctx, device.Device.ID, credentialID); err != nil {
		t.Fatal(err)
	}
	credential := manager.credentialForAddress(ctx, network.ID, "192.168.100.1")
	if credential == nil || credential.Username != "admin" || credential.Password != "secret" {
		t.Fatalf("credential = %+v", credential)
	}
	manager.probe = func(_ context.Context, _ database.Network, _ netip.Addr) (shelly.DeviceInfo, error) {
		return shelly.DeviceInfo{ShellyDeviceID: "manual", Model: "mock", MACAddress: "aabbccddee11"}, nil
	}
	scan, err := manager.StartManual(ctx, network.ID, netip.MustParseAddr("192.168.100.2"))
	if err != nil {
		t.Fatal(err)
	}
	terminal := waitTerminal(t, manager, scan.ID)
	if terminal.Status != "completed" {
		t.Fatalf("manual scan = %+v", terminal)
	}
	preserved, err := store.GetDevice(ctx, device.Device.ID)
	if err != nil || preserved.ConsecutiveFailures != 0 {
		t.Fatalf("manual discovery aged unrelated device: %+v %v", preserved, err)
	}
	fresh := NewManager(ctx, store, manager.cfg, manager.logger, metrics.New(), nil)
	stream, unsubscribe, err := fresh.Subscribe(ctx, scan.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer unsubscribe()
	if snapshot := <-stream; snapshot.Status != "completed" {
		t.Fatalf("stored snapshot = %+v", snapshot)
	}
	if _, _, err := fresh.Subscribe(ctx, 9999); err == nil {
		t.Fatal("subscribed to unknown scan")
	}
}

func TestAppendMDNSCandidatesHonorsCancelledContext(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	prefix := netip.MustParsePrefix("192.168.1.0/24")
	existing := []netip.Addr{netip.MustParseAddr("192.168.1.2")}
	got := appendMDNSCandidates(ctx, existing, prefix)
	if len(got) != 1 || got[0] != existing[0] {
		t.Fatalf("mDNS changed cancelled candidates: %v", got)
	}
}

func TestPrioritizeMDNSCandidates(t *testing.T) {
	t.Parallel()
	prefix := netip.MustParsePrefix("192.168.1.0/29")
	existing := []netip.Addr{netip.MustParseAddr("192.168.1.1"), netip.MustParseAddr("192.168.1.2"), netip.MustParseAddr("192.168.1.3")}
	discovered := []netip.Addr{existing[2], existing[2], netip.MustParseAddr("192.168.1.7"), netip.MustParseAddr("10.0.0.1")}
	got := prioritizeCandidates(existing, discovered, prefix)
	want := []netip.Addr{existing[2], existing[0], existing[1]}
	for index := range want {
		if got[index] != want[index] {
			t.Fatalf("priority = %v, want %v", got, want)
		}
	}
}

func TestPublishReplacesStaleSubscriberSnapshot(t *testing.T) {
	t.Parallel()
	channel := make(chan database.Scan, 1)
	channel <- database.Scan{ID: 1, AddressesScanned: 1}
	manager := &Manager{hubs: map[int64]*hub{1: {subscribers: map[chan database.Scan]struct{}{channel: {}}}}}
	manager.publish(database.Scan{ID: 1, AddressesScanned: 2})
	snapshot := <-channel
	if snapshot.AddressesScanned != 2 {
		t.Fatalf("stale snapshot = %+v", snapshot)
	}
}

func TestValidateNetworkRuntimeBounds(t *testing.T) {
	t.Parallel()
	valid := database.Network{ScanConcurrency: 32, ConnectionTimeoutMS: 750, RequestTimeoutMS: 2000}
	if err := validateNetworkRuntime(valid); err != nil {
		t.Fatal(err)
	}
	invalid := []database.Network{{ScanConcurrency: 0, ConnectionTimeoutMS: 750, RequestTimeoutMS: 2000}, {ScanConcurrency: 257, ConnectionTimeoutMS: 750, RequestTimeoutMS: 2000}, {ScanConcurrency: 1, ConnectionTimeoutMS: 49, RequestTimeoutMS: 2000}, {ScanConcurrency: 1, ConnectionTimeoutMS: 10001, RequestTimeoutMS: 2000}, {ScanConcurrency: 1, ConnectionTimeoutMS: 750, RequestTimeoutMS: 99}, {ScanConcurrency: 1, ConnectionTimeoutMS: 750, RequestTimeoutMS: 30001}}
	for _, network := range invalid {
		if err := validateNetworkRuntime(network); err == nil {
			t.Fatalf("accepted unsafe runtime: %+v", network)
		}
	}
}
