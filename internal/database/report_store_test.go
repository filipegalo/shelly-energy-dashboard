package database

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func TestSaveInvoiceReconciliationUpsertsCycle(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "reports.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ctx := context.Background()
	start := time.Date(2026, time.September, 11, 0, 0, 0, 0, time.UTC)
	end := start.AddDate(0, 1, 0)
	first, err := store.SaveInvoiceReconciliation(ctx, InvoiceReconciliation{CycleStart: start, CycleEnd: end, VariableCostEUR: "12.34", FixedCostEUR: "5.00", TotalCostEUR: "17.34"})
	if err != nil {
		t.Fatal(err)
	}
	second, err := store.SaveInvoiceReconciliation(ctx, InvoiceReconciliation{CycleStart: start, CycleEnd: end, VariableCostEUR: "12.50", FixedCostEUR: "5.10", TotalCostEUR: "17.60"})
	if err != nil {
		t.Fatal(err)
	}
	if first.ID != second.ID || second.VariableCostEUR != "12.50" || second.FixedCostEUR != "5.10" || second.TotalCostEUR != "17.60" {
		t.Fatalf("reconciliation upsert = %#v, first=%#v", second, first)
	}
}
