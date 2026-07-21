package energy

import (
	"errors"
	"time"

	roomcore "github.com/filipegalo/shelly-energy-dashboard/internal/energy/rooms"
	"github.com/shopspring/decimal"
)

// ObserveRoomEnergyReport records bounded, label-free report health metrics.
// Reports remain pure and independently testable; the application manager is
// the optional observation boundary used by HTTP callers.
func (m *Manager) ObserveRoomEnergyReport(report RoomEnergyReport, duration time.Duration, reportErr error) {
	if m == nil || m.metrics == nil {
		return
	}
	m.metrics.RoomEnergyAggregationDuration.Observe(duration.Seconds())
	if reportErr != nil {
		if errors.Is(reportErr, ErrRoomEnergyAllocation) {
			m.metrics.RoomEnergyAllocationErrors.Inc()
		}
		return
	}

	roomTotal := decimal.Zero
	for _, room := range report.Rooms {
		if room.EnergyKWh != nil {
			roomTotal = roomTotal.Add(*room.EnergyKWh)
		}
	}
	m.metrics.RoomEnergyConsumptionKWh.Set(roomTotal.InexactFloat64())
	if report.Unmetered.EnergyKWh != nil {
		m.metrics.RoomEnergyUnmeteredKWh.Set(report.Unmetered.EnergyKWh.InexactFloat64())
	} else {
		m.metrics.RoomEnergyUnmeteredKWh.Set(0)
	}
	main := report.WholeHouse.Consumption
	if main.IsPositive() {
		m.metrics.RoomEnergyCoverageRatio.Set(roomTotal.Div(main).InexactFloat64())
	} else {
		m.metrics.RoomEnergyCoverageRatio.Set(0)
	}
	m.metrics.RoomEnergyReconciliationDifferenceKWh.Set(roomTotal.Sub(main).InexactFloat64())
	for _, point := range report.Reconciliation {
		if point.Status == roomcore.ReconciliationWarning {
			m.metrics.RoomEnergyReconciliationWarnings.Inc()
		}
	}
}
