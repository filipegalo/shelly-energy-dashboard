package database

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"github.com/filipegalo/shelly-inventory/internal/shelly"
)

func TestDeviceUpsertAtNewIPPreservesUserFields(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "inventory.db")
	store, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	network, err := store.SaveNetwork(ctx, Network{Name: "IoT", CIDR: "192.168.20.0/24", Enabled: true, ScanIntervalSeconds: 300, ScanConcurrency: 4, ConnectionTimeoutMS: 100, RequestTimeoutMS: 200})
	if err != nil {
		t.Fatal(err)
	}
	room, err := store.SaveRoom(ctx, Room{Name: "Garage", Icon: "garage", SortOrder: 10})
	if err != nil {
		t.Fatal(err)
	}
	info := shelly.DeviceInfo{ShellyDeviceID: "shellyplus1-stable", Name: "Device name", Model: "SNSW-001X16EU", Generation: 2, MACAddress: "aabbccddeeff", FirmwareVersion: "1.4.4", Capabilities: []string{"switches"}, RawDeviceInfo: json.RawMessage(`{"id":"shellyplus1-stable"}`), RawStatus: json.RawMessage(`{"switch:0":{}}`)}
	first, err := store.UpsertDevice(ctx, network.ID, "192.168.20.10", info, false, "")
	if err != nil {
		t.Fatal(err)
	}
	if !first.New {
		t.Fatal("expected a new device")
	}
	if err := store.UpdateDeviceName(ctx, first.Device.ID, "Garage door controller"); err != nil {
		t.Fatal(err)
	}
	if err := store.UpdateDeviceRoom(ctx, first.Device.ID, room.ID); err != nil {
		t.Fatal(err)
	}
	second, err := store.UpsertDevice(ctx, network.ID, "192.168.20.99", info, false, "")
	if err != nil {
		t.Fatal(err)
	}
	if second.New {
		t.Fatal("stable device was duplicated")
	}
	if second.Device.ID != first.Device.ID || second.Device.IPAddress != "192.168.20.99" {
		t.Fatalf("identity/IP not reconciled: %+v", second.Device)
	}
	if second.Device.CustomName != "Garage door controller" || second.Device.RoomID != room.ID {
		t.Fatalf("user fields were not preserved: %+v", second.Device)
	}
	if !second.Device.FirstSeenAt.Equal(first.Device.FirstSeenAt) {
		t.Fatal("first-seen timestamp changed")
	}
}

