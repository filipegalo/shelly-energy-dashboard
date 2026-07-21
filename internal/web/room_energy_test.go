package web

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/filipegalo/shelly-energy-dashboard/internal/database"
	energyservice "github.com/filipegalo/shelly-energy-dashboard/internal/energy"
	"github.com/filipegalo/shelly-energy-dashboard/internal/shelly"
)

type fakeRoomEnergyService struct {
	device           database.Device
	components       []shelly.EnergyComponent
	reading          shelly.EnergyReading
	discoverErr      error
	testErr          error
	testedDeviceID   int64
	testedComponent  string
	reconfigureCalls int
}

func (f *fakeRoomEnergyService) DiscoverMeter(_ context.Context, deviceID int64) (database.Device, []shelly.EnergyComponent, error) {
	if f.discoverErr != nil {
		return database.Device{}, nil, f.discoverErr
	}
	return f.device, append([]shelly.EnergyComponent(nil), f.components...), nil
}

func (f *fakeRoomEnergyService) TestMeter(_ context.Context, deviceID int64, componentKey string) (database.Device, shelly.EnergyComponent, shelly.EnergyReading, error) {
	f.testedDeviceID, f.testedComponent = deviceID, componentKey
	if f.testErr != nil {
		return database.Device{}, shelly.EnergyComponent{}, shelly.EnergyReading{}, f.testErr
	}
	for _, component := range f.components {
		if component.Key == componentKey {
			return f.device, component, f.reading, nil
		}
	}
	return f.device, shelly.EnergyComponent{}, shelly.EnergyReading{}, errors.New("component was not discovered")
}

func (f *fakeRoomEnergyService) Reconfigure(context.Context) error {
	f.reconfigureCalls++
	return nil
}

func (f *fakeRoomEnergyService) SubscribeRoomEnergy() (<-chan energyservice.RoomLiveEvent, func(), error) {
	stream := make(chan energyservice.RoomLiveEvent, 1)
	stream <- energyservice.RoomLiveEvent{Type: energyservice.RoomLiveEventSnapshot}
	return stream, func() {}, nil
}

func (f *fakeRoomEnergyService) RoomLiveSnapshot() energyservice.RoomLiveSnapshot {
	return energyservice.RoomLiveSnapshot{}
}

func TestRoomEnergyStaticRoutesPrecedeRoomIDRoute(t *testing.T) {
	t.Parallel()
	router, store := testRouter(t)
	defer store.Close()

	for _, path := range []string{
		"/energy/rooms",
		"/energy/rooms/settings",
		"/energy/rooms/reconciliation",
	} {
		response := httptest.NewRecorder()
		router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
		if response.Code != http.StatusOK {
			t.Fatalf("GET %s = %d: %s", path, response.Code, response.Body.String())
		}
		if !strings.Contains(response.Body.String(), "Room metering") {
			t.Fatalf("GET %s did not render the room-energy surface: %s", path, response.Body.String())
		}
	}
}

func TestCreateRoomEnergyAssignmentRediscoverTestsExactComponent(t *testing.T) {
	t.Parallel()
	server, store := testServer(t)
	defer store.Close()
	room, device := seedRoomEnergyDevice(t, store)
	power, imported, voltage := 823.0, 412670.0, 231.4
	fake := &fakeRoomEnergyService{
		device: device,
		components: []shelly.EnergyComponent{{
			Key: "switch:0", Type: "switch", Label: "Verified channel",
			Capabilities: shelly.EnergyCapabilities{PowerW: true, ImportedEnergyWh: true, VoltageV: true},
		}},
		reading: shelly.EnergyReading{Timestamp: time.Now().UTC(), PowerW: &power, ImportedEnergyWh: &imported, VoltageV: &voltage},
	}
	server.roomEnergy = fake

	location, err := time.LoadLocation("Europe/Lisbon")
	if err != nil {
		t.Fatal(err)
	}
	effective := time.Now().In(location).Add(-time.Minute).Format("2006-01-02T15:04")
	response := postRoomEnergyForm(t, server.Router(), "/energy/rooms/assignments", url.Values{
		"room_id":               {intString(room.ID)},
		"device_id":             {intString(device.ID)},
		"component_key":         {"switch:0"},
		"component_type":        {"pm"},
		"component_label":       {"Untrusted browser label"},
		"aggregation_role":      {database.RoomEnergyRolePrimary},
		"enabled":               {"on"},
		"poll_interval_seconds": {"10"},
		"effective_preset":      {"custom"},
		"effective_at":          {effective},
	})
	if response.Code != http.StatusSeeOther {
		t.Fatalf("create assignment = %d: %s", response.Code, response.Body.String())
	}
	assignments, err := store.ListRoomEnergyAssignments(context.Background())
	if err != nil || len(assignments) != 1 {
		t.Fatalf("assignments = %+v: %v", assignments, err)
	}
	assignment := assignments[0]
	if assignment.ComponentType != "switch" || assignment.DisplayName != "Verified channel" {
		t.Fatalf("assignment trusted posted metadata: %+v", assignment)
	}
	if fake.testedDeviceID != device.ID || fake.testedComponent != "switch:0" {
		t.Fatalf("tested (%d,%q), want (%d,%q)", fake.testedDeviceID, fake.testedComponent, device.ID, "switch:0")
	}
	if fake.reconfigureCalls != 1 {
		t.Fatalf("collector reconfigure calls = %d, want 1", fake.reconfigureCalls)
	}
}

