package web

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"embed"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/filipegalo/shelly-inventory/internal/config"
	"github.com/filipegalo/shelly-inventory/internal/credentials"
	"github.com/filipegalo/shelly-inventory/internal/database"
	energyservice "github.com/filipegalo/shelly-inventory/internal/energy"
	energypricing "github.com/filipegalo/shelly-inventory/internal/energy/pricing"
	"github.com/filipegalo/shelly-inventory/internal/networks"
	"github.com/filipegalo/shelly-inventory/internal/scans"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/shopspring/decimal"
)

//go:embed templates/*.html static
var assets embed.FS

type Server struct {
	store   *database.Store
	manager *scans.Manager
	cfg     config.Config
	logger  *slog.Logger
	cipher  *credentials.Cipher
	views   *template.Template
	metrics http.Handler
	energy  *energyservice.Manager
}

type viewData struct {
	Page        string
	Title       string
	CSRF        string
	Error       string
	Stats       database.DashboardStats
	Networks    []database.Network
	Rooms       []database.Room
	Devices     []database.Device
	Device      database.Device
	Room        database.Room
	Scans       []database.Scan
	Scan        database.Scan
	Credentials []database.CredentialProfile
	Query       map[string]string
	Energy      energyView
}

func New(store *database.Store, manager *scans.Manager, cfg config.Config, logger *slog.Logger, cipher *credentials.Cipher, registryHandler http.Handler) (*Server, error) {
	return NewWithEnergy(store, manager, nil, cfg, logger, cipher, registryHandler)
}

func NewWithEnergy(store *database.Store, manager *scans.Manager, energyManager *energyservice.Manager, cfg config.Config, logger *slog.Logger, cipher *credentials.Cipher, registryHandler http.Handler) (*Server, error) {
	views, err := template.New("app.html").Funcs(template.FuncMap{
		"formatTime": func(value time.Time) string {
			if value.IsZero() {
				return "—"
			}
			return value.Local().Format("2 Jan 2006 15:04")
		},
		"formatTimePtr": func(value *time.Time) string {
			if value == nil {
				return "—"
			}
			return value.Local().Format("2 Jan 2006 15:04")
		},
		"prettyJSON": func(raw string) string {
			var value any
			if json.Unmarshal([]byte(raw), &value) != nil {
				return raw
			}
			output, _ := json.MarshalIndent(value, "", "  ")
			return string(output)
		},
		"eq":          func(a, b any) bool { return fmt.Sprint(a) == fmt.Sprint(b) },
		"statusClass": func(value string) string { return "status status-" + strings.ReplaceAll(value, "_", "-") },
		"slice":       func(values ...string) []string { return values },
		"formatPower": func(value *float64) string {
			if value == nil {
				return "—"
			}
			if *value >= 1000 {
				return fmt.Sprintf("%.2f kW", *value/1000)
			}
			return fmt.Sprintf("%.0f W", *value)
		},
		"formatMeasurement": func(value *float64, unit string) string {
			if value == nil {
				return "—"
			}
			return fmt.Sprintf("%.2f %s", *value, unit)
		},
		"relativeTime": func(value time.Time) string {
			if value.IsZero() {
				return "No readings yet"
			}
			age := time.Since(value)
			if age < time.Minute {
				return fmt.Sprintf("Updated %d seconds ago", max(0, int(age.Seconds())))
			}
			return fmt.Sprintf("Updated %d minutes ago", int(age.Minutes()))
		},
		"formatDate": func(value time.Time) string { return value.Format("2 Jan 2006") },
		"formatEndDate": func(value time.Time) string {
			return value.AddDate(0, 0, -1).Format("2 Jan 2006")
		},
		"percentageDisplay": func(value string) string {
			parsed, err := energypricing.ParseFixedDecimal("VAT rate", value)
			if err != nil {
				return value
			}
			return parsed.Mul(decimal.NewFromInt(100)).String()
		},
	}).ParseFS(assets, "templates/*.html")
	if err != nil {
		return nil, err
	}
	return &Server{store: store, manager: manager, energy: energyManager, cfg: cfg, logger: logger, cipher: cipher, views: views, metrics: registryHandler}, nil
}