func TestAuthenticationAndPartialObservationsDoNotDuplicateOrEraseDevice(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store, err := Open(filepath.Join(t.TempDir(), "partial.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	network, err := store.SaveNetwork(ctx, Network{Name: "IoT", CIDR: "192.168.22.0/30", Enabled: true, ScanIntervalSeconds: 300, ScanConcurrency: 2, ConnectionTimeoutMS: 100, RequestTimeoutMS: 200})
	if err != nil {
		t.Fatal(err)
	}
	full := shelly.DeviceInfo{ShellyDeviceID: "stable", Name: "Device", Model: "model", Generation: 2, DeviceType: "switch", MACAddress: "aabbccddeeff", Hostname: "host", FirmwareVersion: "1.2.3", Capabilities: []string{"switches"}, RawDeviceInfo: json.RawMessage(`{"id":"stable"}`)}
	first, err := store.UpsertDevice(ctx, network.ID, "192.168.22.1", full, false, "")
	if err != nil {
		t.Fatal(err)
	}
	auth, err := store.UpsertDevice(ctx, network.ID, "192.168.22.1", shelly.DeviceInfo{}, true, "authentication required")
	if err != nil {
		t.Fatal(err)
	}
	if auth.New || auth.Device.ID != first.Device.ID {
		t.Fatalf("auth response duplicated device: %+v", auth)
	}
	partial, err := store.UpsertDevice(ctx, network.ID, "192.168.22.1", shelly.DeviceInfo{ShellyDeviceID: "stable"}, false, "")
	if err != nil {
		t.Fatal(err)
	}
	if partial.Device.Name != "Device" || partial.Device.Model != "model" || partial.Device.FirmwareVersion != "1.2.3" || partial.Device.CapabilitiesJSON != `["switches"]` {
		t.Fatalf("partial response erased fields: %+v", partial.Device)
	}
	devices, err := store.ListDevices(ctx, DeviceFilter{})
	if err != nil || len(devices) != 1 {
		t.Fatalf("devices: %v %v", devices, err)
	}
}

func TestStorePersistsAcrossRestartAndSoftDeletesNetwork(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "inventory.db")
	store, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	network, err := store.SaveNetwork(ctx, Network{Name: "IoT", CIDR: "192.168.50.0/30", Enabled: true, ScanIntervalSeconds: 300, ScanConcurrency: 2, ConnectionTimeoutMS: 100, RequestTimeoutMS: 200})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if _, err := store.GetNetwork(ctx, network.ID); err != nil {
		t.Fatalf("network did not persist: %v", err)
	}
	if err := store.DeleteNetwork(ctx, network.ID); err != nil {
		t.Fatal(err)
	}
	list, err := store.ListNetworks(ctx, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 0 {
		t.Fatalf("soft-deleted network remains visible: %v", list)
	}
	if _, err := store.SaveNetwork(ctx, Network{Name: "IoT", CIDR: "192.168.50.0/30", Enabled: true, ScanIntervalSeconds: 300, ScanConcurrency: 2, ConnectionTimeoutMS: 100, RequestTimeoutMS: 200}); err != nil {
		t.Fatalf("could not re-add soft-deleted network: %v", err)
	}
}

func TestMissedDeviceTransitionsOfflineAtThreshold(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store, err := Open(filepath.Join(t.TempDir(), "inventory.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	network, _ := store.SaveNetwork(ctx, Network{Name: "IoT", CIDR: "192.168.60.0/30", Enabled: true, ScanIntervalSeconds: 300, ScanConcurrency: 2, ConnectionTimeoutMS: 100, RequestTimeoutMS: 200})
	result, err := store.UpsertDevice(ctx, network.ID, "192.168.60.1", shelly.DeviceInfo{ShellyDeviceID: "stable", Model: "test", RawDeviceInfo: json.RawMessage(`{"id":"stable"}`)}, false, "")
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if err := store.MarkMissedDevices(ctx, network.ID, nil, 3); err != nil {
			t.Fatal(err)
		}
	}
	device, _ := store.GetDevice(ctx, result.Device.ID)
	if device.OnlineStatus != "online" {
		t.Fatalf("went offline early: %s", device.OnlineStatus)
	}
	if err := store.MarkMissedDevices(ctx, network.ID, nil, 3); err != nil {
		t.Fatal(err)
	}
	device, _ = store.GetDevice(ctx, result.Device.ID)
	if device.OnlineStatus != "offline" || device.ConsecutiveFailures != 3 {
		t.Fatalf("bad transition: %+v", device)
	}
}

func TestRoomCredentialScanAndDashboardLifecycle(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store, err := Open(filepath.Join(t.TempDir(), "inventory.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	network, err := store.SaveNetwork(ctx, Network{Name: "IoT", CIDR: "192.168.70.0/30", Enabled: true, AutomaticScanEnabled: true, ScanIntervalSeconds: 60, ScanConcurrency: 2, ConnectionTimeoutMS: 100, RequestTimeoutMS: 200})
	if err != nil {
		t.Fatal(err)
	}
	network.Name = "IoT updated"
	network, err = store.SaveNetwork(ctx, network)
	if err != nil || network.Name != "IoT updated" {
		t.Fatalf("update network: %+v %v", network, err)
	}
	if enabled, err := store.ListNetworks(ctx, true); err != nil || len(enabled) != 1 {
		t.Fatalf("enabled networks: %v %v", enabled, err)
	}
	room, err := store.SaveRoom(ctx, Room{Name: "Office", Description: "Work", Icon: "desk", SortOrder: 20})
	if err != nil {
		t.Fatal(err)
	}
	credentialID, err := store.CreateCredential(ctx, "Local admin", "admin", "v1:nonce:cipher")
	if err != nil {
		t.Fatal(err)
	}
	profiles, err := store.ListCredentials(ctx)
	if err != nil || len(profiles) != 1 || profiles[0].Username != "admin" {
		t.Fatalf("profiles: %v %v", profiles, err)
	}
	username, secret, err := store.CredentialSecret(ctx, credentialID)
	if err != nil || username != "admin" || secret != "v1:nonce:cipher" {
		t.Fatalf("credential secret: %q %q %v", username, secret, err)
	}
	device, err := store.UpsertDevice(ctx, network.ID, "192.168.70.1", shelly.DeviceInfo{ShellyDeviceID: "room-device", Name: "Office relay", Model: "test", RawDeviceInfo: json.RawMessage(`{"id":"room-device"}`)}, false, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := store.UpdateDeviceRoom(ctx, device.Device.ID, room.ID); err != nil {
		t.Fatal(err)
	}
	if err := store.UpdateDeviceCredential(ctx, device.Device.ID, credentialID); err != nil {
		t.Fatal(err)
	}
	if user, ciphertext, err := store.DeviceCredentialForAddress(ctx, network.ID, "192.168.70.1"); err != nil || user != "admin" || ciphertext != "v1:nonce:cipher" {
		t.Fatalf("device credentials: %q %q %v", user, ciphertext, err)
	}
	filtered, err := store.ListDevices(ctx, DeviceFilter{Search: "Office", RoomID: room.ID, NetworkID: network.ID, Status: "online", Generation: "0"})
	if err != nil || len(filtered) != 1 {
		t.Fatalf("filtered devices: %v %v", filtered, err)
	}
	stats, err := store.DashboardStats(ctx)
	if err != nil || stats.TotalDevices != 1 || stats.OnlineDevices != 1 {
		t.Fatalf("dashboard: %+v %v", stats, err)
	}
	scan, err := store.CreateScan(ctx, network.ID, "manual", 2)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.CreateScan(ctx, network.ID, "scheduled", 2); err == nil {
		t.Fatal("expected active scan conflict")
	}
	if err := store.StartScan(ctx, scan.ID); err != nil {
		t.Fatal(err)
	}
	scan.Status = "running"
	scan.AddressesScanned = 1
	scan.DevicesFound = 1
	scan.NewDevicesFound = 1
	if err := store.UpdateScanProgress(ctx, scan); err != nil {
		t.Fatal(err)
	}
	scan.Status = "completed"
	scan.AddressesScanned = 2
	if err := store.CompleteScan(ctx, scan); err != nil {
		t.Fatal(err)
	}
	loaded, err := store.GetScan(ctx, scan.ID)
	if err != nil || loaded.Status != "completed" || loaded.Percentage() != 100 {
		t.Fatalf("scan: %+v %v", loaded, err)
	}
	if scans, err := store.ListScans(ctx, 10); err != nil || len(scans) != 1 {
		t.Fatalf("scans: %v %v", scans, err)
	}
	if err := store.DeleteRoom(ctx, room.ID); err != nil {
		t.Fatal(err)
	}
	deviceAfter, err := store.GetDevice(ctx, device.Device.ID)
	if err != nil || deviceAfter.RoomID != 1 {
		t.Fatalf("device not moved to Unassigned: %+v %v", deviceAfter, err)
	}
}

func TestOpenRecoversInterruptedScansAndScheduledDueQuery(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "restart.db")
	store, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	network, err := store.SaveNetwork(ctx, Network{Name: "Scheduled", CIDR: "192.168.110.0/30", Enabled: true, AutomaticScanEnabled: true, ScanIntervalSeconds: 60, ScanConcurrency: 1, ConnectionTimeoutMS: 100, RequestTimeoutMS: 200})
	if err != nil {
		t.Fatal(err)
	}
	due, err := store.DueScheduledNetworks(ctx, time.Now())
	if err != nil || len(due) != 1 {
		t.Fatalf("due networks: %v %v", due, err)
	}
	scan, err := store.CreateScan(ctx, network.ID, "scheduled", 2)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.StartScan(ctx, scan.ID); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	recovered, err := store.GetScan(ctx, scan.ID)
	if err != nil || recovered.Status != "failed" || recovered.ErrorSummary != "interrupted by application restart" {
		t.Fatalf("recovered scan: %+v %v", recovered, err)
	}
}

func TestNetworkCannotBeDeletedDuringActiveScan(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store, err := Open(filepath.Join(t.TempDir(), "active.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.Ready(ctx); err != nil {
		t.Fatal(err)
	}
	network, err := store.SaveNetwork(ctx, Network{Name: "Active", CIDR: "192.168.130.0/30", Enabled: true, ScanIntervalSeconds: 60, ScanConcurrency: 1, ConnectionTimeoutMS: 100, RequestTimeoutMS: 200})
	if err != nil {
		t.Fatal(err)
	}
	scan, err := store.CreateScan(ctx, network.ID, "manual", 2)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.DeleteNetwork(ctx, network.ID); err == nil {
		t.Fatal("deleted network during active scan")
	}
	scan.Status = "cancelled"
	if err := store.CompleteScan(ctx, scan); err != nil {
		t.Fatal(err)
	}
	if err := store.DeleteNetwork(ctx, network.ID); err != nil {
		t.Fatal(err)
	}
}

func TestRoomListingAndViewHelpers(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store, err := Open(filepath.Join(t.TempDir(), "helpers.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if _, err := store.SaveRoom(ctx, Room{Name: "Living Room", Icon: "sofa", SortOrder: 5}); err != nil {
		t.Fatal(err)
	}
	rooms, err := store.ListRooms(ctx)
	if err != nil || len(rooms) != 2 {
		t.Fatalf("rooms: %v %v", rooms, err)
	}
	device := Device{Name: "Shelly", CapabilitiesJSON: `["switches","power meters"]`}
	if device.DisplayName() != "Shelly" || len(device.Capabilities()) != 2 {
		t.Fatalf("device helpers: %q %v", device.DisplayName(), device.Capabilities())
	}
	device.CustomName = "Lamp"
	if device.DisplayName() != "Lamp" {
		t.Fatal("custom name did not take priority")
	}
	device.CustomName = ""
	device.Name = ""
	device.Model = "Model"
	if device.DisplayName() != "Model" {
		t.Fatal("model fallback failed")
	}
	if (Scan{}).Percentage() != 0 {
		t.Fatal("zero-total scan percentage must be zero")
	}
}
