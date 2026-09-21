package database

import (
	"context"
	"database/sql"
	"fmt"
)

type schemaMigration struct {
	version int
	sql     string
}

var orderedSchemaMigrations = []schemaMigration{
	{
		version: 3,
		sql: `
ALTER TABLE rooms ADD COLUMN archived_at TEXT;

CREATE INDEX idx_rooms_active_sort
  ON rooms(sort_order, name)
  WHERE archived_at IS NULL;

CREATE TABLE room_energy_config (
  id INTEGER PRIMARY KEY CHECK (id = 1),
  relative_tolerance REAL NOT NULL DEFAULT 0.05 CHECK (relative_tolerance >= 0 AND relative_tolerance <= 1),
  absolute_tolerance_wh REAL NOT NULL DEFAULT 100 CHECK (absolute_tolerance_wh >= 0),
  diagnostic_collection INTEGER NOT NULL DEFAULT 0 CHECK (diagnostic_collection IN (0,1)),
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);

INSERT INTO room_energy_config (id,created_at,updated_at)
VALUES (1,strftime('%Y-%m-%dT%H:%M:%fZ','now'),strftime('%Y-%m-%dT%H:%M:%fZ','now'));

CREATE TABLE room_energy_assignments (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  room_id INTEGER NOT NULL REFERENCES rooms(id) ON DELETE RESTRICT,
  device_id INTEGER NOT NULL REFERENCES devices(id) ON DELETE RESTRICT,
  component_key TEXT NOT NULL CHECK (length(component_key) BETWEEN 1 AND 64),
  component_type TEXT NOT NULL CHECK (length(component_type) BETWEEN 1 AND 32),
  display_name TEXT NOT NULL DEFAULT '' CHECK (length(display_name) <= 100),
  enabled INTEGER NOT NULL DEFAULT 1 CHECK (enabled IN (0,1)),
  aggregation_role TEXT NOT NULL CHECK (aggregation_role IN ('primary','submeter','excluded')),
  parent_assignment_id INTEGER REFERENCES room_energy_assignments(id) ON DELETE RESTRICT,
  poll_interval_seconds INTEGER NOT NULL DEFAULT 0 CHECK (poll_interval_seconds = 0 OR poll_interval_seconds BETWEEN 5 AND 300),
  main_meter_acknowledged INTEGER NOT NULL DEFAULT 0 CHECK (main_meter_acknowledged IN (0,1)),
  effective_from TEXT NOT NULL CHECK (length(effective_from) = 30 AND julianday(effective_from) IS NOT NULL),
  effective_to TEXT CHECK (effective_to IS NULL OR (length(effective_to) = 30 AND julianday(effective_to) IS NOT NULL AND effective_to > effective_from)),
  created_at TEXT NOT NULL CHECK (length(created_at) = 30 AND julianday(created_at) IS NOT NULL),
  updated_at TEXT NOT NULL CHECK (length(updated_at) = 30 AND julianday(updated_at) IS NOT NULL),
  CHECK (
    (aggregation_role = 'submeter' AND parent_assignment_id IS NOT NULL)
    OR
    (aggregation_role <> 'submeter' AND parent_assignment_id IS NULL)
  )
);

CREATE UNIQUE INDEX idx_room_energy_assignments_source_start
  ON room_energy_assignments(device_id, component_key, effective_from);

CREATE UNIQUE INDEX idx_room_energy_assignments_source_open
  ON room_energy_assignments(device_id, component_key)
  WHERE effective_to IS NULL;

CREATE INDEX idx_room_energy_assignments_source_time
  ON room_energy_assignments(device_id, component_key, effective_from, effective_to);

CREATE INDEX idx_room_energy_assignments_room_time
  ON room_energy_assignments(room_id, effective_from, effective_to);

CREATE INDEX idx_room_energy_assignments_parent
  ON room_energy_assignments(parent_assignment_id);

CREATE INDEX idx_room_energy_assignments_collection
  ON room_energy_assignments(enabled, effective_from, effective_to);

CREATE TRIGGER room_energy_assignments_no_overlap_insert
BEFORE INSERT ON room_energy_assignments
WHEN EXISTS (
  SELECT 1
  FROM room_energy_assignments existing
  WHERE existing.device_id = NEW.device_id
    AND existing.component_key = NEW.component_key
    AND existing.effective_from < coalesce(NEW.effective_to, '9999-12-31T23:59:59.999999999Z')
    AND NEW.effective_from < coalesce(existing.effective_to, '9999-12-31T23:59:59.999999999Z')
)
BEGIN
  SELECT RAISE(ABORT, 'room_energy_assignment_overlap');
END;

CREATE TRIGGER room_energy_assignments_no_overlap_update
BEFORE UPDATE OF device_id, component_key, effective_from, effective_to ON room_energy_assignments
WHEN EXISTS (
  SELECT 1
  FROM room_energy_assignments existing
  WHERE existing.id <> NEW.id
    AND existing.device_id = NEW.device_id
    AND existing.component_key = NEW.component_key
    AND existing.effective_from < coalesce(NEW.effective_to, '9999-12-31T23:59:59.999999999Z')
    AND NEW.effective_from < coalesce(existing.effective_to, '9999-12-31T23:59:59.999999999Z')
)
BEGIN
  SELECT RAISE(ABORT, 'room_energy_assignment_overlap');
END;

CREATE TRIGGER room_energy_assignments_parent_insert
BEFORE INSERT ON room_energy_assignments
WHEN NEW.aggregation_role = 'submeter' AND NOT EXISTS (
  SELECT 1
  FROM room_energy_assignments parent
  WHERE parent.id = NEW.parent_assignment_id
    AND parent.room_id = NEW.room_id
    AND parent.aggregation_role = 'primary'
    AND parent.enabled = 1
    AND parent.effective_from <= NEW.effective_from
    AND (
      (NEW.effective_to IS NULL AND parent.effective_to IS NULL)
      OR
      (NEW.effective_to IS NOT NULL AND (parent.effective_to IS NULL OR NEW.effective_to <= parent.effective_to))
    )
)
BEGIN
  SELECT RAISE(ABORT, 'room_energy_invalid_hierarchy');
END;

CREATE TRIGGER room_energy_assignments_parent_update
BEFORE UPDATE OF room_id, aggregation_role, parent_assignment_id, enabled, effective_from, effective_to ON room_energy_assignments
WHEN NEW.aggregation_role = 'submeter' AND NOT EXISTS (
  SELECT 1
  FROM room_energy_assignments parent
  WHERE parent.id = NEW.parent_assignment_id
    AND parent.room_id = NEW.room_id
    AND parent.aggregation_role = 'primary'
    AND parent.enabled = 1
    AND parent.effective_from <= NEW.effective_from
    AND (
      (NEW.effective_to IS NULL AND parent.effective_to IS NULL)
      OR
      (NEW.effective_to IS NOT NULL AND (parent.effective_to IS NULL OR NEW.effective_to <= parent.effective_to))
    )
)
BEGIN
  SELECT RAISE(ABORT, 'room_energy_invalid_hierarchy');
END;

CREATE TRIGGER room_energy_assignments_children_update
BEFORE UPDATE OF room_id, aggregation_role, effective_from, effective_to ON room_energy_assignments
WHEN EXISTS (
  SELECT 1
  FROM room_energy_assignments child
  WHERE child.parent_assignment_id = OLD.id
    AND (
      NEW.room_id <> child.room_id
      OR NEW.aggregation_role <> 'primary'
      OR NEW.effective_from > child.effective_from
      OR (NEW.effective_to IS NOT NULL AND (child.effective_to IS NULL OR child.effective_to > NEW.effective_to))
    )
)
BEGIN
  SELECT RAISE(ABORT, 'room_energy_invalid_hierarchy');
END;
`,
	},
	{
		version: 4,
		sql: `
CREATE TABLE invoice_reconciliations (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  cycle_start TEXT NOT NULL,
  cycle_end TEXT NOT NULL,
  variable_cost_eur TEXT NOT NULL DEFAULT '0',
  fixed_cost_eur TEXT NOT NULL DEFAULT '0',
  total_cost_eur TEXT NOT NULL DEFAULT '0',
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  UNIQUE(cycle_start, cycle_end)
);
`,
	},
}

func applySchemaMigrations(ctx context.Context, db *sql.DB) error {
	for _, migration := range orderedSchemaMigrations {
		var applied bool
		if err := db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM schema_migrations WHERE version=?)`, migration.version).Scan(&applied); err != nil {
			return fmt.Errorf("inspect migration %d: %w", migration.version, err)
		}
		if applied {
			continue
		}

		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			return fmt.Errorf("begin migration %d: %w", migration.version, err)
		}
		if _, err := tx.ExecContext(ctx, migration.sql); err != nil {
			tx.Rollback()
			return fmt.Errorf("execute migration %d: %w", migration.version, err)
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO schema_migrations (version,applied_at) VALUES (?,datetime('now'))`, migration.version); err != nil {
			tx.Rollback()
			return fmt.Errorf("record migration %d: %w", migration.version, err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("commit migration %d: %w", migration.version, err)
		}
	}
	return nil
}