func TestRoomEnergyChartPreservesMissingValuesAsNull(t *testing.T) {
	t.Parallel()
	server, store := testServer(t)
	defer store.Close()
	room, device := seedRoomEnergyDevice(t, store)
	ctx := context.Background()
	config, err := store.GetEnergyConfig(ctx)
	if err != nil {
		t.Fatal(err)
	}
	config.DeviceID, config.ComponentKey, config.ComponentType = device.ID, "switch:0", "switch"
	config.Enabled = true
	if _, err := store.SaveEnergyConfig(ctx, config); err != nil {
		t.Fatal(err)
	}
	if _, err := store.CreateRoomEnergyAssignment(ctx, database.RoomEnergyAssignment{
		RoomID: room.ID, DeviceID: device.ID, ComponentKey: "switch:1", ComponentType: "switch",
		DisplayName: "Room meter", Enabled: true, AggregationRole: database.RoomEnergyRolePrimary,
		PollIntervalSeconds: 10, EffectiveFrom: time.Now().Add(-48 * time.Hour),
	}); err != nil {
		t.Fatal(err)
	}

	response := httptest.NewRecorder()
	server.Router().ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/energy/rooms/chart?range=24h", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("room chart = %d: %s", response.Code, response.Body.String())
	}
	var payload struct {
		Series []struct {
			Points []struct {
				EnergyKWh *string `json:"energy_kwh"`
			} `json:"points"`
		} `json:"series"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Series) == 0 || len(payload.Series[0].Points) == 0 {
		t.Fatalf("chart has no aligned room points: %s", response.Body.String())
	}
	for _, point := range payload.Series[0].Points {
		if point.EnergyKWh != nil {
			t.Fatalf("missing reading was rendered as %q rather than null", *point.EnergyKWh)
		}
	}
}

func TestRoomAssignmentEffectivePresetUsesConfiguredTimezone(t *testing.T) {
	t.Parallel()
	location, err := time.LoadLocation("Europe/Lisbon")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, time.July, 21, 16, 42, 0, 0, location)
	config := database.EnergyConfig{Timezone: "Europe/Lisbon", BillingCycleStartDay: 11}

	today, err := roomAssignmentEffectiveTime("today", "", config, now)
	if err != nil || today.Hour() != 0 || today.Day() != 21 || today.Location().String() != location.String() {
		t.Fatalf("today preset = %s: %v", today, err)
	}
	cycle, err := roomAssignmentEffectiveTime("billing_cycle", "", config, now)
	if err != nil || cycle.Day() != 11 || cycle.Hour() != 0 {
		t.Fatalf("billing preset = %s: %v", cycle, err)
	}
	custom, err := roomAssignmentEffectiveTime("custom", "2026-07-15T10:30", config, now)
	if err != nil || custom.Day() != 15 || custom.Hour() != 10 || custom.Minute() != 30 || custom.Location().String() != location.String() {
		t.Fatalf("custom preset = %s: %v", custom, err)
	}
}

func seedRoomEnergyDevice(t *testing.T, store *database.Store) (database.Room, database.Device) {
	t.Helper()
	ctx := context.Background()
	network, err := store.SaveNetwork(ctx, database.Network{Name: "Energy", CIDR: "192.168.44.0/24", Enabled: true, ScanIntervalSeconds: 300, ScanConcurrency: 2, ConnectionTimeoutMS: 100, RequestTimeoutMS: 200})
	if err != nil {
		t.Fatal(err)
	}
	room, err := store.SaveRoom(ctx, database.Room{Name: "Kitchen", Icon: "kitchen", SortOrder: 10})
	if err != nil {
		t.Fatal(err)
	}
	created, err := store.UpsertDevice(ctx, network.ID, "192.168.44.20", shelly.DeviceInfo{
		ShellyDeviceID: "room-meter-1", Name: "Room Meter", Model: "SPSW-001PE16EU", Generation: 2,
		Capabilities: []string{"power meters"},
	}, false, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := store.UpdateDeviceRoom(ctx, created.Device.ID, room.ID); err != nil {
		t.Fatal(err)
	}
	device, err := store.GetDevice(ctx, created.Device.ID)
	if err != nil {
		t.Fatal(err)
	}
	return room, device
}

func postRoomEnergyForm(t *testing.T, router http.Handler, path string, values url.Values) *httptest.ResponseRecorder {
	t.Helper()
	seed := httptest.NewRecorder()
	router.ServeHTTP(seed, httptest.NewRequest(http.MethodGet, "/", nil))
	cookies := seed.Result().Cookies()
	if len(cookies) == 0 {
		t.Fatal("CSRF cookie missing")
	}
	values.Set("_csrf", cookies[0].Value)
	request := httptest.NewRequest(http.MethodPost, path, strings.NewReader(values.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.AddCookie(cookies[0])
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	return response
}
