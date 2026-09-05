package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/filipegalo/shelly-energy-dashboard/internal/config"
	"github.com/filipegalo/shelly-energy-dashboard/internal/credentials"
	"github.com/filipegalo/shelly-energy-dashboard/internal/database"
	energyservice "github.com/filipegalo/shelly-energy-dashboard/internal/energy"
	"github.com/filipegalo/shelly-energy-dashboard/internal/metrics"
	"github.com/filipegalo/shelly-energy-dashboard/internal/scans"
	"github.com/filipegalo/shelly-energy-dashboard/internal/scheduler"
	"github.com/filipegalo/shelly-energy-dashboard/internal/web"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "healthcheck" {
		healthcheck()
		return
	}
	cfg, err := config.Load()
	if err != nil {
		fatal("load configuration", err)
	}
	logger := newLogger(cfg.LogLevel)
	store, err := database.Open(database.Options{
		SQLitePath: cfg.DatabasePath,
		Postgres: database.PostgresOptions{
			Username: cfg.DatabaseUsername, Password: cfg.DatabasePassword, Hostname: cfg.DatabaseHostname, Port: cfg.DatabasePort, Database: cfg.DatabaseDB, SSLMode: cfg.DatabaseSSLMode,
		},
	})
	if err != nil {
		fatal("open database", err)
	}
	defer store.Close()

	var cipher *credentials.Cipher
	if len(cfg.EncryptionKey) > 0 {
		cipher, err = credentials.NewCipher(cfg.EncryptionKey)
		if err != nil {
			fatal("configure credential encryption", err)
		}
	}

	appCtx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	metricSet := metrics.New()
	registerInventoryGauges(metricSet, store)
	manager := scans.NewManager(appCtx, store, cfg, logger, metricSet, cipher)
	energyManager := energyservice.NewManager(appCtx, store, cfg, logger, metricSet, cipher)
	if err := energyManager.Start(appCtx); err != nil {
		logger.Error("start energy collector", "operation", "energy_startup", "error", err)
	}
	scheduled := scheduler.New(store, manager, logger)
	go scheduled.Run(appCtx)
	webServer, err := web.NewWithEnergy(store, manager, energyManager, cfg, logger, cipher, promhttp.HandlerFor(metricSet.Registry, promhttp.HandlerOpts{}))
	if err != nil {
		fatal("initialize web server", err)
	}
	httpServer := &http.Server{Addr: cfg.ListenAddress, Handler: webServer.Router(), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 35 * time.Second, WriteTimeout: 0, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 1 << 20}

	go func() {
		logger.Info("Shelly Inventory started", "operation", "startup", "listen_address", cfg.ListenAddress, "database_backend", store.Backend(), "mdns_enabled", cfg.EnableMDNS)
		if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("HTTP server stopped", "operation", "serve", "error", err)
			stop()
		}
	}()
	<-appCtx.Done()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		logger.Error("HTTP shutdown", "operation", "shutdown", "error", err)
	}
	energyManager.Stop()
	manager.Wait()
	logger.Info("Shelly Inventory stopped", "operation", "shutdown")
}

func healthcheck() {
	address := os.Getenv("APP_LISTEN_ADDRESS")
	if address == "" {
		address = "0.0.0.0:8080"
	}
	address = strings.Replace(address, "0.0.0.0", "127.0.0.1", 1)
	client := http.Client{Timeout: 2 * time.Second}
	response, err := client.Get("http://" + address + "/health")
	if err != nil {
		os.Exit(1)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		os.Exit(1)
	}
}

func registerInventoryGauges(metricSet *metrics.Metrics, store *database.Store) {
	definitions := []struct {
		name, help string
		value      func(database.DashboardStats) float64
	}{
		{"shelly_inventory_devices_online", "Devices currently online.", func(s database.DashboardStats) float64 { return float64(s.OnlineDevices) }},
		{"shelly_inventory_devices_offline", "Devices currently offline.", func(s database.DashboardStats) float64 { return float64(s.OfflineDevices) }},
		{"shelly_inventory_devices_authentication_required", "Devices requiring credentials.", func(s database.DashboardStats) float64 { return float64(s.AuthDevices) }},
	}
	for _, definition := range definitions {
		definition := definition
		metricSet.Registry.MustRegister(prometheus.NewGaugeFunc(prometheus.GaugeOpts{Name: definition.name, Help: definition.help}, func() float64 {
			stats, err := store.DashboardStats(context.Background())
			if err != nil {
				return 0
			}
			return definition.value(stats)
		}))
	}
}

func newLogger(level string) *slog.Logger {
	var parsed slog.Level
	if err := parsed.UnmarshalText([]byte(level)); err != nil {
		parsed = slog.LevelInfo
	}
	return slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: parsed}))
}

func fatal(operation string, err error) { slog.Error(operation, "error", err); os.Exit(1) }