func (s *Server) Router() http.Handler {
	router := chi.NewRouter()
	router.Use(middleware.RequestID, middleware.RealIP, middleware.Recoverer, s.hostAndOriginPolicy, s.securityHeaders, s.logRequest, s.csrf)
	staticFS, _ := fs.Sub(assets, "static")
	router.Handle("/static/*", http.StripPrefix("/static/", http.FileServer(http.FS(staticFS))))
	router.Get("/health", func(w http.ResponseWriter, _ *http.Request) {
		jsonResponse(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	router.Get("/ready", s.ready)
	router.Handle("/metrics", s.metrics)
	router.Get("/", s.dashboard)
	router.Get("/devices", s.devicesPage)
	router.Get("/devices/{id}", s.devicePage)
	router.Post("/devices/{id}/rename", s.renameDevice)
	router.Post("/devices/{id}/room", s.assignRoom)
	router.Post("/devices/{id}/credentials", s.assignCredentials)
	router.Post("/devices/{id}/refresh", s.refreshDevice)
	router.Get("/rooms", s.roomsPage)
	router.Get("/rooms/{id}", s.roomPage)
	router.Post("/rooms", s.saveRoom)
	router.Post("/rooms/{id}/update", s.saveRoom)
	router.Post("/rooms/{id}/delete", s.deleteRoom)
	router.Get("/networks", s.networksPage)
	router.Post("/networks", s.saveNetwork)
	router.Post("/networks/{id}/update", s.saveNetwork)
	router.Post("/networks/{id}/delete", s.deleteNetwork)
	router.Post("/networks/{id}/scan", s.startScan)
	router.Post("/discovery/manual", s.manualDiscovery)
	router.Get("/scans", s.scansPage)
	router.Get("/scans/{id}", s.scanPage)
	router.Post("/scans/{id}/cancel", s.cancelScan)
	router.Get("/events/scans/{id}", s.scanEvents)
	router.Get("/settings", s.settingsPage)
	router.Post("/settings/credentials", s.createCredential)
	router.Get("/energy", s.energyPage)
	router.Get("/energy/settings", s.energySettingsPage)
	router.Get("/energy/history", s.energyHistory)
	router.Get("/energy/pricing-preview", s.energyPricingPreview)
	router.Get("/energy/cost-breakdown", s.energyCostBreakdown)
	router.Get("/energy/partials/summary", s.energySummaryPartial)
	router.Get("/energy/partials/chart", s.energyHistory)
	router.Get("/energy/partials/cost-breakdown", s.energyCostBreakdown)
	router.Get("/energy/events", s.energyEvents)
	router.Post("/energy/settings/meter/test", s.testEnergyMeter)
	router.Post("/energy/settings/meter", s.saveEnergyMeter)
	router.Post("/energy/settings/pricing", s.saveEnergyPricing)
	router.Post("/energy/settings/billing-cycle", s.saveEnergyBillingCycle)
	router.Post("/energy/monitoring/enable", s.enableEnergyMonitoring)
	router.Post("/energy/monitoring/disable", s.disableEnergyMonitoring)
	return router
}

func (s *Server) dashboard(w http.ResponseWriter, r *http.Request) {
	stats, err := s.store.DashboardStats(r.Context())
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	devices, _ := s.store.ListDevices(r.Context(), database.DeviceFilter{})
	rooms, _ := s.store.ListRooms(r.Context())
	networkList, _ := s.store.ListNetworks(r.Context(), true)
	scanList, _ := s.store.ListScans(r.Context(), 5)
	if len(devices) > 8 {
		devices = devices[:8]
	}
	s.render(w, r, viewData{Page: "dashboard", Title: "Dashboard", Stats: stats, Devices: devices, Rooms: rooms, Networks: networkList, Scans: scanList})
}

func (s *Server) devicesPage(w http.ResponseWriter, r *http.Request) {
	roomID, _ := strconv.ParseInt(r.URL.Query().Get("room"), 10, 64)
	networkID, _ := strconv.ParseInt(r.URL.Query().Get("network"), 10, 64)
	filter := database.DeviceFilter{Search: r.URL.Query().Get("q"), Status: r.URL.Query().Get("status"), Generation: r.URL.Query().Get("generation"), Model: r.URL.Query().Get("model"), RoomID: roomID, NetworkID: networkID}
	devices, err := s.store.ListDevices(r.Context(), filter)
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	rooms, _ := s.store.ListRooms(r.Context())
	networkList, _ := s.store.ListNetworks(r.Context(), false)
	s.render(w, r, viewData{Page: "devices", Title: "Devices", Devices: devices, Rooms: rooms, Networks: networkList, Query: map[string]string{"q": filter.Search, "status": filter.Status, "generation": filter.Generation, "model": filter.Model, "room": fmt.Sprint(roomID), "network": fmt.Sprint(networkID)}})
}

func (s *Server) devicePage(w http.ResponseWriter, r *http.Request) {
	device, err := s.store.GetDevice(r.Context(), idParam(r))
	if err != nil {
		s.notFound(w, r)
		return
	}
	rooms, _ := s.store.ListRooms(r.Context())
	credentialsList, _ := s.store.ListCredentials(r.Context())
	s.render(w, r, viewData{Page: "device", Title: device.DisplayName(), Device: device, Rooms: rooms, Credentials: credentialsList})
}

func (s *Server) renameDevice(w http.ResponseWriter, r *http.Request) {
	if err := s.store.UpdateDeviceName(r.Context(), idParam(r), strings.TrimSpace(r.FormValue("custom_name"))); err != nil {
		s.formError(w, r, err)
		return
	}
	s.redirect(w, r, "/devices/"+chi.URLParam(r, "id"))
}

func (s *Server) assignRoom(w http.ResponseWriter, r *http.Request) {
	roomID, _ := strconv.ParseInt(r.FormValue("room_id"), 10, 64)
	if roomID < 1 {
		s.formError(w, r, errors.New("select a room"))
		return
	}
	if err := s.store.UpdateDeviceRoom(r.Context(), idParam(r), roomID); err != nil {
		s.formError(w, r, err)
		return
	}
	s.redirect(w, r, "/devices/"+chi.URLParam(r, "id"))
}

func (s *Server) assignCredentials(w http.ResponseWriter, r *http.Request) {
	credentialID, _ := strconv.ParseInt(r.FormValue("credential_id"), 10, 64)
	if err := s.store.UpdateDeviceCredential(r.Context(), idParam(r), credentialID); err != nil {
		s.formError(w, r, err)
		return
	}
	s.redirect(w, r, "/devices/"+chi.URLParam(r, "id"))
}

func (s *Server) refreshDevice(w http.ResponseWriter, r *http.Request) {
	device, err := s.store.GetDevice(r.Context(), idParam(r))
	if err != nil {
		s.notFound(w, r)
		return
	}
	address, err := netip.ParseAddr(device.IPAddress)
	if err != nil {
		s.formError(w, r, errors.New("device has an invalid address"))
		return
	}
	scan, err := s.manager.StartManual(r.Context(), device.NetworkID, address)
	if err != nil {
		s.formError(w, r, err)
		return
	}
	s.redirect(w, r, fmt.Sprintf("/scans/%d", scan.ID))
}

func (s *Server) roomsPage(w http.ResponseWriter, r *http.Request) {
	rooms, err := s.store.ListRooms(r.Context())
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	s.render(w, r, viewData{Page: "rooms", Title: "Rooms", Rooms: rooms})
}

func (s *Server) roomPage(w http.ResponseWriter, r *http.Request) {
	room, err := s.store.GetRoom(r.Context(), idParam(r))
	if err != nil {
		s.notFound(w, r)
		return
	}
	devices, _ := s.store.ListDevices(r.Context(), database.DeviceFilter{RoomID: room.ID})
	s.render(w, r, viewData{Page: "room", Title: room.Name, Room: room, Devices: devices})
}

func (s *Server) saveRoom(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimSpace(r.FormValue("name"))
	if name == "" || len(name) > 100 {
		s.formError(w, r, errors.New("room name is required and must be at most 100 characters"))
		return
	}
	sortOrder, _ := strconv.Atoi(r.FormValue("sort_order"))
	room := database.Room{ID: idParam(r), Name: name, Description: strings.TrimSpace(r.FormValue("description")), Icon: strings.TrimSpace(r.FormValue("icon")), SortOrder: sortOrder}
	if room.Icon == "" {
		room.Icon = "home"
	}
	if _, err := s.store.SaveRoom(r.Context(), room); err != nil {
		s.formError(w, r, err)
		return
	}
	s.redirect(w, r, "/rooms")
}

func (s *Server) deleteRoom(w http.ResponseWriter, r *http.Request) {
	if err := s.store.DeleteRoom(r.Context(), idParam(r)); err != nil {
		s.formError(w, r, err)
		return
	}
	s.redirect(w, r, "/rooms")
}

func (s *Server) networksPage(w http.ResponseWriter, r *http.Request) {
	networkList, err := s.store.ListNetworks(r.Context(), false)
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	s.render(w, r, viewData{Page: "networks", Title: "Networks", Networks: networkList})
}

func (s *Server) saveNetwork(w http.ResponseWriter, r *http.Request) {
	prefix, err := networks.ValidateCIDR(strings.TrimSpace(r.FormValue("cidr")), s.cfg.AllowNonPrivateNetworks, s.cfg.MaxSubnetPrefix)
	if err != nil {
		s.formError(w, r, err)
		return
	}
	name := strings.TrimSpace(r.FormValue("name"))
	if name == "" || len(name) > 100 {
		s.formError(w, r, errors.New("network name is required and must be at most 100 characters"))
		return
	}
	network := database.Network{ID: idParam(r), Name: name, CIDR: prefix.String(), Enabled: formBool(r, "enabled"), AutomaticScanEnabled: formBool(r, "automatic_scan_enabled"), ScanIntervalSeconds: formInt(r, "scan_interval_seconds", 300), ScanConcurrency: formInt(r, "scan_concurrency", s.cfg.DefaultScanConcurrency), ConnectionTimeoutMS: formInt(r, "connection_timeout_ms", int(s.cfg.DefaultConnectionTimeout/time.Millisecond)), RequestTimeoutMS: formInt(r, "request_timeout_ms", int(s.cfg.DefaultRequestTimeout/time.Millisecond))}
	if network.ScanConcurrency < 1 || network.ScanConcurrency > 256 || network.ScanIntervalSeconds < 10 || network.ScanIntervalSeconds > 604800 || network.ConnectionTimeoutMS < 50 || network.ConnectionTimeoutMS > 10000 || network.RequestTimeoutMS < 100 || network.RequestTimeoutMS > 30000 {
		s.formError(w, r, errors.New("network scan settings are outside safe limits"))
		return
	}
	if _, err := s.store.SaveNetwork(r.Context(), network); err != nil {
		s.formError(w, r, err)
		return
	}
	s.redirect(w, r, "/networks")
}

func (s *Server) deleteNetwork(w http.ResponseWriter, r *http.Request) {
	if err := s.store.DeleteNetwork(r.Context(), idParam(r)); err != nil {
		s.formError(w, r, err)
		return
	}
	s.redirect(w, r, "/networks")
}

func (s *Server) startScan(w http.ResponseWriter, r *http.Request) {
	scan, err := s.manager.Start(r.Context(), idParam(r), "manual")
	if err != nil {
		s.formError(w, r, err)
		return
	}
	s.redirect(w, r, fmt.Sprintf("/scans/%d", scan.ID))
}

func (s *Server) manualDiscovery(w http.ResponseWriter, r *http.Request) {
	networkID, _ := strconv.ParseInt(r.FormValue("network_id"), 10, 64)
	target := strings.TrimSpace(r.FormValue("target"))
	if target == "" || strings.ContainsAny(target, "/:@?#") {
		s.formError(w, r, errors.New("enter an IP address or plain hostname without a port or URL"))
		return
	}
	addresses := []netip.Addr{}
	if address, err := netip.ParseAddr(target); err == nil {
		addresses = append(addresses, address)
	} else {
		resolved, err := net.DefaultResolver.LookupNetIP(r.Context(), "ip4", target)
		if err != nil {
			s.formError(w, r, errors.New("hostname could not be resolved"))
			return
		}
		addresses = resolved
	}
	if len(addresses) == 0 {
		s.formError(w, r, errors.New("target has no IPv4 addresses"))
		return
	}
	var scan database.Scan
	var err error
	for _, address := range addresses {
		scan, err = s.manager.StartManual(r.Context(), networkID, address)
		if err == nil {
			break
		}
	}
	if err != nil {
		s.formError(w, r, err)
		return
	}
	s.redirect(w, r, fmt.Sprintf("/scans/%d", scan.ID))
}

func (s *Server) scansPage(w http.ResponseWriter, r *http.Request) {
	scanList, err := s.store.ListScans(r.Context(), 100)
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	s.render(w, r, viewData{Page: "scans", Title: "Scans", Scans: scanList})
}

func (s *Server) scanPage(w http.ResponseWriter, r *http.Request) {
	scan, err := s.store.GetScan(r.Context(), idParam(r))
	if err != nil {
		s.notFound(w, r)
		return
	}
	s.render(w, r, viewData{Page: "scan", Title: fmt.Sprintf("Scan #%d", scan.ID), Scan: scan})
}

func (s *Server) cancelScan(w http.ResponseWriter, r *http.Request) {
	if err := s.manager.Cancel(idParam(r)); err != nil {
		s.formError(w, r, err)
		return
	}
	s.redirect(w, r, "/scans/"+chi.URLParam(r, "id"))
}

func (s *Server) scanEvents(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	stream, unsubscribe, err := s.manager.Subscribe(r.Context(), idParam(r))
	if err != nil {
		s.notFound(w, r)
		return
	}
	defer unsubscribe()
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Accel-Buffering", "no")
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-ticker.C:
			fmt.Fprint(w, ": keepalive\n\n")
			flusher.Flush()
		case scan := <-stream:
			payload, _ := json.Marshal(scan)
			fmt.Fprintf(w, "id: %d\nevent: progress\ndata: %s\n\n", scan.AddressesScanned, payload)
			flusher.Flush()
			if scan.Status != "queued" && scan.Status != "running" {
				return
			}
		}
	}
}

func (s *Server) settingsPage(w http.ResponseWriter, r *http.Request) {
	profiles, _ := s.store.ListCredentials(r.Context())
	s.render(w, r, viewData{Page: "settings", Title: "Settings", Credentials: profiles})
}

func (s *Server) createCredential(w http.ResponseWriter, r *http.Request) {
	if s.cipher == nil {
		s.formError(w, r, errors.New("APP_ENCRYPTION_KEY must be configured before storing credentials"))
		return
	}
	name, username, password := strings.TrimSpace(r.FormValue("name")), strings.TrimSpace(r.FormValue("username")), r.FormValue("password")
	if name == "" || username == "" || password == "" {
		s.formError(w, r, errors.New("name, username, and password are required"))
		return
	}
	encrypted, err := s.cipher.Encrypt(password)
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	if _, err := s.store.CreateCredential(r.Context(), name, username, encrypted); err != nil {
		s.formError(w, r, err)
		return
	}
	s.redirect(w, r, "/settings")
}

func (s *Server) ready(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	if err := s.store.Ready(ctx); err != nil {
		jsonResponse(w, http.StatusServiceUnavailable, map[string]string{"status": "not_ready"})
		return
	}
	jsonResponse(w, http.StatusOK, map[string]string{"status": "ready"})
}

func (s *Server) render(w http.ResponseWriter, r *http.Request, data viewData) {
	data.CSRF = csrfToken(r)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := s.views.ExecuteTemplate(w, "app.html", data); err != nil {
		s.logger.Error("render response", "operation", "template", "error", err)
	}
}

func (s *Server) redirect(w http.ResponseWriter, r *http.Request, location string) {
	if r.Header.Get("HX-Request") == "true" {
		w.Header().Set("HX-Redirect", location)
		w.WriteHeader(http.StatusNoContent)
		return
	}
	http.Redirect(w, r, location, http.StatusSeeOther)
}

func (s *Server) formError(w http.ResponseWriter, r *http.Request, err error) {
	s.logger.Warn("form request rejected", "operation", "form", "error", err)
	http.Error(w, err.Error(), http.StatusBadRequest)
}

func (s *Server) serverError(w http.ResponseWriter, r *http.Request, err error) {
	s.logger.Error("request failed", "operation", r.URL.Path, "error", err)
	http.Error(w, "An internal error occurred.", http.StatusInternalServerError)
}

func (s *Server) notFound(w http.ResponseWriter, _ *http.Request) {
	http.Error(w, "Not found", http.StatusNotFound)
}

func (s *Server) securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self' data:; connect-src 'self'; frame-ancestors 'none'; base-uri 'self'; form-action 'self'")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "same-origin")
		w.Header().Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
		next.ServeHTTP(w, r)
	})
}

