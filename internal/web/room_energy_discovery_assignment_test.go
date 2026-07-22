package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/filipegalo/shelly-energy-dashboard/internal/database"
	"github.com/filipegalo/shelly-energy-dashboard/internal/shelly"
)

func TestRoomEnergyDiscoveryDistinguishesAssignedAndAvailableComponents(t *testing.T) {
	t.Parallel()
	server, store := testServer(t)
	defer store.Close()
	room, device := seedRoomEnergyDevice(t, store)
	now := time.Now().UTC()

	for _, assignment := range []database.RoomEnergyAssignment{
		{
			RoomID: room.ID, DeviceID: device.ID, ComponentKey: "switch:0", ComponentType: "switch",
			DisplayName: "Kitchen circuit", Enabled: true, AggregationRole: database.RoomEnergyRolePrimary,
			PollIntervalSeconds: 10, EffectiveFrom: now.Add(-time.Hour),
		},
		{
			RoomID: room.ID, DeviceID: device.ID, ComponentKey: "switch:1", ComponentType: "switch",
			DisplayName: "Future excluded load", Enabled: true, AggregationRole: database.RoomEnergyRoleExcluded,
			PollIntervalSeconds: 10, EffectiveFrom: now.Add(time.Hour),
		},
		{
			RoomID: room.ID, DeviceID: device.ID, ComponentKey: "switch:2", ComponentType: "switch",
			DisplayName: "Paused circuit", Enabled: false, AggregationRole: database.RoomEnergyRolePrimary,
			PollIntervalSeconds: 10, EffectiveFrom: now.Add(-time.Hour),
		},
		{
			RoomID: room.ID, DeviceID: device.ID, ComponentKey: "switch:3", ComponentType: "switch",
			DisplayName: "Historical circuit", Enabled: true, AggregationRole: database.RoomEnergyRolePrimary,
			PollIntervalSeconds: 10, EffectiveFrom: now.Add(-3 * time.Hour), EffectiveTo: timePointer(now.Add(-2 * time.Hour)),
		},
	} {
		if _, err := store.CreateRoomEnergyAssignment(context.Background(), assignment); err != nil {
			t.Fatal(err)
		}
	}

	server.roomEnergy = &fakeRoomEnergyService{
		device: device,
		components: []shelly.EnergyComponent{
			{Key: "switch:0", Type: "switch", Label: "Channel zero", Capabilities: shelly.EnergyCapabilities{PowerW: true}},
			{Key: "switch:1", Type: "switch", Label: "Channel one", Capabilities: shelly.EnergyCapabilities{PowerW: true}},
			{Key: "switch:2", Type: "switch", Label: "Paused channel", Capabilities: shelly.EnergyCapabilities{PowerW: true}},
			{Key: "switch:3", Type: "switch", Label: "Historical channel", Capabilities: shelly.EnergyCapabilities{PowerW: true}},
			{Key: "switch:4", Type: "switch", Label: "Available channel", Capabilities: shelly.EnergyCapabilities{PowerW: true}},
		},
	}

	body := postRoomEnergyDiscovery(t, server.Router(), device.ID)
	for _, test := range []struct {
		label string
		key   string
		role  string
		state string
	}{
		{label: "Channel zero", key: "switch:0", role: "primary", state: "active"},
		{label: "Channel one", key: "switch:1", role: "excluded", state: "future"},
		{label: "Paused channel", key: "switch:2", role: "primary", state: "disabled"},
	} {
		component := discoveryComponent(t, body, test.label)
		componentText := strings.ToLower(visibleText(component))
		for _, want := range []string{room.Name, test.role, test.state} {
			if !strings.Contains(componentText, strings.ToLower(want)) {
				t.Errorf("assigned %s discovery result does not identify %q", test.key, want)
			}
		}
		if hasAssignmentCreationForm(component, test.key) {
			t.Errorf("assigned component %s still renders a new assignment creation form", test.key)
		}
		if test.state == "future" && strings.Contains(componentText, "already assigned") {
			t.Error("future component is described as already assigned rather than scheduled/configured")
		}
	}
	available := discoveryComponent(t, body, "Available channel")
	if !hasAssignmentCreationForm(available, "switch:4") {
		t.Error("unassigned component switch:4 no longer renders its assignment creation form")
	}
	historical := discoveryComponent(t, body, "Historical channel")
	if !hasAssignmentCreationForm(historical, "switch:3") {
		t.Error("historical-only component switch:3 does not render a new assignment creation form")
	}
	if count := strings.Count(body, "Assign to a room"); count != 2 {
		t.Errorf("Assign to a room actions = %d, want 2 for historical-only and unassigned components", count)
	}
}

