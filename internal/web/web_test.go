package web

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"image/png"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/filipegalo/shelly-energy-dashboard/internal/config"
	"github.com/filipegalo/shelly-energy-dashboard/internal/credentials"
	"github.com/filipegalo/shelly-energy-dashboard/internal/database"
	"github.com/filipegalo/shelly-energy-dashboard/internal/metrics"
	"github.com/filipegalo/shelly-energy-dashboard/internal/scans"
	"github.com/filipegalo/shelly-energy-dashboard/internal/shelly"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

func TestHealthReadyDashboardAndSecurityHeaders(t *testing.T) {
	t.Parallel()
	router, store := testRouter(t)
	defer store.Close()
	for _, path := range []string{"/health", "/ready", "/", "/devices", "/rooms", "/networks", "/scans", "/settings", "/energy", "/energy/settings"} {
		request := httptest.NewRequest(http.MethodGet, path, nil)
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		if response.Code != http.StatusOK {
			t.Fatalf("GET %s = %d: %s", path, response.Code, response.Body.String())
		}
		if response.Header().Get("Content-Security-Policy") == "" {
			t.Fatalf("GET %s has no CSP", path)
		}
		if response.Header().Get("Referrer-Policy") != "same-origin" {
			t.Fatalf("GET %s Referrer-Policy = %q", path, response.Header().Get("Referrer-Policy"))
		}
		if strings.Contains(response.Body.String(), ">Manage networks<") {
			t.Fatalf("GET %s rendered the redundant global Manage networks action", path)
		}
	}
}

func TestDashboardAdvertisesAndServesFavicon(t *testing.T) {
	t.Parallel()
	router, store := testRouter(t)
	defer store.Close()

	pageResponse := httptest.NewRecorder()
	router.ServeHTTP(pageResponse, httptest.NewRequest(http.MethodGet, "/", nil))
	if pageResponse.Code != http.StatusOK {
		t.Fatalf("GET / = %d: %s", pageResponse.Code, pageResponse.Body.String())
	}
	if !strings.Contains(pageResponse.Body.String(), `<link rel="icon" href="/static/favicon.svg" type="image/svg+xml">`) {
		t.Fatalf("dashboard does not advertise the SVG favicon: %s", pageResponse.Body.String())
	}
	if !strings.Contains(pageResponse.Body.String(), `<link rel="apple-touch-icon" href="/static/apple-touch-icon.png" sizes="180x180">`) {
		t.Fatalf("dashboard does not advertise the Apple touch icon: %s", pageResponse.Body.String())
	}

	iconResponse := httptest.NewRecorder()
	router.ServeHTTP(iconResponse, httptest.NewRequest(http.MethodGet, "/static/favicon.svg", nil))
	if iconResponse.Code != http.StatusOK {
		t.Fatalf("GET /static/favicon.svg = %d: %s", iconResponse.Code, iconResponse.Body.String())
	}
	if contentType := iconResponse.Header().Get("Content-Type"); !strings.Contains(contentType, "image/svg+xml") {
		t.Fatalf("favicon Content-Type = %q, want image/svg+xml", contentType)
	}
	if body := iconResponse.Body.String(); !strings.Contains(body, `<svg`) || !strings.Contains(body, `#ff6037`) {
		t.Fatalf("favicon is not the expected branded SVG: %s", body)
	}

	touchResponse := httptest.NewRecorder()
	router.ServeHTTP(touchResponse, httptest.NewRequest(http.MethodGet, "/static/apple-touch-icon.png", nil))
	if touchResponse.Code != http.StatusOK {
		t.Fatalf("GET /static/apple-touch-icon.png = %d", touchResponse.Code)
	}
	if contentType := touchResponse.Header().Get("Content-Type"); !strings.Contains(contentType, "image/png") {
		t.Fatalf("touch icon Content-Type = %q, want image/png", contentType)
	}
	config, err := png.DecodeConfig(bytes.NewReader(touchResponse.Body.Bytes()))
	if err != nil {
		t.Fatalf("decode Apple touch icon: %v", err)
	}
	if config.Width != 180 || config.Height != 180 {
		t.Fatalf("touch icon dimensions = %dx%d, want 180x180", config.Width, config.Height)
	}
}