func (s *Server) hostAndOriginPolicy(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host, _, validHost := parseAuthority(r.Host)
		if !validHost || !s.hostAllowed(host) {
			http.Error(w, "unrecognized Host header", http.StatusMisdirectedRequest)
			return
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead && r.Method != http.MethodOptions {
			if strings.EqualFold(r.Header.Get("Sec-Fetch-Site"), "cross-site") {
				http.Error(w, "cross-site request rejected", http.StatusForbidden)
				return
			}
			if origin := r.Header.Get("Origin"); origin != "" {
				if !s.originAllowed(r, origin) {
					if s.logger != nil {
						s.logger.Warn("cross-origin request rejected", "operation", "origin_policy", "request_host", boundedHeader(r.Host), "origin", boundedHeader(origin))
					}
					http.Error(w, "cross-origin request rejected", http.StatusForbidden)
					return
				}
			}
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) originAllowed(r *http.Request, rawOrigin string) bool {
	origin, err := url.Parse(rawOrigin)
	if err != nil || (origin.Scheme != "http" && origin.Scheme != "https") || origin.Host == "" || origin.User != nil || origin.Path != "" || origin.RawQuery != "" || origin.Fragment != "" {
		return false
	}
	effectiveScheme := "http"
	if r.TLS != nil || s.cfg.SecureCookies {
		effectiveScheme = "https"
	}
	if origin.Scheme != effectiveScheme {
		return false
	}

	requestHost, requestPort, requestOK := parseAuthority(r.Host)
	originHost, originPort, originOK := parseAuthority(origin.Host)
	if !requestOK || !originOK || requestHost != originHost {
		return false
	}
	return portsMatch(requestPort, originPort, effectiveScheme)
}

func (s *Server) hostAllowed(host string) bool {
	host = normalizeHostname(host)
	for _, candidate := range s.cfg.AllowedHosts {
		normalized := normalizeHostname(candidate)
		if normalized == "*" || normalized == host {
			return true
		}
	}
	return false
}

func parseAuthority(authority string) (string, string, bool) {
	parsed, err := url.Parse("http://" + authority)
	if err != nil || parsed.Host == "" || parsed.User != nil || parsed.Path != "" || parsed.RawQuery != "" || parsed.Fragment != "" || parsed.Hostname() == "" {
		return "", "", false
	}
	return normalizeHostname(parsed.Hostname()), parsed.Port(), true
}

func normalizeHostname(host string) string {
	return strings.TrimSuffix(strings.ToLower(strings.TrimSpace(strings.Trim(host, "[]"))), ".")
}

func portsMatch(requestPort, originPort, originScheme string) bool {
	if requestPort == originPort {
		return true
	}
	defaultPort := "80"
	if originScheme == "https" {
		defaultPort = "443"
	}
	return (requestPort == "" && originPort == defaultPort) || (originPort == "" && requestPort == defaultPort)
}

func boundedHeader(value string) string {
	const maxLength = 256
	if len(value) <= maxLength {
		return value
	}
	return value[:maxLength] + "..."
}

func (s *Server) logRequest(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started := time.Now()
		next.ServeHTTP(w, r)
		s.logger.Info("http request", "operation", "http", "method", r.Method, "path", r.URL.Path, "duration", time.Since(started))
	})
}

