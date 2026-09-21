package database

import (
	"context"
	"time"
)

// InvoiceReconciliation is a user-entered bill total for checking the energy
// estimate. Fixed charges are deliberately kept separate from variable energy.
type InvoiceReconciliation struct {
	ID              int64
	CycleStart      time.Time
	CycleEnd        time.Time
	VariableCostEUR string
	FixedCostEUR    string
	TotalCostEUR    string
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

func (s *Store) SaveInvoiceReconciliation(ctx context.Context, value InvoiceReconciliation) (InvoiceReconciliation, error) {
	now := timestamp(time.Now())
	_, err := s.db.ExecContext(ctx, `INSERT INTO invoice_reconciliations (cycle_start,cycle_end,variable_cost_eur,fixed_cost_eur,total_cost_eur,created_at,updated_at) VALUES (?,?,?,?,?,?,?) ON CONFLICT(cycle_start,cycle_end) DO UPDATE SET variable_cost_eur=excluded.variable_cost_eur,fixed_cost_eur=excluded.fixed_cost_eur,total_cost_eur=excluded.total_cost_eur,updated_at=excluded.updated_at`, timestamp(value.CycleStart), timestamp(value.CycleEnd), value.VariableCostEUR, value.FixedCostEUR, value.TotalCostEUR, now, now)
	if err != nil {
		return InvoiceReconciliation{}, err
	}
	return s.InvoiceReconciliationForCycle(ctx, value.CycleStart, value.CycleEnd)
}

func (s *Store) InvoiceReconciliationForCycle(ctx context.Context, start, end time.Time) (InvoiceReconciliation, error) {
	var value InvoiceReconciliation
	var cycleStart, cycleEnd, createdAt, updatedAt string
	err := s.db.QueryRowContext(ctx, `SELECT id,cycle_start,cycle_end,variable_cost_eur,fixed_cost_eur,total_cost_eur,created_at,updated_at FROM invoice_reconciliations WHERE cycle_start=? AND cycle_end=?`, timestamp(start), timestamp(end)).Scan(&value.ID, &cycleStart, &cycleEnd, &value.VariableCostEUR, &value.FixedCostEUR, &value.TotalCostEUR, &createdAt, &updatedAt)
	if err != nil {
		return InvoiceReconciliation{}, err
	}
	value.CycleStart, value.CycleEnd, value.CreatedAt, value.UpdatedAt = parseTime(cycleStart), parseTime(cycleEnd), parseTime(createdAt), parseTime(updatedAt)
	return value, nil
}
