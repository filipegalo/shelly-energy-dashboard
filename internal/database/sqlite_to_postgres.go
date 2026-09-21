package database

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	_ "modernc.org/sqlite"
)

// MigrateSQLiteToPostgres copies a stopped SQLite database into an empty
// PostgreSQL deployment. IDs are preserved, so every foreign-key relation and
// encrypted credential envelope remains unchanged.
func MigrateSQLiteToPostgres(ctx context.Context, sqlitePath string, postgres PostgresOptions) error {
	if sqlitePath == "" {
		return fmt.Errorf("SQLite source path is required")
	}
	// The source is mounted read-only by the migration Job. SQLite must not try
	// to create a journal or a lock sidecar beside the original backup.
	source, err := sql.Open("sqlite", "file:"+sqlitePath+"?mode=ro&immutable=1&_pragma=foreign_keys(1)")
	if err != nil {
		return fmt.Errorf("open SQLite source: %w", err)
	}
	defer source.Close()
	if err := source.PingContext(ctx); err != nil {
		return fmt.Errorf("connect SQLite source: %w", err)
	}
	target, err := openPostgres(postgres)
	if err != nil {
		return err
	}
	defer target.Close()
	tx, err := target.db.raw.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	tables := []string{"invoice_reconciliations", "room_energy_assignments", "meter_events", "energy_interval_aggregates", "energy_samples", "electricity_pricing_versions", "room_energy_config", "energy_config", "scans", "devices", "credential_profiles", "networks", "rooms", "schema_migrations"}
	if _, err := tx.ExecContext(ctx, "TRUNCATE TABLE "+strings.Join(tables, ",")+" RESTART IDENTITY CASCADE"); err != nil {
		return fmt.Errorf("empty PostgreSQL target: %w", err)
	}
	// Parent-to-child order is required because the target enforces foreign keys.
	for _, table := range []string{"rooms", "networks", "credential_profiles", "devices", "scans", "energy_config", "room_energy_config", "electricity_pricing_versions", "energy_samples", "energy_interval_aggregates", "meter_events", "room_energy_assignments", "invoice_reconciliations", "schema_migrations"} {
		if err := copySQLiteTable(ctx, source, tx, table); err != nil {
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit PostgreSQL migration: %w", err)
	}
	if err := applyPostgresSchema(ctx, target.db.raw); err != nil {
		return err
	}
	return nil
}

func copySQLiteTable(ctx context.Context, source *sql.DB, target *sql.Tx, table string) error {
	info, err := source.QueryContext(ctx, "PRAGMA table_info("+table+")")
	if err != nil {
		return fmt.Errorf("inspect source table %s: %w", table, err)
	}
	defer info.Close()
	columns := []string{}
	for info.Next() {
		var cid int
		var name, typ string
		var notNull, pk int
		var defaultValue any
		if err := info.Scan(&cid, &name, &typ, &notNull, &defaultValue, &pk); err != nil {
			return err
		}
		columns = append(columns, name)
	}
	if err := info.Err(); err != nil {
		return err
	}
	if len(columns) == 0 {
		return fmt.Errorf("source table %s does not exist", table)
	}
	rows, err := source.QueryContext(ctx, "SELECT "+strings.Join(columns, ",")+" FROM "+table+" ORDER BY rowid")
	if err != nil {
		return fmt.Errorf("read source table %s: %w", table, err)
	}
	defer rows.Close()
	markers := make([]string, len(columns))
	for i := range markers {
		markers[i] = fmt.Sprintf("$%d", i+1)
	}
	insert := "INSERT INTO " + table + " (" + strings.Join(columns, ",") + ") VALUES (" + strings.Join(markers, ",") + ")"
	for rows.Next() {
		values := make([]any, len(columns))
		pointers := make([]any, len(columns))
		for i := range values {
			pointers[i] = &values[i]
		}
		if err := rows.Scan(pointers...); err != nil {
			return err
		}
		if _, err := target.ExecContext(ctx, insert, values...); err != nil {
			return fmt.Errorf("copy %s: %w", table, err)
		}
	}
	return rows.Err()
}