func TestEnergyOnboardingAndPricingSettings(t *testing.T) {
	t.Parallel()
	router, store := testRouter(t)
	defer store.Close()

	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/energy", nil))
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "Connect your main Shelly energy meter") {
		t.Fatalf("energy onboarding = %d: %s", response.Code, response.Body.String())
	}

	postForm(t, router, "/energy/settings/billing-cycle", url.Values{"billing_cycle_start_day": {"17"}, "timezone": {"Europe/Lisbon"}, "sample_retention_days": {"7"}, "minute_retention_days": {"90"}, "hourly_retention_days": {"730"}})
	postForm(t, router, "/energy/settings/pricing", url.Values{
		"energy_price": {"0.15"}, "contracted_power_kva": {"6.9"}, "reduced_vat_enabled": {"on"},
		"reduced_vat_rate": {"6"}, "standard_vat_rate": {"23"}, "standard_limit_kwh": {"200"},
		"large_family_limit_kwh": {"300"}, "excise_tax": {"0.001"}, "excise_tax_vat_rate": {"23"},
		"include_excise_tax": {"on"}, "effective_from": {"2026-07-21T16:45"},
	})
	config, err := store.GetEnergyConfig(context.Background())
	if err != nil || config.BillingCycleStartDay != 17 || config.Timezone != "Europe/Lisbon" {
		t.Fatalf("billing config = %+v: %v", config, err)
	}
	versions, err := store.ListPricingVersions(context.Background())
	foundSavedPrice := false
	for _, version := range versions {
		foundSavedPrice = foundSavedPrice || (version.EnergyPricePerKWhExcludingVAT == "0.15" && version.BillingCycleStartDay == 17)
	}
	if err != nil || len(versions) != 3 || !foundSavedPrice {
		t.Fatalf("pricing versions = %+v: %v", versions, err)
	}
}

func TestSelectEnergyComponentKey(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		requested  string
		components []shelly.EnergyComponent
		want       string
	}{
		{
			name:       "explicit choice wins",
			requested:  "emeter:1",
			components: []shelly.EnergyComponent{{Key: "emeter:0"}, {Key: "emeter:1"}, {Key: "emeter:total"}},
			want:       "emeter:1",
		},
		{
			name:       "single discovered component",
			components: []shelly.EnergyComponent{{Key: "em:0"}},
			want:       "em:0",
		},
		{
			name:       "whole house aggregate among phase channels",
			components: []shelly.EnergyComponent{{Key: "emeter:0"}, {Key: "emeter:1"}, {Key: "emeter:2"}, {Key: "emeter:total"}},
			want:       "emeter:total",
		},
		{
			name:       "ambiguous components require user choice",
			components: []shelly.EnergyComponent{{Key: "switch:0"}, {Key: "switch:1"}},
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if got := selectEnergyComponentKey(test.requested, test.components); got != test.want {
				t.Fatalf("selectEnergyComponentKey() = %q, want %q", got, test.want)
			}
		})
	}
}

func TestEnergySettingsRendersAutoSelectedComponent(t *testing.T) {
	t.Parallel()
	server, store := testServer(t)
	defer store.Close()
	power, imported := 1234.0, 5678.0
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/energy/settings", nil)
	server.render(response, request, viewData{
		Page:  "energy-settings",
		Title: "Energy settings",
		Energy: energyView{
			Device:               database.Device{ID: 42},
			Components:           []shelly.EnergyComponent{{Key: "emeter:0", Label: "Phase 0"}, {Key: "emeter:total", Label: "Total three-phase meter"}},
			SelectedComponentKey: "emeter:total",
			TestSuccessful:       true,
			TestComponent:        shelly.EnergyComponent{Key: "emeter:total"},
			TestReading:          shelly.EnergyReading{PowerW: &power, ImportedEnergyWh: &imported},
		},
	})
	body := response.Body.String()
	if !strings.Contains(body, `<option value="emeter:total" selected>`) {
		t.Fatalf("auto-selected aggregate was not retained in component dropdown: %s", body)
	}
	if !strings.Contains(body, "Save tested meter") {
		t.Fatalf("save action not rendered after successful test: %s", body)
	}
	if !strings.Contains(body, `name="enabled" checked`) {
		t.Fatalf("new meter should default background collection to enabled: %s", body)
	}
}