func TestRoomEnergySettingsAllowsDisabledAssignmentRevision(t *testing.T) {
	t.Parallel()
	server, store := testServer(t)
	defer store.Close()
	room, device := seedRoomEnergyDevice(t, store)
	assignment, err := store.CreateRoomEnergyAssignment(context.Background(), database.RoomEnergyAssignment{
		RoomID: room.ID, DeviceID: device.ID, ComponentKey: "switch:0", ComponentType: "switch",
		DisplayName: "Paused circuit", Enabled: false, AggregationRole: database.RoomEnergyRolePrimary,
		PollIntervalSeconds: 10, EffectiveFrom: time.Now().UTC().Add(-time.Hour),
	})
	if err != nil {
		t.Fatal(err)
	}

	response := httptest.NewRecorder()
	server.Router().ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/energy/rooms/settings", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("settings = %d: %s", response.Code, response.Body.String())
	}
	assignmentStart := `id="room-energy-assignment-` + intString(assignment.ID) + `"`
	assignmentBody := htmlFragment(t, response.Body.String(), assignmentStart, "</details>")
	if !strings.Contains(visibleText(assignmentBody), "Revise future settings") {
		t.Error("disabled assignment does not expose a revision action for re-enabling collection")
	}
}

func timePointer(value time.Time) *time.Time {
	return &value
}

func postRoomEnergyDiscovery(t *testing.T, router http.Handler, deviceID int64) string {
	t.Helper()
	seed := httptest.NewRecorder()
	router.ServeHTTP(seed, httptest.NewRequest(http.MethodGet, "/", nil))
	cookies := seed.Result().Cookies()
	if len(cookies) == 0 {
		t.Fatal("CSRF cookie missing")
	}
	values := url.Values{"_csrf": {cookies[0].Value}}
	request := httptest.NewRequest(http.MethodPost, "/energy/rooms/devices/"+intString(deviceID)+"/components/discover", strings.NewReader(values.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("HX-Request", "true")
	request.AddCookie(cookies[0])
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("component discovery = %d: %s", response.Code, response.Body.String())
	}
	return response.Body.String()
}

func discoveryComponent(t *testing.T, body, label string) string {
	t.Helper()
	labelIndex := strings.Index(body, label)
	if labelIndex < 0 {
		t.Fatalf("discovery result does not contain component %q", label)
	}
	start := strings.LastIndex(body[:labelIndex], "<article")
	if start < 0 {
		t.Fatalf("component %q is not rendered in an article", label)
	}
	end := strings.Index(body[labelIndex:], "</article>")
	if end < 0 {
		t.Fatalf("component %q article is not closed", label)
	}
	return body[start : labelIndex+end+len("</article>")]
}

func hasAssignmentCreationForm(body, componentKey string) bool {
	for remaining := body; ; {
		start := strings.Index(remaining, "<form")
		if start < 0 {
			return false
		}
		remaining = remaining[start:]
		end := strings.Index(remaining, "</form>")
		if end < 0 {
			return false
		}
		form := remaining[:end+len("</form>")]
		if strings.Contains(form, `action="/energy/rooms/assignments"`) &&
			strings.Contains(form, `name="component_key"`) &&
			strings.Contains(form, `value="`+componentKey+`"`) {
			return true
		}
		remaining = remaining[end+len("</form>"):]
	}
}
