package scheduler

import (
	"context"
	"io"
	"log/slog"
	"path/filepath"
	"testing"
	"time"

	"github.com/filipegalo/shelly-energy-dashboard/internal/config"
	"github.com/filipegalo/shelly-energy-dashboard/internal/database"
	"github.com/filipegalo/shelly-energy-dashboard/internal/metrics"
	"github.com/filipegalo/shelly-energy-dashboard/internal/scans"
)

func TestRunDueStartsEnabledScheduledNetworkOnly(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	store, err := database.Open(filepath.Join(t.TempDir(), "scheduler.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	_, err = store.SaveNetwork(ctx, database.Network{Name: "Scheduled", CIDR: "192.168.90.0/30", Enabled: true, AutomaticScanEnabled: true, ScanIntervalSeconds: 60, ScanConcurrency: 1, ConnectionTimeoutMS: 50, RequestTimeoutMS: 100})
	if err != nil {
		t.Fatal(err)
	}
	_, err = store.SaveNetwork(ctx, database.Network{Name: "Disabled", CIDR: "192.168.91.0/30", Enabled: true, AutomaticScanEnabled: false, ScanIntervalSeconds: 60, ScanConcurrency: 1, ConnectionTimeoutMS: 50, RequestTimeoutMS: 100})
	if err != nil {
		t.Fatal(err)
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	manager := scans.NewManager(ctx, store, config.Config{MaxSubnetPrefix: 16, MaxHosts: 65534, DefaultScanConcurrency: 1, DefaultConnectionTimeout: 50 * time.Millisecond, DefaultRequestTimeout: 100 * time.Millisecond, OfflineFailureThreshold: 3}, logger, metrics.New(), nil)
	scheduler := New(store, manager, logger)
	scheduler.runDue(ctx)
	items, err := store.ListScans(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].NetworkName != "Scheduled" || items[0].TriggerType != "scheduled" {
		t.Fatalf("scheduled scans = %+v", items)
	}
	cancel()
	manager.Wait()
}

func TestRunStopsWithContextAndClosedStoreErrorsAreNonfatal(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	store, err := database.Open(filepath.Join(t.TempDir(), "closed.db"))
	if err != nil {
		t.Fatal(err)
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	manager := scans.NewManager(ctx, store, config.Config{}, logger, metrics.New(), nil)
	scheduler := New(store, manager, logger)
	store.Close()
	cancel()
	done := make(chan struct{})
	go func() { scheduler.Run(ctx); close(done) }()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("scheduler did not stop")
	}
}
