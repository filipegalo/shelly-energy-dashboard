package database

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/filipegalo/shelly-energy-dashboard/internal/shelly"
)

var (
	ErrRoomEnergyAssignmentOverlap     = errors.New("room energy source assignment overlaps an existing assignment")
	ErrRoomEnergyParentRequired        = errors.New("a room energy submeter requires a parent assignment")
	ErrRoomEnergyInvalidHierarchy      = errors.New("room energy assignment hierarchy is invalid")
	ErrRoomEnergyMainMeterConfirmation = errors.New("assigning the whole-house main meter to a room requires confirmation")
	ErrRoomEnergyInvalidAssignment     = errors.New("room energy assignment is invalid")
)

const (
	roomEnergyTimeLayout = "2006-01-02T15:04:05.000000000Z"
	roomEnergyInfinity   = "9999-12-31T23:59:59.999999999Z"
)

func roomEnergyTimestamp(value time.Time) string {
	return value.UTC().Format(roomEnergyTimeLayout)
}

func (s *Store) GetRoomEnergyConfig(ctx context.Context) (RoomEnergyConfig, error) {
	return scanRoomEnergyConfig(s.db.QueryRowContext(ctx, `SELECT id,relative_tolerance,absolute_tolerance_wh,diagnostic_collection,created_at,updated_at FROM room_energy_config WHERE id=1`))
}

func (s *Store) SaveRoomEnergyConfig(ctx context.Context, config RoomEnergyConfig) (RoomEnergyConfig, error) {
	if math.IsNaN(config.RelativeTolerance) || math.IsInf(config.RelativeTolerance, 0) || config.RelativeTolerance < 0 || config.RelativeTolerance > 1 {
		return RoomEnergyConfig{}, errors.New("room energy relative tolerance must be between 0 and 1")
	}
	if math.IsNaN(config.AbsoluteToleranceWh) || math.IsInf(config.AbsoluteToleranceWh, 0) || config.AbsoluteToleranceWh < 0 || config.AbsoluteToleranceWh > 1_000_000_000 {
		return RoomEnergyConfig{}, errors.New("room energy absolute tolerance is outside the supported range")
	}
	_, err := s.db.ExecContext(ctx, `UPDATE room_energy_config SET relative_tolerance=?,absolute_tolerance_wh=?,diagnostic_collection=?,updated_at=? WHERE id=1`, config.RelativeTolerance, config.AbsoluteToleranceWh, config.DiagnosticCollection, timestamp(time.Now()))
	if err != nil {
		return RoomEnergyConfig{}, err
	}
	return s.GetRoomEnergyConfig(ctx)
}

func scanRoomEnergyConfig(scanner interface{ Scan(...any) error }) (RoomEnergyConfig, error) {
	var config RoomEnergyConfig
	var createdAt, updatedAt string
	err := scanner.Scan(&config.ID, &config.RelativeTolerance, &config.AbsoluteToleranceWh, &config.DiagnosticCollection, &createdAt, &updatedAt)
	config.CreatedAt = parseTime(createdAt)
	config.UpdatedAt = parseTime(updatedAt)
	return config, err
}

func (s *Store) CreateRoomEnergyAssignment(ctx context.Context, assignment RoomEnergyAssignment) (RoomEnergyAssignment, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return RoomEnergyAssignment{}, err
	}
	defer tx.Rollback()

	created, err := createRoomEnergyAssignment(ctx, tx, assignment)
	if err != nil {
		return RoomEnergyAssignment{}, err
	}
	if err := tx.Commit(); err != nil {
		return RoomEnergyAssignment{}, normalizeRoomEnergyAssignmentError(err)
	}
	return created, nil
}