type csrfKey struct{}

func (s *Server) csrf(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := ""
		if cookie, err := r.Cookie("shelly_csrf"); err == nil {
			token = cookie.Value
		}
		if token == "" {
			buffer := make([]byte, 32)
			if _, err := rand.Read(buffer); err != nil {
				http.Error(w, "could not create CSRF token", http.StatusInternalServerError)
				return
			}
			token = base64.RawURLEncoding.EncodeToString(buffer)
			http.SetCookie(w, &http.Cookie{Name: "shelly_csrf", Value: token, Path: "/", HttpOnly: true, Secure: s.cfg.SecureCookies || r.TLS != nil, SameSite: http.SameSiteStrictMode, MaxAge: 86400})
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead && r.Method != http.MethodOptions {
			r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
			provided := r.Header.Get("X-CSRF-Token")
			if provided == "" {
				provided = r.FormValue("_csrf")
			}
			if subtle.ConstantTimeCompare([]byte(token), []byte(provided)) != 1 {
				http.Error(w, "invalid CSRF token", http.StatusForbidden)
				return
			}
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), csrfKey{}, token)))
	})
}

func csrfToken(r *http.Request) string {
	value, _ := r.Context().Value(csrfKey{}).(string)
	return value
}
func idParam(r *http.Request) int64 {
	value, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	return value
}
func formBool(r *http.Request, name string) bool {
	value := r.FormValue(name)
	return value == "on" || value == "true" || value == "1"
}
func formInt(r *http.Request, name string, fallback int) int {
	value, err := strconv.Atoi(r.FormValue(name))
	if err != nil {
		return fallback
	}
	return value
}
func jsonResponse(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

var _ = promhttp.Handler
