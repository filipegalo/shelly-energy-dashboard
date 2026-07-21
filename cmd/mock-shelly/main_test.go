package main

import (
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRoomProfilesExposeDeterministicComponents(t *testing.T) {
	t.Parallel()
	tests := []struct {
		profile    string
		components []string
		wantDaily  float64
	}{
		{profile: "room-main3em", components: []string{"em:0", "emdata:0"}, wantDaily: 30},
		{profile: "room-pro4pm", components: []string{"switch:0", "switch:1", "switch:2", "switch:3"}, wantDaily: 21},
		{profile: "room-em2", components: []string{"em1:0", "em1data:0", "em1:1", "em1data:1"}, wantDaily: 15},
		{profile: "room-plug-dishwasher", components: []string{"switch:0"}, wantDaily: 3},
		{profile: "room-plug-refrigerator", components: []string{"switch:0"}, wantDaily: 2},
		{profile: "room-plug-living", components: []string{"switch:0"}, wantDaily: 5},
		{profile: "room-plug-office", components: []string{"switch:0"}, wantDaily: 4},
		{profile: "room-plug-garage", components: []string{"switch:0"}, wantDaily: 2},
		{profile: "reconcile-below", components: []string{"switch:0", "switch:1", "switch:2", "switch:3"}, wantDaily: 21},
		{profile: "reconcile-slight-over", components: []string{"switch:0", "switch:1", "switch:2", "switch:3"}, wantDaily: 30.6},
		{profile: "reconcile-significant-over", components: []string{"switch:0", "switch:1", "switch:2", "switch:3"}, wantDaily: 36},
	}

	for _, test := range tests {
		test := test
		t.Run(test.profile, func(t *testing.T) {
			t.Parallel()
			response := serve(t, newMockHandler(test.profile), "/rpc/Shelly.GetStatus")
			if response.Code != http.StatusOK {
				t.Fatalf("status = %d: %s", response.Code, response.Body.String())
			}
			var payload map[string]any
			decodeJSON(t, response, &payload)
			for _, component := range test.components {
				if _, ok := payload[component]; !ok {
					t.Errorf("component %q missing from %v", component, payload)
				}
			}
			if got := dailyKWhFromStatus(payload); math.Abs(got-test.wantDaily) > 0.000001 {
				t.Fatalf("daily energy represented by power = %.6f kWh, want %.6f", got, test.wantDaily)
			}
		})
	}
}

func TestRoomProfileCountersAdvanceByTenSecondDailyProportion(t *testing.T) {
	t.Parallel()
	handler := newMockHandler("room-pro4pm")
	first := switchTotal(t, serve(t, handler, "/rpc/Switch.GetStatus?id=0"))
	second := switchTotal(t, serve(t, handler, "/rpc/Switch.GetStatus?id=0"))
	want := kitchenDailyKWh * 1000 / readingsPerDay
	if got := second - first; math.Abs(got-want) > 0.000001 {
		t.Fatalf("counter delta = %.9f Wh, want %.9f", got, want)
	}

	mainHandler := newMockHandler("room-main3em")
	mainFirst := totalActiveEnergy(t, serve(t, mainHandler, "/rpc/EMData.GetStatus?id=0"))
	mainSecond := totalActiveEnergy(t, serve(t, mainHandler, "/rpc/EMData.GetStatus?id=0"))
	mainWant := wholeHouseDailyKWh * 1000 / readingsPerDay
	if got := mainSecond - mainFirst; math.Abs(got-mainWant) > 0.000001 {
		t.Fatalf("main counter delta = %.9f Wh, want %.9f", got, mainWant)
	}
}

func TestKitchenApplianceFixturesRemainSubmeterSized(t *testing.T) {
	t.Parallel()
	roomDailyKWh := kitchenDailyKWh + livingRoomDailyKWh + officeDailyKWh + garageDailyKWh
	if roomDailyKWh != 21 || wholeHouseDailyKWh-roomDailyKWh != unmeteredDailyKWh {
		t.Fatalf("baseline reconciliation = house %v - rooms %v = %v, want unmetered %v", wholeHouseDailyKWh, roomDailyKWh, wholeHouseDailyKWh-roomDailyKWh, unmeteredDailyKWh)
	}
	if kitchenDailyKWh != 10 || dishwasherDailyKWh != 3 || refrigeratorDailyKWh != 2 {
		t.Fatalf("unexpected Kitchen fixture proportions: circuit=%v dishwasher=%v refrigerator=%v", kitchenDailyKWh, dishwasherDailyKWh, refrigeratorDailyKWh)
	}
	if otherKitchenDailyKWh := kitchenDailyKWh - dishwasherDailyKWh - refrigeratorDailyKWh; otherKitchenDailyKWh != 5 {
		t.Fatalf("other Kitchen circuit usage = %v kWh, want 5", otherKitchenDailyKWh)
	}
	if incorrectlySummed := kitchenDailyKWh + dishwasherDailyKWh + refrigeratorDailyKWh; incorrectlySummed != 15 {
		t.Fatalf("double-count fixture total = %v kWh, want 15", incorrectlySummed)
	}
}

func TestRoomMockRejectsUnknownComponentsAndRPCMethods(t *testing.T) {
	t.Parallel()
	handler := newMockHandler("room-pro4pm")
	for _, test := range []struct {
		path string
		want int
	}{
		{path: "/rpc/Switch.GetStatus?id=99", want: http.StatusNotFound},
		{path: "/rpc/Switch.GetStatus?id=bogus", want: http.StatusBadRequest},
		{path: "/rpc/Switch.GetStatus", want: http.StatusBadRequest},
		{path: "/rpc/Switch.GetStatus?id=0&id=1", want: http.StatusBadRequest},
		{path: "/rpc/Switch.GetStatus?id=0&method=evil", want: http.StatusBadRequest},
		{path: "/rpc/Anything.Run?id=0", want: http.StatusNotFound},
		{path: "/rpc/EM.GetStatus?id=0", want: http.StatusNotFound},
	} {
		response := serve(t, handler, test.path)
		if response.Code != test.want {
			t.Errorf("GET %s = %d, want %d: %s", test.path, response.Code, test.want, response.Body.String())
		}
	}
}

func TestRoomOfflineStaleAndResetBehaviors(t *testing.T) {
	t.Parallel()

	offline := newMockHandler("room-source-offline")
	for range 2 {
		if response := serve(t, offline, "/rpc/Switch.GetStatus?id=0"); response.Code != http.StatusOK {
			t.Fatalf("offline fixture warm-up = %d", response.Code)
		}
	}
	if response := serve(t, offline, "/shelly"); response.Code != http.StatusServiceUnavailable {
		t.Fatalf("offline fixture after warm-up = %d, want 503", response.Code)
	}

	stale := newMockHandler("room-source-stale")
	for range 2 {
		if response := serve(t, stale, "/rpc/Switch.GetStatus?id=0"); response.Code != http.StatusOK {
			t.Fatalf("stale fixture warm-up = %d", response.Code)
		}
	}
	if response := serve(t, stale, "/rpc/Switch.GetStatus?id=0"); response.Code != http.StatusServiceUnavailable {
		t.Fatalf("stale fixture after warm-up = %d, want 503", response.Code)
	}
	if response := serve(t, stale, "/shelly"); response.Code != http.StatusOK {
		t.Fatalf("stale fixture identity = %d, want online identity", response.Code)
	}

	reset := newMockHandler("room-source-reset")
	first := switchTotal(t, serve(t, reset, "/rpc/Switch.GetStatus?id=0"))
	_ = switchTotal(t, serve(t, reset, "/rpc/Switch.GetStatus?id=0"))
	third := switchTotal(t, serve(t, reset, "/rpc/Switch.GetStatus?id=0"))
	if third >= first {
		t.Fatalf("reset fixture counter did not roll back: first=%v third=%v", first, third)
	}
}

func TestLegacyProfilesRemainAvailable(t *testing.T) {
	t.Parallel()
	for _, profile := range []string{"gen1", "gen2", "gen3", "auth", "energy", "energy-reset", "energy-invalid", "energy-slow", "energy-flaky"} {
		profile := profile
		t.Run(profile, func(t *testing.T) {
			t.Parallel()
			response := serve(t, newMockHandler(profile), "/shelly")
			if response.Code != http.StatusOK {
				t.Fatalf("GET /shelly = %d: %s", response.Code, response.Body.String())
			}
		})
	}
}

func TestFixedSystemAndLegacyEnergyEndpoints(t *testing.T) {
	t.Parallel()

	gen1 := newMockHandler("gen1")
	for _, path := range []string{"/settings", "/status", "/rpc/Sys.GetStatus"} {
		if response := serve(t, gen1, path); response.Code != http.StatusOK {
			t.Errorf("GET %s = %d: %s", path, response.Code, response.Body.String())
		}
	}
	if response := serveMethod(t, gen1, http.MethodPost, "/settings", nil); response.Code != http.StatusMethodNotAllowed || response.Header().Get("Allow") != http.MethodGet {
		t.Fatalf("POST /settings = %d Allow=%q", response.Code, response.Header().Get("Allow"))
	}

	gen2 := newMockHandler("gen2")
	for _, path := range []string{"/rpc/Sys.GetConfig", "/rpc/Shelly.GetDeviceInfo", "/rpc/Shelly.GetStatus"} {
		if response := serve(t, gen2, path); response.Code != http.StatusOK {
			t.Errorf("GET %s = %d: %s", path, response.Code, response.Body.String())
		}
	}

	unsupported := newMockHandler("unsupported")
	for _, path := range []string{"/rpc/Sys.GetConfig", "/rpc/Shelly.GetDeviceInfo"} {
		if response := serve(t, unsupported, path); response.Code != http.StatusNotFound {
			t.Errorf("unsupported GET %s = %d", path, response.Code)
		}
	}

	auth := newMockHandler("auth")
	if response := serve(t, auth, "/rpc/Sys.GetConfig"); response.Code != http.StatusUnauthorized || response.Header().Get("WWW-Authenticate") == "" {
		t.Fatalf("Digest challenge = %d header=%q", response.Code, response.Header().Get("WWW-Authenticate"))
	}
	if response := serveMethod(t, auth, http.MethodGet, "/rpc/Sys.GetConfig", map[string]string{"Authorization": "Digest mock-response"}); response.Code != http.StatusOK {
		t.Fatalf("authorized config = %d: %s", response.Code, response.Body.String())
	}

	energy := newMockHandler("energy")
	for _, path := range []string{"/rpc/Shelly.GetStatus", "/rpc/EM.GetStatus?id=0", "/rpc/EMData.GetStatus?id=0"} {
		if response := serve(t, energy, path); response.Code != http.StatusOK {
			t.Errorf("energy GET %s = %d: %s", path, response.Code, response.Body.String())
		}
	}
	invalid := newMockHandler("energy-invalid")
	if response := serve(t, invalid, "/rpc/EM.GetStatus?id=0"); response.Code != http.StatusOK || !containsJSONError(response.Body.String(), "ct_type_not_set") {
		t.Fatalf("invalid energy response = %d: %s", response.Code, response.Body.String())
	}
	reset := newMockHandler("energy-reset")
	for range 3 {
		_ = totalActiveEnergy(t, serve(t, reset, "/rpc/EMData.GetStatus?id=0"))
	}
	if got := totalActiveEnergy(t, serve(t, reset, "/rpc/EMData.GetStatus?id=0")); got != 10 {
		t.Fatalf("legacy reset counter after rollover = %v, want 10", got)
	}

	malformed := serve(t, newMockHandler("malformed"), "/shelly")
	if malformed.Code != http.StatusOK || json.Valid(malformed.Body.Bytes()) {
		t.Fatalf("malformed profile = %d %q", malformed.Code, malformed.Body.String())
	}
	if response := serve(t, newMockHandler("offline"), "/shelly"); response.Code != http.StatusServiceUnavailable {
		t.Fatalf("offline legacy profile = %d, want 503", response.Code)
	}
}

func TestFixedRoomComponentEndpoints(t *testing.T) {
	t.Parallel()

	mainMeter := newMockHandler("room-main3em")
	for _, path := range []string{"/rpc/Sys.GetConfig", "/rpc/Shelly.GetDeviceInfo", "/rpc/EM.GetStatus?id=0", "/rpc/EMData.GetStatus?id=0", "/rpc/Sys.GetStatus"} {
		if response := serve(t, mainMeter, path); response.Code != http.StatusOK {
			t.Errorf("main meter GET %s = %d: %s", path, response.Code, response.Body.String())
		}
	}

	twoChannel := newMockHandler("room-em2")
	for _, path := range []string{"/rpc/EM1.GetStatus?id=0", "/rpc/EM1Data.GetStatus?id=0", "/rpc/EM1.GetStatus?id=1", "/rpc/EM1Data.GetStatus?id=1"} {
		if response := serve(t, twoChannel, path); response.Code != http.StatusOK {
			t.Errorf("two-channel GET %s = %d: %s", path, response.Code, response.Body.String())
		}
	}

	fixture := roomProfile("test-fixed-components", "TEST", "PM", "AABBCCDDEEFE", "Fixed component test", []componentFixture{
		energyFixture("pm", 0, 1.5, false),
		energyFixture("em", 1, 2.5, false),
	})
	state := &mockState{profile: "test-fixed-components", fixture: &fixture, componentSteps: map[string]uint64{}}
	mux := http.NewServeMux()
	state.register(mux)
	for _, path := range []string{"/rpc/PM.GetStatus?id=0", "/rpc/PMData.GetStatus?id=0", "/rpc/EM.GetStatus?id=1", "/rpc/EMData.GetStatus?id=1"} {
		if response := serve(t, mux, path); response.Code != http.StatusOK {
			t.Errorf("fixed component GET %s = %d: %s", path, response.Code, response.Body.String())
		}
	}
	if response := serve(t, newMockHandler("gen2"), "/rpc/PM.GetStatus?id=0"); response.Code != http.StatusNotFound {
		t.Fatalf("unadvertised PM endpoint = %d, want 404", response.Code)
	}
}

func TestEnvironmentValue(t *testing.T) {
	t.Setenv("MOCK_TEST_VALUE", "configured")
	if got := env("MOCK_TEST_VALUE", "fallback"); got != "configured" {
		t.Fatalf("env configured = %q", got)
	}
	t.Setenv("MOCK_TEST_VALUE", "")
	if got := env("MOCK_TEST_VALUE", "fallback"); got != "fallback" {
		t.Fatalf("env fallback = %q", got)
	}
}

func serve(t *testing.T, handler http.Handler, target string) *httptest.ResponseRecorder {
	t.Helper()
	return serveMethod(t, handler, http.MethodGet, target, nil)
}

func serveMethod(t *testing.T, handler http.Handler, method, target string, headers map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(method, target, nil)
	for name, value := range headers {
		request.Header.Set(name, value)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

func containsJSONError(raw, wanted string) bool {
	var payload map[string]any
	if json.Unmarshal([]byte(raw), &payload) != nil {
		return false
	}
	values, _ := payload["errors"].([]any)
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}

func decodeJSON(t *testing.T, response *httptest.ResponseRecorder, destination any) {
	t.Helper()
	if err := json.Unmarshal(response.Body.Bytes(), destination); err != nil {
		t.Fatalf("decode %q: %v", response.Body.String(), err)
	}
}

func dailyKWhFromStatus(payload map[string]any) float64 {
	totalPower := 0.0
	for key, raw := range payload {
		component, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		switch {
		case len(key) >= 7 && key[:7] == "switch:":
			totalPower += numeric(component["apower"])
		case len(key) >= 4 && key[:4] == "em1:":
			totalPower += numeric(component["act_power"])
		case len(key) >= 3 && key[:3] == "em:":
			totalPower += numeric(component["total_act_power"])
		}
	}
	return totalPower * 24 / 1000
}

func switchTotal(t *testing.T, response *httptest.ResponseRecorder) float64 {
	t.Helper()
	if response.Code != http.StatusOK {
		t.Fatalf("switch response = %d: %s", response.Code, response.Body.String())
	}
	var payload struct {
		Energy struct {
			Total float64 `json:"total"`
		} `json:"aenergy"`
	}
	decodeJSON(t, response, &payload)
	return payload.Energy.Total
}

func totalActiveEnergy(t *testing.T, response *httptest.ResponseRecorder) float64 {
	t.Helper()
	if response.Code != http.StatusOK {
		t.Fatalf("energy-data response = %d: %s", response.Code, response.Body.String())
	}
	var payload struct {
		Total float64 `json:"total_act"`
	}
	decodeJSON(t, response, &payload)
	return payload.Total
}

func numeric(value any) float64 {
	number, _ := value.(float64)
	return number
}