func TestEnergySettingsPreservesPausedCollectionState(t *testing.T) {
	t.Parallel()
	server, store := testServer(t)
	defer store.Close()
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/energy/settings", nil)
	server.render(response, request, viewData{
		Page:  "energy-settings",
		Title: "Energy settings",
		Energy: energyView{
			Configured:     true,
			Config:         database.EnergyConfig{Enabled: false},
			Device:         database.Device{ID: 42},
			TestSuccessful: true,
			TestComponent:  shelly.EnergyComponent{Key: "emeter:total"},
		},
	})
	if body := response.Body.String(); strings.Contains(body, `name="enabled" checked`) {
		t.Fatalf("retesting a paused meter must not silently enable collection: %s", body)
	}
}

func TestEnergyCustomRangeValidation(t *testing.T) {
	t.Parallel()
	config := database.EnergyConfig{BillingCycleStartDay: 11}
	now := time.Date(2026, 7, 21, 12, 0, 0, 0, time.UTC)
	request := httptest.NewRequest(http.MethodGet, "/energy?range=custom&start=2026-07-01&end=2026-07-07", nil)
	start, end, interval, err := energyRange("custom", request, config, now)
	if err != nil || interval != "hour" || start.Day() != 1 || end.Day() != 8 {
		t.Fatalf("custom range = %s..%s %s: %v", start, end, interval, err)
	}
	request = httptest.NewRequest(http.MethodGet, "/energy?range=custom&start=2025-01-01&end=2026-07-07", nil)
	if _, _, _, err := energyRange("custom", request, config, now); err == nil {
		t.Fatal("expected oversized custom range rejection")
	}
}

