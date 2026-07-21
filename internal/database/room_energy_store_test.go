package database

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/filipegalo/shelly-energy-dashboard/internal/shelly"
)

func TestRoomEnergyMigrationAndDefaults(t *testing.T) {
	t.Parallel()
	store := openEnergyTestStore(t)
	ctx := context.Background()

	settings, err := store.GetRoomEnergyConfig(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if settings.RelativeTolerance != 0.05 || settings.AbsoluteToleranceWh != 100 || settings.DiagnosticCollection {
		t.Fatalf("room energy defaults = %+v", settings)
	}

	var applied int
	if err := store.db.QueryRowContext(ctx, `SELECT count(*) FROM schema_migrations WHERE version=3`).Scan(&applied); err != nil || applied != 1 {
		t.Fatalf("migration v3 marker: count=%d err=%v", applied, err)
	}
}

func TestCreateRoomEnergyAssignmentUsesStableSourceIdentity(t *testing.T) {
	t.Parallel()
	store := openEnergyTestStore(t)
	ctx := context.Background()
	deviceID := createEnergyTestDevice(t, store)
	kitchen := createRoomEnergyTestRoom(t, store, "Kitchen")
	start := time.Date(2026, time.July, 21, 10, 0, 0, 0, time.UTC)

	created, err := store.CreateRoomEnergyAssignment(ctx, RoomEnergyAssignment{
		RoomID:              kitchen.ID,
		DeviceID:            deviceID,
		ComponentKey:        "switch:0",
		ComponentType:       "switch",
		DisplayName:         "Oven circuit",
		Enabled:             true,
		AggregationRole:     RoomEnergyRolePrimary,
		EffectiveFrom:       start,
		PollIntervalSeconds: 15,
	})
	if err != nil {
		t.Fatal(err)
	}
	if created.ID < 1 || created.RoomID != kitchen.ID || created.DeviceID != deviceID || created.ComponentKey != "switch:0" || created.AggregationRole != RoomEnergyRolePrimary || !created.EffectiveFrom.Equal(start) {
		t.Fatalf("assignment = %+v", created)
	}

	// A discovery update may change the address, but it must not change the
	// assignment's stable device/component identity.
	device, err := store.GetDevice(ctx, deviceID)
	if err != nil {
		t.Fatal(err)
	}
	updated, err := store.UpsertDevice(ctx, device.NetworkID, "192.168.201.2", shelly.DeviceInfo{
		ShellyDeviceID: device.ShellyDeviceID,
		Model:          device.Model,
		MACAddress:     device.MACAddress,
		RawDeviceInfo:  json.RawMessage(`{"id":"energy-meter"}`),
	}, false, "")
	if err != nil {
		t.Fatal(err)
	}
	if updated.Device.ID != deviceID {
		t.Fatalf("device identity changed after IP move: %+v", updated.Device)
	}
	loaded, err := store.GetRoomEnergyAssignment(ctx, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.DeviceID != deviceID || loaded.ComponentKey != "switch:0" || loaded.IPAddress != "192.168.201.2" {
		t.Fatalf("assignment did not follow stable device identity: %+v", loaded)
	}
}

func TestRoomEnergyAssignmentsAllowIndependentChannelsAndRejectOverlap(t *testing.T) {
	t.Parallel()
	store := openEnergyTestStore(t)
	ctx := context.Background()
	deviceID := createEnergyTestDevice(t, store)
	kitchen := createRoomEnergyTestRoom(t, store, "Kitchen")
	office := createRoomEnergyTestRoom(t, store, "Office")
	start := time.Date(2026, time.July, 21, 10, 0, 0, 0, time.UTC)

	first := RoomEnergyAssignment{RoomID: kitchen.ID, DeviceID: deviceID, ComponentKey: "switch:0", ComponentType: "switch", DisplayName: "Channel 0", Enabled: true, AggregationRole: RoomEnergyRolePrimary, EffectiveFrom: start}
	if _, err := store.CreateRoomEnergyAssignment(ctx, first); err != nil {
		t.Fatal(err)
	}
	second := first
	second.RoomID, second.ComponentKey, second.DisplayName = office.ID, "switch:1", "Channel 1"
	if _, err := store.CreateRoomEnergyAssignment(ctx, second); err != nil {
		t.Fatalf("independent channel assignment failed: %v", err)
	}

	conflict := first
	conflict.RoomID, conflict.EffectiveFrom = office.ID, start.Add(time.Hour)
	if _, err := store.CreateRoomEnergyAssignment(ctx, conflict); !errors.Is(err, ErrRoomEnergyAssignmentOverlap) {
		t.Fatalf("overlap error = %v", err)
	}
}

func TestMoveRoomEnergyAssignmentPreservesHistory(t *testing.T) {
	t.Parallel()
	store := openEnergyTestStore(t)
	ctx := context.Background()
	deviceID := createEnergyTestDevice(t, store)
	office := createRoomEnergyTestRoom(t, store, "Office")
	bedroom := createRoomEnergyTestRoom(t, store, "Bedroom")
	start := time.Date(2026, time.July, 14, 8, 0, 0, 0, time.UTC)
	boundary := time.Date(2026, time.July, 15, 10, 0, 0, 0, time.UTC)

	original, err := store.CreateRoomEnergyAssignment(ctx, RoomEnergyAssignment{RoomID: office.ID, DeviceID: deviceID, ComponentKey: "switch:0", ComponentType: "switch", DisplayName: "Desk", Enabled: true, AggregationRole: RoomEnergyRolePrimary, EffectiveFrom: start})
	if err != nil {
		t.Fatal(err)
	}
	moved, err := store.MoveRoomEnergyAssignment(ctx, original.ID, bedroom.ID, boundary)
	if err != nil {
		t.Fatal(err)
	}
	closed, err := store.GetRoomEnergyAssignment(ctx, original.ID)
	if err != nil {
		t.Fatal(err)
	}
	if closed.EffectiveTo == nil || !closed.EffectiveTo.Equal(boundary) || moved.RoomID != bedroom.ID || !moved.EffectiveFrom.Equal(boundary) || moved.ID == original.ID {
		t.Fatalf("closed=%+v moved=%+v", closed, moved)
	}

	before, err := store.ListRoomEnergyAssignmentsOverlapping(ctx, start, boundary)
	if err != nil || len(before) != 1 || before[0].RoomID != office.ID {
		t.Fatalf("assignments before move = %+v err=%v", before, err)
	}
	after, err := store.ListRoomEnergyAssignmentsOverlapping(ctx, boundary, boundary.Add(time.Hour))
	if err != nil || len(after) != 1 || after[0].RoomID != bedroom.ID {
		t.Fatalf("assignments after move = %+v err=%v", after, err)
	}
}

func TestRoomEnergySubmeterHierarchyValidation(t *testing.T) {
	t.Parallel()
	store := openEnergyTestStore(t)
	ctx := context.Background()
	deviceID := createEnergyTestDevice(t, store)
	kitchen := createRoomEnergyTestRoom(t, store, "Kitchen")
	office := createRoomEnergyTestRoom(t, store, "Office")
	start := time.Date(2026, time.July, 21, 10, 0, 0, 0, time.UTC)

	parent, err := store.CreateRoomEnergyAssignment(ctx, RoomEnergyAssignment{RoomID: kitchen.ID, DeviceID: deviceID, ComponentKey: "em:0", ComponentType: "em", DisplayName: "Kitchen circuit", Enabled: true, AggregationRole: RoomEnergyRolePrimary, EffectiveFrom: start})
	if err != nil {
		t.Fatal(err)
	}

	withoutParent := RoomEnergyAssignment{RoomID: kitchen.ID, DeviceID: deviceID, ComponentKey: "switch:0", ComponentType: "switch", DisplayName: "Dishwasher", Enabled: true, AggregationRole: RoomEnergyRoleSubmeter, EffectiveFrom: start}
	if _, err := store.CreateRoomEnergyAssignment(ctx, withoutParent); !errors.Is(err, ErrRoomEnergyParentRequired) {
		t.Fatalf("missing-parent error = %v", err)
	}

	valid := withoutParent
	valid.ParentAssignmentID = parent.ID
	child, err := store.CreateRoomEnergyAssignment(ctx, valid)
	if err != nil {
		t.Fatal(err)
	}
	if child.ParentAssignmentID != parent.ID || child.ParentDisplayName != "Kitchen circuit" {
		t.Fatalf("submeter = %+v", child)
	}

	crossRoom := withoutParent
	crossRoom.RoomID, crossRoom.ComponentKey, crossRoom.ParentAssignmentID = office.ID, "switch:1", parent.ID
	if _, err := store.CreateRoomEnergyAssignment(ctx, crossRoom); !errors.Is(err, ErrRoomEnergyInvalidHierarchy) {
		t.Fatalf("cross-room parent error = %v", err)
	}

	childAsParent := withoutParent
	childAsParent.ComponentKey, childAsParent.ParentAssignmentID = "switch:2", child.ID
	if _, err := store.CreateRoomEnergyAssignment(ctx, childAsParent); !errors.Is(err, ErrRoomEnergyInvalidHierarchy) {
		t.Fatalf("submeter-as-parent error = %v", err)
	}
}

func TestMainMeterAssignmentRequiresAcknowledgement(t *testing.T) {
	t.Parallel()
	store := openEnergyTestStore(t)
	ctx := context.Background()
	deviceID := createEnergyTestDevice(t, store)
	room := createRoomEnergyTestRoom(t, store, "Whole property")
	config, err := store.GetEnergyConfig(ctx)
	if err != nil {
		t.Fatal(err)
	}
	config.DeviceID, config.ComponentKey, config.ComponentType = deviceID, "em:0", "em"
	if _, err := store.SaveEnergyConfig(ctx, config); err != nil {
		t.Fatal(err)
	}
	assignment := RoomEnergyAssignment{RoomID: room.ID, DeviceID: deviceID, ComponentKey: "em:0", ComponentType: "em", DisplayName: "Main meter", Enabled: true, AggregationRole: RoomEnergyRolePrimary, EffectiveFrom: time.Now().UTC()}
	if _, err := store.CreateRoomEnergyAssignment(ctx, assignment); !errors.Is(err, ErrRoomEnergyMainMeterConfirmation) {
		t.Fatalf("main-meter error = %v", err)
	}
	assignment.MainMeterAcknowledged = true
	if _, err := store.CreateRoomEnergyAssignment(ctx, assignment); err != nil {
		t.Fatalf("acknowledged main meter: %v", err)
	}
}

func TestDeletingRoomWithEnergyHistoryArchivesIt(t *testing.T) {
	t.Parallel()
	store := openEnergyTestStore(t)
	ctx := context.Background()
	deviceID := createEnergyTestDevice(t, store)
	room := createRoomEnergyTestRoom(t, store, "Former office")
	assignment, err := store.CreateRoomEnergyAssignment(ctx, RoomEnergyAssignment{RoomID: room.ID, DeviceID: deviceID, ComponentKey: "switch:0", ComponentType: "switch", DisplayName: "Office", Enabled: true, AggregationRole: RoomEnergyRolePrimary, EffectiveFrom: time.Now().UTC().Add(-time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.DeleteRoom(ctx, room.ID); err != nil {
		t.Fatal(err)
	}
	archived, err := store.GetRoom(ctx, room.ID)
	if err != nil || !archived.Archived {
		t.Fatalf("archived room = %+v err=%v", archived, err)
	}
	activeRooms, err := store.ListRooms(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, candidate := range activeRooms {
		if candidate.ID == room.ID {
			t.Fatalf("archived room remains in active list: %+v", activeRooms)
		}
	}
	closed, err := store.GetRoomEnergyAssignment(ctx, assignment.ID)
	if err != nil || closed.EffectiveTo == nil || !closed.Enabled {
		t.Fatalf("archived-room assignment = %+v err=%v", closed, err)
	}
	allRooms, err := store.ListRoomsIncludingArchived(ctx)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, candidate := range allRooms {
		found = found || candidate.ID == room.ID && candidate.Archived
	}
	if !found {
		t.Fatalf("archived room missing from historical list: %+v", allRooms)
	}
}

func TestRoomEnergyAssignmentRejectsUntrustedComponentIdentity(t *testing.T) {
	t.Parallel()
	store := openEnergyTestStore(t)
	ctx := context.Background()
	deviceID := createEnergyTestDevice(t, store)
	room := createRoomEnergyTestRoom(t, store, "Kitchen")
	base := RoomEnergyAssignment{RoomID: room.ID, DeviceID: deviceID, ComponentType: "switch", DisplayName: "Unsafe", Enabled: true, AggregationRole: RoomEnergyRolePrimary, EffectiveFrom: time.Now().UTC()}

	for _, component := range []string{"Shelly.Reboot:0", "switch:0?url=http://evil.invalid", "switch:-1", "switch:999"} {
		assignment := base
		assignment.ComponentKey = component
		if _, err := store.CreateRoomEnergyAssignment(ctx, assignment); !errors.Is(err, ErrRoomEnergyInvalidAssignment) {
			t.Fatalf("component %q error = %v", component, err)
		}
	}
	base.ComponentKey, base.ComponentType = "switch:0", "em"
	if _, err := store.CreateRoomEnergyAssignment(ctx, base); !errors.Is(err, ErrRoomEnergyInvalidAssignment) {
		t.Fatalf("mismatched component type error = %v", err)
	}
}

func TestRoomEnergyAssignmentRejectsSystemUnassignedRoom(t *testing.T) {
	t.Parallel()
	store := openEnergyTestStore(t)
	deviceID := createEnergyTestDevice(t, store)
	_, err := store.CreateRoomEnergyAssignment(context.Background(), RoomEnergyAssignment{
		RoomID: 1, DeviceID: deviceID, ComponentKey: "switch:0", ComponentType: "switch",
		DisplayName: "Unassigned source", Enabled: true, AggregationRole: RoomEnergyRolePrimary,
		EffectiveFrom: time.Now().UTC(),
	})
	if !errors.Is(err, ErrRoomEnergyInvalidAssignment) {
		t.Fatalf("system-room assignment error = %v", err)
	}
}

func TestReviseRoomEnergyAssignmentEffectiveDatesRoleAndParent(t *testing.T) {
	t.Parallel()
	store := openEnergyTestStore(t)
	ctx := context.Background()
	deviceID := createEnergyTestDevice(t, store)
	room := createRoomEnergyTestRoom(t, store, "Kitchen")
	start := time.Date(2026, time.July, 21, 10, 0, 0, 0, time.UTC)
	boundary := start.Add(time.Hour)
	parent, err := store.CreateRoomEnergyAssignment(ctx, RoomEnergyAssignment{RoomID: room.ID, DeviceID: deviceID, ComponentKey: "em:0", ComponentType: "em", DisplayName: "Circuit", Enabled: true, AggregationRole: RoomEnergyRolePrimary, EffectiveFrom: start})
	if err != nil {
		t.Fatal(err)
	}
	source, err := store.CreateRoomEnergyAssignment(ctx, RoomEnergyAssignment{RoomID: room.ID, DeviceID: deviceID, ComponentKey: "switch:0", ComponentType: "switch", DisplayName: "Dishwasher", Enabled: true, AggregationRole: RoomEnergyRolePrimary, EffectiveFrom: start})
	if err != nil {
		t.Fatal(err)
	}

	revised, err := store.ReviseRoomEnergyAssignment(ctx, source.ID, RoomEnergyAssignmentRevision{
		RoomID:              room.ID,
		DisplayName:         "Dishwasher plug",
		Enabled:             true,
		AggregationRole:     RoomEnergyRoleSubmeter,
		ParentAssignmentID:  parent.ID,
		PollIntervalSeconds: 20,
		EffectiveFrom:       boundary,
	})
	if err != nil {
		t.Fatal(err)
	}
	old, err := store.GetRoomEnergyAssignment(ctx, source.ID)
	if err != nil {
		t.Fatal(err)
	}
	if old.EffectiveTo == nil || !old.EffectiveTo.Equal(boundary) || revised.ID == source.ID || revised.AggregationRole != RoomEnergyRoleSubmeter || revised.ParentAssignmentID != parent.ID || revised.DisplayName != "Dishwasher plug" || revised.PollIntervalSeconds != 20 {
		t.Fatalf("old=%+v revised=%+v", old, revised)
	}

	finiteEnd := start.Add(4 * time.Hour)
	finite, err := store.CreateRoomEnergyAssignment(ctx, RoomEnergyAssignment{RoomID: room.ID, DeviceID: deviceID, ComponentKey: "switch:1", ComponentType: "switch", DisplayName: "Finite", Enabled: true, AggregationRole: RoomEnergyRolePrimary, EffectiveFrom: start, EffectiveTo: &finiteEnd})
	if err != nil {
		t.Fatal(err)
	}
	finiteRevision, err := store.ReviseRoomEnergyAssignment(ctx, finite.ID, RoomEnergyAssignmentRevision{RoomID: room.ID, DisplayName: "Finite revised", Enabled: true, AggregationRole: RoomEnergyRolePrimary, EffectiveFrom: start.Add(2 * time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	if finiteRevision.EffectiveTo == nil || !finiteRevision.EffectiveTo.Equal(finiteEnd) {
		t.Fatalf("finite revision lost original end: %+v", finiteRevision)
	}
}

func TestCannotCloseOrMoveParentWhileSubmeterRemainsActive(t *testing.T) {
	t.Parallel()
	store := openEnergyTestStore(t)
	ctx := context.Background()
	deviceID := createEnergyTestDevice(t, store)
	kitchen := createRoomEnergyTestRoom(t, store, "Kitchen")
	office := createRoomEnergyTestRoom(t, store, "Office")
	start := time.Date(2026, time.July, 21, 10, 0, 0, 0, time.UTC)
	boundary := start.Add(time.Hour)
	parent, err := store.CreateRoomEnergyAssignment(ctx, RoomEnergyAssignment{RoomID: kitchen.ID, DeviceID: deviceID, ComponentKey: "em:0", ComponentType: "em", DisplayName: "Circuit", Enabled: true, AggregationRole: RoomEnergyRolePrimary, EffectiveFrom: start})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.CreateRoomEnergyAssignment(ctx, RoomEnergyAssignment{RoomID: kitchen.ID, DeviceID: deviceID, ComponentKey: "switch:0", ComponentType: "switch", DisplayName: "Appliance", Enabled: true, AggregationRole: RoomEnergyRoleSubmeter, ParentAssignmentID: parent.ID, EffectiveFrom: start}); err != nil {
		t.Fatal(err)
	}
	if err := store.CloseRoomEnergyAssignment(ctx, parent.ID, boundary); !errors.Is(err, ErrRoomEnergyInvalidHierarchy) {
		t.Fatalf("close-parent error = %v", err)
	}
	if _, err := store.MoveRoomEnergyAssignment(ctx, parent.ID, office.ID, boundary); !errors.Is(err, ErrRoomEnergyInvalidHierarchy) {
		t.Fatalf("move-parent error = %v", err)
	}
	loaded, err := store.GetRoomEnergyAssignment(ctx, parent.ID)
	if err != nil || loaded.EffectiveTo != nil {
		t.Fatalf("failed parent operation changed history: %+v err=%v", loaded, err)
	}
}

func TestCloseAndDeleteFutureRoomEnergyAssignments(t *testing.T) {
	t.Parallel()
	store := openEnergyTestStore(t)
	ctx := context.Background()
	deviceID := createEnergyTestDevice(t, store)
	room := createRoomEnergyTestRoom(t, store, "Office")
	now := time.Date(2026, time.July, 21, 10, 0, 0, 0, time.UTC)
	active, err := store.CreateRoomEnergyAssignment(ctx, RoomEnergyAssignment{RoomID: room.ID, DeviceID: deviceID, ComponentKey: "switch:0", ComponentType: "switch", DisplayName: "Active", Enabled: true, AggregationRole: RoomEnergyRolePrimary, EffectiveFrom: now.Add(-time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.CloseRoomEnergyAssignment(ctx, active.ID, now); err != nil {
		t.Fatal(err)
	}
	closed, err := store.GetRoomEnergyAssignment(ctx, active.ID)
	if err != nil || closed.EffectiveTo == nil || !closed.EffectiveTo.Equal(now) || !closed.Enabled {
		t.Fatalf("closed assignment = %+v err=%v", closed, err)
	}

	future, err := store.CreateRoomEnergyAssignment(ctx, RoomEnergyAssignment{RoomID: room.ID, DeviceID: deviceID, ComponentKey: "switch:1", ComponentType: "switch", DisplayName: "Future", Enabled: true, AggregationRole: RoomEnergyRolePrimary, EffectiveFrom: now.Add(time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.DeleteFutureRoomEnergyAssignment(ctx, future.ID, now); err != nil {
		t.Fatal(err)
	}
	if _, err := store.GetRoomEnergyAssignment(ctx, future.ID); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("future assignment still exists: %v", err)
	}
	if err := store.DeleteFutureRoomEnergyAssignment(ctx, active.ID, now); err == nil {
		t.Fatal("historical assignment was deletable")
	}
}

func createRoomEnergyTestRoom(t *testing.T, store *Store, name string) Room {
	t.Helper()
	room, err := store.SaveRoom(context.Background(), Room{Name: name, Icon: "home", SortOrder: 10})
	if err != nil {
		t.Fatal(err)
	}
	return room
}
