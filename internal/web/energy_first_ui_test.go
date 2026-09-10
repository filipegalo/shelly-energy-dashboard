package web

import (
	"context"
	"encoding/json"
	"html"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/filipegalo/shelly-energy-dashboard/internal/appinfo"
	"github.com/filipegalo/shelly-energy-dashboard/internal/database"
	"github.com/filipegalo/shelly-energy-dashboard/internal/shelly"
)

func TestSidebarGroupsEnergyBeforeShellyManagement(t *testing.T) {
	t.Parallel()
	router, store := testRouter(t)
	defer store.Close()

	body := getPageBody(t, router, "/")
	nav := htmlFragment(t, body, "<nav", "</nav>")
	wantOrder := []string{
		"Energy",
		"Overview",
		"Whole House",
		"Rooms",
		"Reconciliation",
		"Energy Settings",
		"Shelly Management",
		"Devices",
		"Room Setup",
		"Networks",
		"Scan History",
		"System",
		"Settings",
	}
	assertTextInOrder(t, visibleText(nav), wantOrder...)

	wantLinks := map[string]string{
		"/":                            "Overview",
		"/energy":                      "Whole House",
		"/energy/rooms":                "Rooms",
		"/energy/rooms/reconciliation": "Reconciliation",
		"/energy/settings":             "Energy Settings",
		"/devices":                     "Devices",
		"/rooms":                       "Room Setup",
		"/networks":                    "Networks",
		"/scans":                       "Scan History",
		"/settings":                    "Settings",
	}
	for href, wantText := range wantLinks {
		if got := linkText(nav, href); got != wantText {
			t.Errorf("sidebar link %q text = %q, want %q", href, got, wantText)
		}
	}
	if !strings.Contains(body, "Local home energy") {
		t.Error("sidebar does not use the energy-focused product subtitle")
	}
	if !strings.Contains(body, "Version "+appinfo.Current()) {
		t.Error("sidebar does not show the configured application version")
	}
	if !strings.Contains(nav, `href="/" class="active" aria-current="page"`) {
		t.Error("active overview link does not expose aria-current")
	}
}

func TestSidebarShowsApplicationVersion(t *testing.T) {
	t.Parallel()
	server, store := testServer(t)
	defer store.Close()
	server.version = "v1.2.3"

	response := httptest.NewRecorder()
	server.render(response, httptest.NewRequest(http.MethodGet, "/", nil), viewData{
		Page:       "dashboard",
		Title:      "Energy Overview",
		RoomEnergy: &roomEnergyViewModel{},
	})

	if body := response.Body.String(); !strings.Contains(body, `<span class="app-version">Version v1.2.3</span>`) {
		t.Fatalf("sidebar does not show the application version: %s", body)
	}
}

func TestSidebarIsCollapsedWhenRequested(t *testing.T) {
	t.Parallel()
	router, store := testRouter(t)
	defer store.Close()

	if body := getPageBody(t, router, "/?range=24h&sidebar=closed"); !strings.Contains(body, `<div class="shell sidebar-collapsed">`) {
		t.Fatalf("sidebar=closed does not collapse the sidebar: %s", body)
	}
	if body := getPageBody(t, router, "/?range=24h"); strings.Contains(body, "sidebar-collapsed") {
		t.Fatalf("range alone unexpectedly collapses the sidebar: %s", body)
	}
}

func TestDashboardEnergyFirstOnboardingPreservesShellyActions(t *testing.T) {
	t.Parallel()
	router, store := testRouter(t)
	defer store.Close()

	body := getPageBody(t, router, "/")
	for _, want := range []string{
		"Energy Overview",
		"Your home right now",
		"Connect your main Shelly energy meter",
		"Configure energy meters",
		"Energy by room",
		"Shelly devices",
		"Scan for Shelly devices",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("dashboard onboarding does not contain %q", want)
		}
	}
	for _, href := range []string{"/energy/settings", "/energy/rooms", "/devices", "/scans"} {
		if !strings.Contains(body, `href="`+href+`"`) {
			t.Errorf("dashboard does not preserve link to %s", href)
		}
	}
	for _, oldCopy := range []string{"LAN INVENTORY", "Everything on your Shelly network", "Scan now"} {
		if strings.Contains(body, oldCopy) {
			t.Errorf("inventory-first dashboard copy remains prominent: %q", oldCopy)
		}
	}
	if strings.Contains(body, `id="energy-chart"`) {
		t.Error("onboarding dashboard renders an unsupported energy chart")
	}
}