func TestEnergyPricingPreviewAndEffectiveTimeValidation(t *testing.T) {
	t.Parallel()
	router, store := testRouter(t)
	defer store.Close()
	query := url.Values{
		"energy_price": {"0.15"}, "contracted_power_kva": {"6.9"}, "reduced_vat_enabled": {"on"},
		"reduced_vat_rate": {"6"}, "standard_vat_rate": {"23"}, "standard_limit_kwh": {"200"},
		"large_family_limit_kwh": {"300"}, "excise_tax": {"0.001"}, "excise_tax_vat_rate": {"23"}, "include_excise_tax": {"on"},
	}
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/energy/pricing-preview?"+query.Encode(), nil))
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"allowance_kwh"`) {
		t.Fatalf("pricing preview = %d: %s", response.Code, response.Body.String())
	}
	query.Set("energy_price", "1e-2147483648")
	response = httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/energy/pricing-preview?"+query.Encode(), nil))
	if response.Code != http.StatusUnprocessableEntity {
		t.Fatalf("exponent pricing preview = %d: %s", response.Code, response.Body.String())
	}
	if _, err := parseEffectiveTime("not-a-date", time.UTC); err == nil {
		t.Fatal("invalid effective timestamp was accepted")
	}
	if got := decimalRate("0.5"); got != "0.005" {
		t.Fatalf("0.5%% decimal rate = %s", got)
	}
}

func TestMutationRequiresCSRF(t *testing.T) {
	t.Parallel()
	router, store := testRouter(t)
	defer store.Close()
	request := httptest.NewRequest(http.MethodPost, "/rooms", strings.NewReader("name=Kitchen"))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusForbidden {
		t.Fatalf("POST without CSRF = %d", response.Code)
	}
}

func TestRejectsUnknownHostAndCrossSiteMutation(t *testing.T) {
	t.Parallel()
	router, store := testRouter(t)
	defer store.Close()
	request := httptest.NewRequest(http.MethodGet, "http://evil.invalid/health", nil)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusMisdirectedRequest {
		t.Fatalf("unknown Host = %d", response.Code)
	}
	request = httptest.NewRequest(http.MethodPost, "http://example.com/rooms", strings.NewReader("name=Kitchen"))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("Origin", "https://evil.invalid")
	response = httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusForbidden {
		t.Fatalf("cross-origin POST = %d", response.Code)
	}
	if !strings.Contains(response.Body.String(), "cross-origin request rejected") {
		t.Fatalf("cross-origin POST reached a different guard: %s", response.Body.String())
	}
	request = httptest.NewRequest(http.MethodPost, "http://example.com/rooms", strings.NewReader("name=Kitchen"))
	request.Header.Set("Sec-Fetch-Site", "cross-site")
	response = httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusForbidden || !strings.Contains(response.Body.String(), "cross-site request rejected") {
		t.Fatalf("cross-site POST = %d: %s", response.Code, response.Body.String())
	}
}

func TestOriginPolicyCanonicalizesAllowedOrigins(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name          string
		allowedHosts  []string
		host          string
		origin        string
		secureCookies bool
		tlsRequest    bool
		wantStatus    int
	}{
		{name: "exact origin", allowedHosts: []string{"localhost"}, host: "localhost:8080", origin: "http://localhost:8080", wantStatus: http.StatusNoContent},
		{name: "default port", allowedHosts: []string{"localhost"}, host: "localhost:80", origin: "http://localhost", wantStatus: http.StatusNoContent},
		{name: "trailing dot", allowedHosts: []string{"localhost"}, host: "localhost.:8080", origin: "http://localhost:8080", wantStatus: http.StatusNoContent},
		{name: "HTTP scheme mismatch", allowedHosts: []string{"localhost"}, host: "localhost:8080", origin: "https://localhost:8080", wantStatus: http.StatusForbidden},
		{name: "direct TLS", allowedHosts: []string{"localhost"}, host: "localhost:8080", origin: "https://localhost:8080", tlsRequest: true, wantStatus: http.StatusNoContent},
		{name: "TLS terminated by proxy", allowedHosts: []string{"localhost"}, host: "localhost:8080", origin: "https://localhost:8080", secureCookies: true, wantStatus: http.StatusNoContent},
		{name: "configured alias remains cross-origin", allowedHosts: []string{"localhost", "127.0.0.1"}, host: "localhost:8080", origin: "http://127.0.0.1:8080", wantStatus: http.StatusForbidden},
		{name: "port mismatch", allowedHosts: []string{"localhost"}, host: "localhost:8080", origin: "http://localhost:8081", wantStatus: http.StatusForbidden},
		{name: "unknown origin host", allowedHosts: []string{"localhost"}, host: "localhost:8080", origin: "http://evil.invalid:8080", wantStatus: http.StatusForbidden},
		{name: "opaque origin", allowedHosts: []string{"localhost"}, host: "localhost:8080", origin: "null", wantStatus: http.StatusForbidden},
		{name: "wildcard does not trust another origin", allowedHosts: []string{"*"}, host: "localhost:8080", origin: "http://evil.invalid:8080", wantStatus: http.StatusForbidden},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			server := &Server{cfg: config.Config{AllowedHosts: test.allowedHosts, SecureCookies: test.secureCookies}}
			handler := server.hostAndOriginPolicy(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusNoContent)
			}))
			request := httptest.NewRequest(http.MethodPost, "http://"+test.host+"/networks", nil)
			if test.tlsRequest {
				request.TLS = &tls.ConnectionState{}
			}
			request.Header.Set("Origin", test.origin)
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != test.wantStatus {
				t.Fatalf("status = %d, want %d (%s)", response.Code, test.wantStatus, response.Body.String())
			}
		})
	}
}

func TestDeviceSuppliedHTMLIsEscaped(t *testing.T) {
	t.Parallel()
	router, store := testRouter(t)
	defer store.Close()
	ctx := context.Background()
	network, _ := store.SaveNetwork(ctx, database.Network{Name: "IoT", CIDR: "192.168.1.0/30", Enabled: true, ScanIntervalSeconds: 300, ScanConcurrency: 2, ConnectionTimeoutMS: 100, RequestTimeoutMS: 200})
	created, err := store.UpsertDevice(ctx, network.ID, "192.168.1.1", shelly.DeviceInfo{ShellyDeviceID: "stable", Name: `<script>alert("x")</script>`, Model: "test", RawDeviceInfo: json.RawMessage(`{"id":"stable"}`)}, false, "")
	if err != nil {
		t.Fatal(err)
	}
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/devices/"+intString(created.Device.ID), nil))
	body := response.Body.String()
	if strings.Contains(body, `<script>alert("x")</script>`) {
		t.Fatal("device HTML was rendered as trusted content")
	}
	if !strings.Contains(body, "&lt;script&gt;") {
		t.Fatal("escaped device name not found")
	}
}

func TestRoomNetworkCredentialAndDeviceMutations(t *testing.T) {
	t.Parallel()
	router, store := testRouter(t)
	defer store.Close()
	ctx := context.Background()
	postForm(t, router, "/rooms", url.Values{"name": {"Kitchen"}, "description": {"Food prep"}, "icon": {"kitchen"}, "sort_order": {"4"}})
	rooms, err := store.ListRooms(ctx)
	if err != nil || len(rooms) != 2 {
		t.Fatalf("rooms: %v %v", rooms, err)
	}
	room := rooms[1]
	postForm(t, router, "/networks", url.Values{"name": {"IoT"}, "cidr": {"192.168.120.0/30"}, "enabled": {"on"}, "scan_concurrency": {"2"}, "scan_interval_seconds": {"300"}, "connection_timeout_ms": {"100"}, "request_timeout_ms": {"200"}})
	networks, err := store.ListNetworks(ctx, false)
	if err != nil || len(networks) != 1 {
		t.Fatalf("networks: %v %v", networks, err)
	}
	network := networks[0]
	postForm(t, router, "/settings/credentials", url.Values{"name": {"Admin"}, "username": {"admin"}, "password": {"secret"}})
	profiles, err := store.ListCredentials(ctx)
	if err != nil || len(profiles) != 1 {
		t.Fatalf("credentials: %v %v", profiles, err)
	}
	created, err := store.UpsertDevice(ctx, network.ID, "192.168.120.1", shelly.DeviceInfo{ShellyDeviceID: "mutate", Name: "Relay", Model: "test", RawDeviceInfo: json.RawMessage(`{"id":"mutate"}`)}, false, "")
	if err != nil {
		t.Fatal(err)
	}
	id := intString(created.Device.ID)
	postForm(t, router, "/devices/"+id+"/rename", url.Values{"custom_name": {"Kitchen relay"}})
	postForm(t, router, "/devices/"+id+"/room", url.Values{"room_id": {intString(room.ID)}})
	postForm(t, router, "/devices/"+id+"/credentials", url.Values{"credential_id": {intString(profiles[0].ID)}})
	device, err := store.GetDevice(ctx, created.Device.ID)
	if err != nil || device.CustomName != "Kitchen relay" || device.RoomID != room.ID || device.CredentialProfileID != profiles[0].ID {
		t.Fatalf("device mutations: %+v %v", device, err)
	}
	postForm(t, router, "/rooms/"+intString(room.ID)+"/update", url.Values{"name": {"Kitchen 2"}, "description": {"Updated"}, "icon": {"food"}, "sort_order": {"2"}})
	updated, err := store.GetRoom(ctx, room.ID)
	if err != nil || updated.Name != "Kitchen 2" {
		t.Fatalf("updated room: %+v %v", updated, err)
	}
}

func testRouter(t *testing.T) (http.Handler, *database.Store) {
	t.Helper()
	server, store := testServer(t)
	return server.Router(), store
}

func testServer(t *testing.T) (*Server, *database.Store) {
	t.Helper()
	store, err := database.Open(filepath.Join(t.TempDir(), "web.db"))
	if err != nil {
		t.Fatal(err)
	}
	cfg := config.Config{MaxSubnetPrefix: 16, MaxHosts: 65534, DefaultScanConcurrency: 2, DefaultConnectionTimeout: 100 * time.Millisecond, DefaultRequestTimeout: 200 * time.Millisecond, OfflineFailureThreshold: 3, AllowedHosts: []string{"example.com"}}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	metricSet := metrics.New()
	cipher, _ := credentials.NewCipher([]byte("0123456789abcdef0123456789abcdef"))
	manager := scans.NewManager(context.Background(), store, cfg, logger, metricSet, cipher)
	server, err := New(store, manager, cfg, logger, cipher, promhttp.HandlerFor(metricSet.Registry, promhttp.HandlerOpts{}))
	if err != nil {
		t.Fatal(err)
	}
	return server, store
}

func intString(value int64) string {
	const digits = "0123456789"
	if value == 0 {
		return "0"
	}
	buffer := make([]byte, 0, 20)
	for value > 0 {
		buffer = append([]byte{digits[value%10]}, buffer...)
		value /= 10
	}
	return string(buffer)
}

func postForm(t *testing.T, router http.Handler, path string, values url.Values) {
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
	if response.Code != http.StatusSeeOther && response.Code != http.StatusNoContent {
		t.Fatalf("POST %s = %d: %s", path, response.Code, response.Body.String())
	}
}