func createRoomEnergyAssignment(ctx context.Context, tx *Tx, assignment RoomEnergyAssignment) (RoomEnergyAssignment, error) {
	assignment = normalizeRoomEnergyAssignment(assignment)
	if err := validateRoomEnergyAssignment(ctx, tx, assignment); err != nil {
		return RoomEnergyAssignment{}, err
	}

	now := roomEnergyTimestamp(time.Now())
	var effectiveTo any
	if assignment.EffectiveTo != nil {
		effectiveTo = roomEnergyTimestamp(*assignment.EffectiveTo)
	}
	result, err := tx.ExecContext(ctx, `INSERT INTO room_energy_assignments (room_id,device_id,component_key,component_type,display_name,enabled,aggregation_role,parent_assignment_id,poll_interval_seconds,main_meter_acknowledged,effective_from,effective_to,created_at,updated_at) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		assignment.RoomID, assignment.DeviceID, assignment.ComponentKey, assignment.ComponentType, assignment.DisplayName, assignment.Enabled, assignment.AggregationRole, nullableAssignmentID(assignment.ParentAssignmentID), assignment.PollIntervalSeconds, assignment.MainMeterAcknowledged, roomEnergyTimestamp(assignment.EffectiveFrom), effectiveTo, now, now)
	if err != nil {
		return RoomEnergyAssignment{}, normalizeRoomEnergyAssignmentError(err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		return RoomEnergyAssignment{}, err
	}
	return roomEnergyAssignmentByID(ctx, tx, id)
}

func normalizeRoomEnergyAssignment(assignment RoomEnergyAssignment) RoomEnergyAssignment {
	result := assignment
	result.ComponentKey = strings.TrimSpace(assignment.ComponentKey)
	result.ComponentType = strings.TrimSpace(assignment.ComponentType)
	result.DisplayName = strings.TrimSpace(assignment.DisplayName)
	result.AggregationRole = strings.TrimSpace(assignment.AggregationRole)
	result.EffectiveFrom = assignment.EffectiveFrom.UTC()
	if assignment.EffectiveTo != nil {
		value := assignment.EffectiveTo.UTC()
		result.EffectiveTo = &value
	}
	return result
}

func validateRoomEnergyAssignment(ctx context.Context, tx *Tx, assignment RoomEnergyAssignment) error {
	if assignment.RoomID < 1 || assignment.DeviceID < 1 || assignment.EffectiveFrom.IsZero() {
		return ErrRoomEnergyInvalidAssignment
	}
	if len(assignment.ComponentKey) < 1 || len(assignment.ComponentKey) > 64 || len(assignment.ComponentType) < 1 || len(assignment.ComponentType) > 32 || len(assignment.DisplayName) > 100 {
		return ErrRoomEnergyInvalidAssignment
	}
	componentType, _, _, componentErr := shelly.ParseEnergyComponentKey(assignment.ComponentKey)
	if componentErr != nil || componentType != assignment.ComponentType {
		return ErrRoomEnergyInvalidAssignment
	}
	if assignment.PollIntervalSeconds != 0 && (assignment.PollIntervalSeconds < 5 || assignment.PollIntervalSeconds > 300) {
		return ErrRoomEnergyInvalidAssignment
	}
	if assignment.EffectiveTo != nil && !assignment.EffectiveFrom.Before(*assignment.EffectiveTo) {
		return ErrRoomEnergyInvalidAssignment
	}
	switch assignment.AggregationRole {
	case RoomEnergyRolePrimary, RoomEnergyRoleExcluded:
		if assignment.ParentAssignmentID != 0 {
			return ErrRoomEnergyInvalidHierarchy
		}
	case RoomEnergyRoleSubmeter:
		if assignment.ParentAssignmentID == 0 {
			return ErrRoomEnergyParentRequired
		}
	default:
		return ErrRoomEnergyInvalidAssignment
	}

	var roomExists, deviceExists bool
	if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM rooms WHERE id=? AND archived_at IS NULL AND is_system=0)`, assignment.RoomID).Scan(&roomExists); err != nil {
		return err
	}
	if !roomExists {
		return ErrRoomEnergyInvalidAssignment
	}
	if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM devices WHERE id=? AND (coalesce(shelly_device_id,'')<>'' OR coalesce(mac_address,'')<>''))`, assignment.DeviceID).Scan(&deviceExists); err != nil {
		return err
	}
	if !deviceExists {
		return ErrRoomEnergyInvalidAssignment
	}

	if assignment.AggregationRole == RoomEnergyRoleSubmeter {
		parent, err := roomEnergyAssignmentByID(ctx, tx, assignment.ParentAssignmentID)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return ErrRoomEnergyInvalidHierarchy
			}
			return err
		}
		if parent.RoomID != assignment.RoomID || parent.AggregationRole != RoomEnergyRolePrimary || !parent.Enabled || parent.EffectiveFrom.After(assignment.EffectiveFrom) || !assignmentEndCoveredByParent(assignment.EffectiveTo, parent.EffectiveTo) {
			return ErrRoomEnergyInvalidHierarchy
		}
	}

	var mainDeviceID int64
	var mainComponentKey string
	if err := tx.QueryRowContext(ctx, `SELECT coalesce(device_id,0),component_key FROM energy_config WHERE id=1`).Scan(&mainDeviceID, &mainComponentKey); err != nil {
		return err
	}
	if assignment.DeviceID == mainDeviceID && assignment.ComponentKey == mainComponentKey && !assignment.MainMeterAcknowledged {
		return ErrRoomEnergyMainMeterConfirmation
	}

	var overlaps bool
	var effectiveTo any
	if assignment.EffectiveTo != nil {
		effectiveTo = roomEnergyTimestamp(*assignment.EffectiveTo)
	}
	if err := tx.QueryRowContext(ctx, `SELECT EXISTS(
		SELECT 1 FROM room_energy_assignments existing
		WHERE existing.device_id=? AND existing.component_key=?
		  AND existing.effective_from < coalesce(?,?)
		  AND ? < coalesce(existing.effective_to,?)
	)`, assignment.DeviceID, assignment.ComponentKey, effectiveTo, roomEnergyInfinity, roomEnergyTimestamp(assignment.EffectiveFrom), roomEnergyInfinity).Scan(&overlaps); err != nil {
		return err
	}
	if overlaps {
		return ErrRoomEnergyAssignmentOverlap
	}
	return nil
}

func assignmentEndCoveredByParent(childEnd, parentEnd *time.Time) bool {
	if childEnd == nil {
		return parentEnd == nil
	}
	return parentEnd == nil || !childEnd.After(*parentEnd)
}

func nullableAssignmentID(id int64) any {
	if id == 0 {
		return nil
	}
	return id
}

func normalizeRoomEnergyAssignmentError(err error) error {
	if err == nil {
		return nil
	}
	message := strings.ToLower(err.Error())
	if strings.Contains(message, "room_energy_assignment_overlap") || strings.Contains(message, "idx_room_energy_assignments_source_open") || strings.Contains(message, "idx_room_energy_assignments_source_start") {
		return fmt.Errorf("%w: %v", ErrRoomEnergyAssignmentOverlap, err)
	}
	if strings.Contains(message, "room_energy_invalid_hierarchy") {
		return fmt.Errorf("%w: %v", ErrRoomEnergyInvalidHierarchy, err)
	}
	return err
}

const roomEnergyAssignmentSelect = `SELECT
	a.id,a.room_id,r.name,r.archived_at,a.device_id,
	coalesce(nullif(d.custom_name,''),nullif(d.name,''),d.model),d.model,d.ip_address,
	a.component_key,a.component_type,a.display_name,a.enabled,a.aggregation_role,
	coalesce(a.parent_assignment_id,0),coalesce(nullif(parent.display_name,''),parent.component_key,''),
	a.poll_interval_seconds,a.main_meter_acknowledged,a.effective_from,a.effective_to,a.created_at,a.updated_at
	FROM room_energy_assignments a
	JOIN rooms r ON r.id=a.room_id
	JOIN devices d ON d.id=a.device_id
	LEFT JOIN room_energy_assignments parent ON parent.id=a.parent_assignment_id`

type roomEnergyQueryer interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func (s *Store) GetRoomEnergyAssignment(ctx context.Context, id int64) (RoomEnergyAssignment, error) {
	return roomEnergyAssignmentByID(ctx, s.db, id)
}

func roomEnergyAssignmentByID(ctx context.Context, queryer roomEnergyQueryer, id int64) (RoomEnergyAssignment, error) {
	return scanRoomEnergyAssignment(queryer.QueryRowContext(ctx, roomEnergyAssignmentSelect+` WHERE a.id=?`, id))
}

func scanRoomEnergyAssignment(scanner interface{ Scan(...any) error }) (RoomEnergyAssignment, error) {
	var assignment RoomEnergyAssignment
	var archivedAt, effectiveTo sql.NullString
	var effectiveFrom, createdAt, updatedAt string
	err := scanner.Scan(
		&assignment.ID, &assignment.RoomID, &assignment.RoomName, &archivedAt, &assignment.DeviceID,
		&assignment.DeviceName, &assignment.DeviceModel, &assignment.IPAddress,
		&assignment.ComponentKey, &assignment.ComponentType, &assignment.DisplayName, &assignment.Enabled, &assignment.AggregationRole,
		&assignment.ParentAssignmentID, &assignment.ParentDisplayName,
		&assignment.PollIntervalSeconds, &assignment.MainMeterAcknowledged, &effectiveFrom, &effectiveTo, &createdAt, &updatedAt,
	)
	assignment.RoomArchived = archivedAt.Valid
	assignment.EffectiveFrom = parseTime(effectiveFrom)
	if effectiveTo.Valid {
		value := parseTime(effectiveTo.String)
		assignment.EffectiveTo = &value
	}
	assignment.CreatedAt = parseTime(createdAt)
	assignment.UpdatedAt = parseTime(updatedAt)
	return assignment, err
}

func (s *Store) ListRoomEnergyAssignmentsOverlapping(ctx context.Context, start, end time.Time) ([]RoomEnergyAssignment, error) {
	if start.IsZero() || end.IsZero() || !start.Before(end) {
		return nil, errors.New("room energy assignment range must have increasing bounds")
	}
	rows, err := s.db.QueryContext(ctx, roomEnergyAssignmentSelect+` WHERE a.effective_from<? AND (a.effective_to IS NULL OR a.effective_to>?) ORDER BY a.effective_from,a.id`, roomEnergyTimestamp(end), roomEnergyTimestamp(start))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	assignments := make([]RoomEnergyAssignment, 0)
	for rows.Next() {
		assignment, err := scanRoomEnergyAssignment(rows)
		if err != nil {
			return nil, err
		}
		assignments = append(assignments, assignment)
	}
	return assignments, rows.Err()
}

func (s *Store) ListEnabledRoomEnergyAssignmentsAt(ctx context.Context, at time.Time) ([]RoomEnergyAssignment, error) {
	if at.IsZero() {
		return nil, errors.New("room energy assignment instant is required")
	}
	stamp := roomEnergyTimestamp(at)
	rows, err := s.db.QueryContext(ctx, roomEnergyAssignmentSelect+` WHERE a.enabled=1 AND a.effective_from<=? AND (a.effective_to IS NULL OR a.effective_to>?) ORDER BY a.room_id,a.id`, stamp, stamp)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	assignments := make([]RoomEnergyAssignment, 0)
	for rows.Next() {
		assignment, err := scanRoomEnergyAssignment(rows)
		if err != nil {
			return nil, err
		}
		assignments = append(assignments, assignment)
	}
	return assignments, rows.Err()
}

func (s *Store) MoveRoomEnergyAssignment(ctx context.Context, id, roomID int64, effectiveFrom time.Time) (RoomEnergyAssignment, error) {
	if id < 1 || roomID < 1 || effectiveFrom.IsZero() {
		return RoomEnergyAssignment{}, ErrRoomEnergyInvalidAssignment
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return RoomEnergyAssignment{}, err
	}
	defer tx.Rollback()

	original, err := roomEnergyAssignmentByID(ctx, tx, id)
	if err != nil {
		return RoomEnergyAssignment{}, err
	}
	boundary := effectiveFrom.UTC()
	if !original.EffectiveFrom.Before(boundary) || (original.EffectiveTo != nil && !boundary.Before(*original.EffectiveTo)) {
		return RoomEnergyAssignment{}, ErrRoomEnergyInvalidAssignment
	}
	if original.AggregationRole == RoomEnergyRoleSubmeter && roomID != original.RoomID {
		return RoomEnergyAssignment{}, ErrRoomEnergyInvalidHierarchy
	}
	if children, childErr := roomEnergyParentHasChildrenAfter(ctx, tx, id, boundary); childErr != nil {
		return RoomEnergyAssignment{}, childErr
	} else if children {
		return RoomEnergyAssignment{}, ErrRoomEnergyInvalidHierarchy
	}
	stamp := roomEnergyTimestamp(boundary)
	result, err := tx.ExecContext(ctx, `UPDATE room_energy_assignments SET effective_to=?,updated_at=? WHERE id=? AND effective_from<? AND (effective_to IS NULL OR effective_to>=?)`, stamp, roomEnergyTimestamp(time.Now()), id, stamp, stamp)
	if err != nil {
		return RoomEnergyAssignment{}, normalizeRoomEnergyAssignmentError(err)
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return RoomEnergyAssignment{}, err
	}
	if changed != 1 {
		return RoomEnergyAssignment{}, ErrRoomEnergyInvalidAssignment
	}

	moved := original
	moved.ID = 0
	moved.RoomID = roomID
	moved.RoomName = ""
	moved.RoomArchived = false
	moved.IPAddress = ""
	moved.ParentDisplayName = ""
	moved.EffectiveFrom = boundary
	moved.CreatedAt = time.Time{}
	moved.UpdatedAt = time.Time{}
	created, err := createRoomEnergyAssignment(ctx, tx, moved)
	if err != nil {
		return RoomEnergyAssignment{}, err
	}
	if err := tx.Commit(); err != nil {
		return RoomEnergyAssignment{}, normalizeRoomEnergyAssignmentError(err)
	}
	return created, nil
}

func (s *Store) ReviseRoomEnergyAssignment(ctx context.Context, id int64, revision RoomEnergyAssignmentRevision) (RoomEnergyAssignment, error) {
	if id < 1 || revision.RoomID < 1 || revision.EffectiveFrom.IsZero() {
		return RoomEnergyAssignment{}, ErrRoomEnergyInvalidAssignment
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return RoomEnergyAssignment{}, err
	}
	defer tx.Rollback()

	original, err := roomEnergyAssignmentByID(ctx, tx, id)
	if err != nil {
		return RoomEnergyAssignment{}, err
	}
	boundary := revision.EffectiveFrom.UTC()
	if !original.EffectiveFrom.Before(boundary) || (original.EffectiveTo != nil && !boundary.Before(*original.EffectiveTo)) {
		return RoomEnergyAssignment{}, ErrRoomEnergyInvalidAssignment
	}
	if children, childErr := roomEnergyParentHasChildrenAfter(ctx, tx, id, boundary); childErr != nil {
		return RoomEnergyAssignment{}, childErr
	} else if children {
		return RoomEnergyAssignment{}, ErrRoomEnergyInvalidHierarchy
	}

	stamp := roomEnergyTimestamp(boundary)
	result, err := tx.ExecContext(ctx, `UPDATE room_energy_assignments SET effective_to=?,updated_at=? WHERE id=? AND effective_from<? AND (effective_to IS NULL OR effective_to>=?)`, stamp, roomEnergyTimestamp(time.Now()), id, stamp, stamp)
	if err != nil {
		return RoomEnergyAssignment{}, normalizeRoomEnergyAssignmentError(err)
	}
	if changed, affectedErr := result.RowsAffected(); affectedErr != nil {
		return RoomEnergyAssignment{}, affectedErr
	} else if changed != 1 {
		return RoomEnergyAssignment{}, ErrRoomEnergyInvalidAssignment
	}

	next := original
	next.ID = 0
	next.RoomID = revision.RoomID
	next.RoomName = ""
	next.RoomArchived = false
	next.DisplayName = revision.DisplayName
	next.Enabled = revision.Enabled
	next.AggregationRole = revision.AggregationRole
	next.ParentAssignmentID = revision.ParentAssignmentID
	next.ParentDisplayName = ""
	next.PollIntervalSeconds = revision.PollIntervalSeconds
	next.MainMeterAcknowledged = revision.MainMeterAcknowledged
	next.EffectiveFrom = boundary
	// A revision splits the original half-open interval; it must not turn a
	// finite historical/scheduled assignment into an open-ended assignment.
	next.EffectiveTo = copyRoomEnergyTime(original.EffectiveTo)
	next.CreatedAt = time.Time{}
	next.UpdatedAt = time.Time{}
	created, err := createRoomEnergyAssignment(ctx, tx, next)
	if err != nil {
		return RoomEnergyAssignment{}, err
	}
	if err := tx.Commit(); err != nil {
		return RoomEnergyAssignment{}, normalizeRoomEnergyAssignmentError(err)
	}
	return created, nil
}

func copyRoomEnergyTime(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}

func (s *Store) CloseRoomEnergyAssignment(ctx context.Context, id int64, effectiveTo time.Time) error {
	if id < 1 || effectiveTo.IsZero() {
		return ErrRoomEnergyInvalidAssignment
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	assignment, err := roomEnergyAssignmentByID(ctx, tx, id)
	if err != nil {
		return err
	}
	boundary := effectiveTo.UTC()
	if !assignment.EffectiveFrom.Before(boundary) || (assignment.EffectiveTo != nil && boundary.After(*assignment.EffectiveTo)) {
		return ErrRoomEnergyInvalidAssignment
	}
	if children, childErr := roomEnergyParentHasChildrenAfter(ctx, tx, id, boundary); childErr != nil {
		return childErr
	} else if children {
		return ErrRoomEnergyInvalidHierarchy
	}
	stamp := roomEnergyTimestamp(boundary)
	result, err := tx.ExecContext(ctx, `UPDATE room_energy_assignments SET effective_to=?,updated_at=? WHERE id=?`, stamp, roomEnergyTimestamp(time.Now()), id)
	if err != nil {
		return normalizeRoomEnergyAssignmentError(err)
	}
	if changed, affectedErr := result.RowsAffected(); affectedErr != nil {
		return affectedErr
	} else if changed != 1 {
		return ErrRoomEnergyInvalidAssignment
	}
	return tx.Commit()
}

func (s *Store) DeleteFutureRoomEnergyAssignment(ctx context.Context, id int64, now time.Time) error {
	if id < 1 || now.IsZero() {
		return ErrRoomEnergyInvalidAssignment
	}
	result, err := s.db.ExecContext(ctx, `DELETE FROM room_energy_assignments WHERE id=? AND effective_from>?`, id, roomEnergyTimestamp(now))
	if err != nil {
		return err
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if changed != 1 {
		return errors.New("only a future room energy assignment can be removed")
	}
	return nil
}

func (s *Store) ListRoomEnergyAssignments(ctx context.Context) ([]RoomEnergyAssignment, error) {
	rows, err := s.db.QueryContext(ctx, roomEnergyAssignmentSelect+` ORDER BY a.effective_from DESC,a.id DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]RoomEnergyAssignment, 0)
	for rows.Next() {
		assignment, scanErr := scanRoomEnergyAssignment(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		result = append(result, assignment)
	}
	return result, rows.Err()
}

func (s *Store) UpdateRoomEnergyAssignmentDisplayName(ctx context.Context, id int64, displayName string) error {
	displayName = strings.TrimSpace(displayName)
	if id < 1 || len(displayName) > 100 {
		return ErrRoomEnergyInvalidAssignment
	}
	result, err := s.db.ExecContext(ctx, `UPDATE room_energy_assignments SET display_name=?,updated_at=? WHERE id=?`, displayName, roomEnergyTimestamp(time.Now()), id)
	if err != nil {
		return err
	}
	if changed, affectedErr := result.RowsAffected(); affectedErr != nil {
		return affectedErr
	} else if changed != 1 {
		return sql.ErrNoRows
	}
	return nil
}

func roomEnergyParentHasChildrenAfter(ctx context.Context, tx *Tx, parentID int64, boundary time.Time) (bool, error) {
	var exists bool
	stamp := roomEnergyTimestamp(boundary)
	err := tx.QueryRowContext(ctx, `SELECT EXISTS(
		SELECT 1 FROM room_energy_assignments
		WHERE parent_assignment_id=? AND effective_from<? AND (effective_to IS NULL OR effective_to>?)
	)`, parentID, roomEnergyInfinity, stamp).Scan(&exists)
	return exists, err
}