func TestDashboardShowsExistingEnergyReadingAndPrimarySections(t *testing.T) {
	t.Parallel()
	router, store := testRouter(t)
	defer store.Close()
	seedDashboardEnergyReading(t, store, 1234)

	body := getPageBody(t, router, "/")
	for _, want := range []string{
		"Your home right now",
		"Current whole-house power",
		"1.23 kW",
		"Consumption today",
		"Estimated cost today",
		"Current billing cycle",
		"Meter coverage",
		"Energy use and cost chart",
		"Energy by room",
		"Energy health",
		"Shelly devices",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("configured dashboard does not contain %q", want)
		}
	}
	if strings.Contains(body, "LAN INVENTORY") || strings.Contains(body, "Everything on your Shelly network") {
		t.Error("configured dashboard still leads with LAN inventory messaging")
	}
	for _, want := range []string{
		`data-energy-page="/"`,
		`data-refresh-partials="false"`,
		`data-energy-meter-health`,
		`data-energy-freshness-value class="green"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("configured dashboard does not expose live behavior hook %q", want)
		}
	}
}

func seedDashboardEnergyReading(t *testing.T, store *database.Store, powerW float64) {
	t.Helper()
	ctx := context.Background()
	network, err := store.SaveNetwork(ctx, database.Network{
		Name: "Energy network", CIDR: "192.168.80.0/30", Enabled: true,
		ScanIntervalSeconds: 300, ScanConcurrency: 2, ConnectionTimeoutMS: 100, RequestTimeoutMS: 200,
	})
	if err != nil {
		t.Fatal(err)
	}
	created, err := store.UpsertDevice(ctx, network.ID, "192.168.80.1", shelly.DeviceInfo{
		ShellyDeviceID: "whole-house-meter",
		Name:           "Main meter",
		Model:          "Shelly Pro 3EM",
		RawDeviceInfo:  json.RawMessage(`{"id":"whole-house-meter"}`),
	}, false, "")
	if err != nil {
		t.Fatal(err)
	}
	config, err := store.GetEnergyConfig(ctx)
	if err != nil {
		t.Fatal(err)
	}
	config.Enabled = true
	config.DeviceID = created.Device.ID
	config.ComponentKey = "em:0"
	config.ComponentType = "em"
	if _, err := store.SaveEnergyConfig(ctx, config); err != nil {
		t.Fatal(err)
	}
	importedWh := 987654.0
	if _, err := store.InsertEnergySample(ctx, database.EnergySample{
		DeviceID:              created.Device.ID,
		ComponentKey:          "em:0",
		RecordedAt:            time.Now().UTC(),
		PowerW:                &powerW,
		ImportedEnergyTotalWh: &importedWh,
		ReadingQuality:        "measured",
	}); err != nil {
		t.Fatal(err)
	}
}

func getPageBody(t *testing.T, router http.Handler, path string) string {
	t.Helper()
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
	if response.Code != http.StatusOK {
		t.Fatalf("GET %s = %d: %s", path, response.Code, response.Body.String())
	}
	return response.Body.String()
}

func htmlFragment(t *testing.T, body, start, end string) string {
	t.Helper()
	startIndex := strings.Index(body, start)
	if startIndex < 0 {
		t.Fatalf("rendered page has no %q", start)
	}
	endIndex := strings.Index(body[startIndex:], end)
	if endIndex < 0 {
		t.Fatalf("rendered page has no closing %q", end)
	}
	return body[startIndex : startIndex+endIndex+len(end)]
}

func visibleText(fragment string) string {
	var result strings.Builder
	inTag := false
	for _, character := range fragment {
		switch character {
		case '<':
			inTag = true
			result.WriteByte(' ')
		case '>':
			inTag = false
			result.WriteByte(' ')
		default:
			if !inTag {
				result.WriteRune(character)
			}
		}
	}
	return strings.Join(strings.Fields(html.UnescapeString(result.String())), " ")
}

func assertTextInOrder(t *testing.T, text string, values ...string) {
	t.Helper()
	remaining := text
	for _, value := range values {
		index := strings.Index(remaining, value)
		if index < 0 {
			t.Fatalf("%q was not found in order in %q", value, text)
		}
		remaining = remaining[index+len(value):]
	}
}

func linkText(fragment, href string) string {
	needle := `href="` + href + `"`
	start := strings.Index(fragment, needle)
	if start < 0 {
		return ""
	}
	openEnd := strings.Index(fragment[start:], ">")
	if openEnd < 0 {
		return ""
	}
	contentStart := start + openEnd + 1
	closeIndex := strings.Index(fragment[contentStart:], "</a>")
	if closeIndex < 0 {
		return ""
	}
	return visibleText(fragment[contentStart : contentStart+closeIndex])
}
