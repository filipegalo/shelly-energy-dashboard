package scheduler

import (
	"context"
	"log/slog"
	"time"

	"github.com/filipegalo/shelly-energy-dashboard/internal/database"
	"github.com/filipegalo/shelly-energy-dashboard/internal/scans"
)

type Scheduler struct {
	store    *database.Store
	manager  *scans.Manager
	logger   *slog.Logger
	interval time.Duration
}

func New(store *database.Store, manager *scans.Manager, logger *slog.Logger) *Scheduler {
	return &Scheduler{store: store, manager: manager, logger: logger, interval: 15 * time.Second}
}

func (s *Scheduler) Run(ctx context.Context) {
	s.runDue(ctx)
	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.runDue(ctx)
		}
	}
}

func (s *Scheduler) runDue(ctx context.Context) {
	networks, err := s.store.DueScheduledNetworks(ctx, time.Now())
	if err != nil {
		s.logger.Error("load scheduled scans", "operation", "scheduler", "error", err)
		return
	}
	for _, network := range networks {
		if _, err := s.manager.Start(ctx, network.ID, "scheduled"); err != nil {
			s.logger.Warn("scheduled scan skipped", "operation", "scheduler", "network_id", network.ID, "error", err)
		}
	}
}
