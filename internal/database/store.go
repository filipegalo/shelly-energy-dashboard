package database

import (
	"context"
	"database/sql"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/filipegalo/shelly-inventory/internal/shelly"
	_ "modernc.org/sqlite"
)

//go:embed schema.sql
var schema string

type Store struct {
	db *sql.DB
}

func Open(path string) (*Store, error) {
	directory := filepath.Dir(path)
	_, statErr := os.Stat(directory)
	createdDirectory := errors.Is(statErr, os.ErrNotExist)
	if statErr != nil && !createdDirectory {
		return nil, fmt.Errorf("inspect database directory: %w", statErr)
	}
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return nil, fmt.Errorf("create database directory: %w", err)
	}
	if createdDirectory {
		if err := os.Chmod(directory, 0o700); err != nil {
			return nil, fmt.Errorf("secure database directory: %w", err)
		}
	}
	dsn := "file:" + path + "?_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}
	db.SetMaxOpenConns(8)
	db.SetMaxIdleConns(4)
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("apply database schema: %w", err)
	}
	if err := os.Chmod(path, 0o600); err != nil {
		db.Close()
		return nil, fmt.Errorf("secure database file: %w", err)
	}
	store := &Store{db: db}
	if err := store.recoverInterruptedScans(context.Background()); err != nil {
		db.Close()
		return nil, err
	}
	return store, nil
}

func (s *Store) Close() error { return s.db.Close() }

func (s *Store) Ready(ctx context.Context) error { return s.db.PingContext(ctx) }

func (s *Store) recoverInterruptedScans(ctx context.Context) error {
	now := timestamp(time.Now())
	_, err := s.db.ExecContext(ctx, `UPDATE scans SET status='failed', completed_at=?, updated_at=?, error_summary='interrupted by application restart' WHERE status IN ('queued','running')`, now, now)
	return err
}

func (s *Store) ListNetworks(ctx context.Context, enabledOnly bool) ([]Network, error) {
	query := `SELECT id,name,cidr,enabled,automatic_scan_enabled,scan_interval_seconds,scan_concurrency,connection_timeout_ms,request_timeout_ms,created_at,updated_at FROM networks WHERE deleted_at IS NULL`
	if enabledOnly {
		query += ` AND enabled=1`
	}
	query += ` ORDER BY name`
	rows, err := s.db.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []Network
	for rows.Next() {
		network, err := scanNetwork(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, network)
	}
	return result, rows.Err()
}

func (s *Store) GetNetwork(ctx context.Context, id int64) (Network, error) {
	row := s.db.QueryRowContext(ctx, `SELECT id,name,cidr,enabled,automatic_scan_enabled,scan_interval_seconds,scan_concurrency,connection_timeout_ms,request_timeout_ms,created_at,updated_at FROM networks WHERE id=? AND deleted_at IS NULL`, id)
	return scanNetwork(row)
}

func (s *Store) SaveNetwork(ctx context.Context, network Network) (Network, error) {
	now := timestamp(time.Now())
	if network.ID == 0 {
		result, err := s.db.ExecContext(ctx, `INSERT INTO networks (name,cidr,enabled,automatic_scan_enabled,scan_interval_seconds,scan_concurrency,connection_timeout_ms,request_timeout_ms,created_at,updated_at) VALUES (?,?,?,?,?,?,?,?,?,?)`,
			network.Name, network.CIDR, network.Enabled, network.AutomaticScanEnabled, network.ScanIntervalSeconds, network.ScanConcurrency, network.ConnectionTimeoutMS, network.RequestTimeoutMS, now, now)
		if err != nil {
			return Network{}, err
		}
		network.ID, _ = result.LastInsertId()
	} else {
		_, err := s.db.ExecContext(ctx, `UPDATE networks SET name=?,cidr=?,enabled=?,automatic_scan_enabled=?,scan_interval_seconds=?,scan_concurrency=?,connection_timeout_ms=?,request_timeout_ms=?,updated_at=? WHERE id=? AND deleted_at IS NULL`,
			network.Name, network.CIDR, network.Enabled, network.AutomaticScanEnabled, network.ScanIntervalSeconds, network.ScanConcurrency, network.ConnectionTimeoutMS, network.RequestTimeoutMS, now, network.ID)
		if err != nil {
			return Network{}, err
		}
	}
	return s.GetNetwork(ctx, network.ID)
}

func (s *Store) DeleteNetwork(ctx context.Context, id int64) error {
	now := timestamp(time.Now())
	result, err := s.db.ExecContext(ctx, `UPDATE networks SET enabled=0,automatic_scan_enabled=0,deleted_at=?,updated_at=? WHERE id=? AND deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM scans WHERE network_id=? AND status IN ('queued','running'))`, now, now, id, id)
	if err != nil {
		return err
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if changed == 0 {
		return errors.New("network does not exist or has an active scan")
	}
	return nil
}

func scanNetwork(scanner interface{ Scan(...any) error }) (Network, error) {
	var network Network
	var created, updated string
	err := scanner.Scan(&network.ID, &network.Name, &network.CIDR, &network.Enabled, &network.AutomaticScanEnabled, &network.ScanIntervalSeconds, &network.ScanConcurrency, &network.ConnectionTimeoutMS, &network.RequestTimeoutMS, &created, &updated)
	network.CreatedAt = parseTime(created)
	network.UpdatedAt = parseTime(updated)
	return network, err
}

func (s *Store) ListRooms(ctx context.Context) ([]Room, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT r.id,r.name,r.description,r.icon,r.sort_order,r.is_system,count(d.id),coalesce(sum(CASE WHEN d.online_status='online' THEN 1 ELSE 0 END),0),coalesce(sum(CASE WHEN d.online_status='offline' THEN 1 ELSE 0 END),0) FROM rooms r LEFT JOIN devices d ON d.room_id=r.id GROUP BY r.id ORDER BY r.sort_order,r.name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []Room
	for rows.Next() {
		var room Room
		if err := rows.Scan(&room.ID, &room.Name, &room.Description, &room.Icon, &room.SortOrder, &room.IsSystem, &room.DeviceCount, &room.OnlineCount, &room.OfflineCount); err != nil {
			return nil, err
		}
		result = append(result, room)
	}
	return result, rows.Err()
}

func (s *Store) GetRoom(ctx context.Context, id int64) (Room, error) {
	var room Room
	err := s.db.QueryRowContext(ctx, `SELECT r.id,r.name,r.description,r.icon,r.sort_order,r.is_system,count(d.id),coalesce(sum(CASE WHEN d.online_status='online' THEN 1 ELSE 0 END),0),coalesce(sum(CASE WHEN d.online_status='offline' THEN 1 ELSE 0 END),0) FROM rooms r LEFT JOIN devices d ON d.room_id=r.id WHERE r.id=? GROUP BY r.id`, id).Scan(
		&room.ID, &room.Name, &room.Description, &room.Icon, &room.SortOrder, &room.IsSystem, &room.DeviceCount, &room.OnlineCount, &room.OfflineCount)
	return room, err
}

func (s *Store) SaveRoom(ctx context.Context, room Room) (Room, error) {
	now := timestamp(time.Now())
	if room.ID == 0 {
		result, err := s.db.ExecContext(ctx, `INSERT INTO rooms (name,description,icon,sort_order,created_at,updated_at) VALUES (?,?,?,?,?,?)`, room.Name, room.Description, room.Icon, room.SortOrder, now, now)
		if err != nil {
			return Room{}, err
		}
		room.ID, _ = result.LastInsertId()
	} else {
		_, err := s.db.ExecContext(ctx, `UPDATE rooms SET name=?,description=?,icon=?,sort_order=?,updated_at=? WHERE id=? AND is_system=0`, room.Name, room.Description, room.Icon, room.SortOrder, now, room.ID)
		if err != nil {
			return Room{}, err
		}
	}
	return s.GetRoom(ctx, room.ID)
}

func (s *Store) DeleteRoom(ctx context.Context, id int64) error {
	room, err := s.GetRoom(ctx, id)
	if err != nil {
		return err
	}
	if room.IsSystem {
		return errors.New("the Unassigned room cannot be deleted")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `UPDATE devices SET room_id=1,updated_at=? WHERE room_id=?`, timestamp(time.Now()), id); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM rooms WHERE id=?`, id); err != nil {
		return err
	}
	return tx.Commit()
}

type DeviceFilter struct {
	Search, Status, Generation, Model string
	RoomID, NetworkID                 int64
}

func (s *Store) ListDevices(ctx context.Context, filter DeviceFilter) ([]Device, error) {
	query := deviceSelect + ` WHERE 1=1`
	args := []any{}
	if filter.Search != "" {
		query += ` AND (d.name LIKE ? OR d.custom_name LIKE ? OR d.model LIKE ? OR d.ip_address LIKE ? OR d.mac_address LIKE ?)`
		value := "%" + filter.Search + "%"
		args = append(args, value, value, value, value, value)
	}
	if filter.Status != "" {
		query += ` AND d.online_status=?`
		args = append(args, filter.Status)
	}
	if filter.Generation != "" {
		query += ` AND d.generation=?`
		args = append(args, filter.Generation)
	}
	if filter.Model != "" {
		query += ` AND d.model LIKE ?`
		args = append(args, "%"+filter.Model+"%")
	}
	if filter.RoomID != 0 {
		query += ` AND d.room_id=?`
		args = append(args, filter.RoomID)
	}
	if filter.NetworkID != 0 {
		query += ` AND d.network_id=?`
		args = append(args, filter.NetworkID)
	}
	query += ` ORDER BY coalesce(nullif(d.custom_name,''),nullif(d.name,''),d.model),d.ip_address`
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []Device
	for rows.Next() {
		device, err := scanDevice(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, device)
	}
	return result, rows.Err()
}

const deviceSelect = `SELECT d.id,coalesce(d.shelly_device_id,''),d.name,d.custom_name,d.model,d.generation,d.device_type,coalesce(d.mac_address,''),d.ip_address,d.hostname,d.firmware_version,d.firmware_build,d.hardware_revision,d.room_id,r.name,coalesce(d.network_id,0),coalesce(n.name,''),coalesce(d.credential_profile_id,0),d.authentication_required,d.online_status,d.consecutive_failures,d.capabilities_json,d.first_seen_at,d.last_seen_at,d.last_successful_scan_at,d.last_error,coalesce(d.raw_device_info,''),coalesce(d.raw_status,'') FROM devices d JOIN rooms r ON r.id=d.room_id LEFT JOIN networks n ON n.id=d.network_id`

func (s *Store) GetDevice(ctx context.Context, id int64) (Device, error) {
	return scanDevice(s.db.QueryRowContext(ctx, deviceSelect+` WHERE d.id=?`, id))
}

func scanDevice(scanner interface{ Scan(...any) error }) (Device, error) {
	var device Device
	var firstSeen, lastSeen string
	var lastSuccess sql.NullString
	err := scanner.Scan(&device.ID, &device.ShellyDeviceID, &device.Name, &device.CustomName, &device.Model, &device.Generation, &device.DeviceType, &device.MACAddress, &device.IPAddress, &device.Hostname, &device.FirmwareVersion, &device.FirmwareBuild, &device.HardwareRevision, &device.RoomID, &device.RoomName, &device.NetworkID, &device.NetworkName, &device.CredentialProfileID, &device.AuthenticationRequired, &device.OnlineStatus, &device.ConsecutiveFailures, &device.CapabilitiesJSON, &firstSeen, &lastSeen, &lastSuccess, &device.LastError, &device.RawDeviceInfo, &device.RawStatus)
	device.FirstSeenAt = parseTime(firstSeen)
	device.LastSeenAt = parseTime(lastSeen)
	if lastSuccess.Valid {
		value := parseTime(lastSuccess.String)
		device.LastSuccessfulScanAt = &value
	}
	return device, err
}

func (s *Store) UpdateDeviceName(ctx context.Context, id int64, name string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE devices SET custom_name=?,updated_at=? WHERE id=?`, strings.TrimSpace(name), timestamp(time.Now()), id)
	return err
}

func (s *Store) UpdateDeviceRoom(ctx context.Context, id, roomID int64) error {
	_, err := s.db.ExecContext(ctx, `UPDATE devices SET room_id=?,updated_at=? WHERE id=?`, roomID, timestamp(time.Now()), id)
	return err
}

func (s *Store) UpdateDeviceCredential(ctx context.Context, id, credentialID int64) error {
	var value any
	if credentialID != 0 {
		value = credentialID
	}
	_, err := s.db.ExecContext(ctx, `UPDATE devices SET credential_profile_id=?,updated_at=? WHERE id=?`, value, timestamp(time.Now()), id)
	return err
}

type UpsertResult struct {
	Device Device
	New    bool
}

func (s *Store) UpsertDevice(ctx context.Context, networkID int64, ipAddress string, info shelly.DeviceInfo, authRequired bool, lastError string) (UpsertResult, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return UpsertResult{}, err
	}
	defer tx.Rollback()
	var id int64
	queries := []struct {
		query string
		value any
	}{}
	if info.ShellyDeviceID != "" {
		queries = append(queries, struct {
			query string
			value any
		}{`SELECT id FROM devices WHERE lower(shelly_device_id)=lower(?)`, info.ShellyDeviceID})
	}
	if info.MACAddress != "" {
		queries = append(queries, struct {
			query string
			value any
		}{`SELECT id FROM devices WHERE mac_address=?`, info.MACAddress})
	}
	ipQuery := `SELECT id FROM devices WHERE network_id=? AND ip_address=? AND (shelly_device_id IS NULL OR shelly_device_id='')`
	if authRequired && info.ShellyDeviceID == "" && info.MACAddress == "" {
		ipQuery = `SELECT id FROM devices WHERE network_id=? AND ip_address=?`
	}
	queries = append(queries, struct {
		query string
		value any
	}{ipQuery, []any{networkID, ipAddress}})
	for _, candidate := range queries {
		var scanErr error
		if values, ok := candidate.value.([]any); ok {
			scanErr = tx.QueryRowContext(ctx, candidate.query, values...).Scan(&id)
		} else {
			scanErr = tx.QueryRowContext(ctx, candidate.query, candidate.value).Scan(&id)
		}
		if scanErr == nil {
			break
		}
		if !errors.Is(scanErr, sql.ErrNoRows) {
			return UpsertResult{}, scanErr
		}
	}
	now := timestamp(time.Now())
	capabilities, _ := json.Marshal(info.Capabilities)
	var capabilitiesUpdate any
	if len(info.Capabilities) > 0 {
		capabilitiesUpdate = string(capabilities)
	}
	rawInfo := nullableJSON(info.RawDeviceInfo)
	rawStatus := nullableJSON(info.RawStatus)
	status := "online"
	if authRequired {
		status = "authentication_required"
	}
	isNew := id == 0
	if isNew {
		result, err := tx.ExecContext(ctx, `INSERT INTO devices (shelly_device_id,name,model,generation,device_type,mac_address,ip_address,hostname,firmware_version,firmware_build,hardware_revision,network_id,authentication_required,online_status,capabilities_json,first_seen_at,last_seen_at,last_successful_scan_at,last_error,raw_device_info,raw_status,created_at,updated_at) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
			nullable(info.ShellyDeviceID), info.Name, info.Model, info.Generation, info.DeviceType, nullable(info.MACAddress), ipAddress, info.Hostname, info.FirmwareVersion, info.FirmwareBuild, info.HardwareRevision, networkID, authRequired || info.AuthenticationRequired, status, string(capabilities), now, now, nullableTime(status == "online", now), lastError, rawInfo, rawStatus, now, now)
		if err != nil {
			return UpsertResult{}, err
		}
		id, _ = result.LastInsertId()
	} else {
		_, err := tx.ExecContext(ctx, `UPDATE devices SET shelly_device_id=coalesce(?,shelly_device_id),name=coalesce(nullif(?,''),name),model=coalesce(nullif(?,''),model),generation=coalesce(nullif(?,0),generation),device_type=coalesce(nullif(?,''),device_type),mac_address=coalesce(?,mac_address),ip_address=?,hostname=coalesce(nullif(?,''),hostname),firmware_version=coalesce(nullif(?,''),firmware_version),firmware_build=coalesce(nullif(?,''),firmware_build),hardware_revision=coalesce(nullif(?,''),hardware_revision),network_id=?,authentication_required=?,online_status=?,consecutive_failures=CASE WHEN ?='online' THEN 0 ELSE consecutive_failures END,capabilities_json=coalesce(?,capabilities_json),last_seen_at=?,last_successful_scan_at=CASE WHEN ?='online' THEN ? ELSE last_successful_scan_at END,last_error=?,raw_device_info=coalesce(?,raw_device_info),raw_status=coalesce(?,raw_status),updated_at=? WHERE id=?`,
			nullable(info.ShellyDeviceID), info.Name, info.Model, info.Generation, info.DeviceType, nullable(info.MACAddress), ipAddress, info.Hostname, info.FirmwareVersion, info.FirmwareBuild, info.HardwareRevision, networkID, authRequired || info.AuthenticationRequired, status, status, capabilitiesUpdate, now, status, now, lastError, rawInfo, rawStatus, now, id)
		if err != nil {
			return UpsertResult{}, err
		}
	}
	if err := tx.Commit(); err != nil {
		return UpsertResult{}, err
	}
	device, err := s.GetDevice(ctx, id)
	return UpsertResult{Device: device, New: isNew}, err
}

func (s *Store) MarkMissedDevices(ctx context.Context, networkID int64, seenIDs []int64, threshold int) error {
	query, args := missedDevicesStatement(networkID, seenIDs, threshold, timestamp(time.Now()))
	_, err := s.db.ExecContext(ctx, query, args...)
	return err
}

func missedDevicesStatement(networkID int64, seenIDs []int64, threshold int, now string) (string, []any) {
	query := `UPDATE devices SET consecutive_failures=consecutive_failures+1,online_status=CASE WHEN consecutive_failures+1>=? THEN 'offline' ELSE online_status END,updated_at=? WHERE network_id=?`
	args := []any{threshold, now, networkID}
	if len(seenIDs) > 0 {
		placeholders := make([]string, len(seenIDs))
		for i, id := range seenIDs {
			placeholders[i] = "?"
			args = append(args, id)
		}
		query += ` AND id NOT IN (` + strings.Join(placeholders, ",") + `)`
	}
	return query, args
}

func nullable(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func nullableJSON(value json.RawMessage) any {
	if len(value) == 0 {
		return nil
	}
	return string(value)
}

func nullableTime(include bool, value string) any {
	if !include {
		return nil
	}
	return value
}

func (s *Store) CreateCredential(ctx context.Context, name, username, encryptedPassword string) (int64, error) {
	now := timestamp(time.Now())
	result, err := s.db.ExecContext(ctx, `INSERT INTO credential_profiles (name,username,encrypted_password,created_at,updated_at) VALUES (?,?,?,?,?)`, name, username, encryptedPassword, now, now)
	if err != nil {
		return 0, err
	}
	return result.LastInsertId()
}

func (s *Store) ListCredentials(ctx context.Context) ([]CredentialProfile, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,name,username FROM credential_profiles ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []CredentialProfile
	for rows.Next() {
		var profile CredentialProfile
		if err := rows.Scan(&profile.ID, &profile.Name, &profile.Username); err != nil {
			return nil, err
		}
		result = append(result, profile)
	}
	return result, rows.Err()
}

func (s *Store) CredentialSecret(ctx context.Context, id int64) (string, string, error) {
	var username, encrypted string
	err := s.db.QueryRowContext(ctx, `SELECT username,encrypted_password FROM credential_profiles WHERE id=?`, id).Scan(&username, &encrypted)
	return username, encrypted, err
}

func (s *Store) DeviceCredentialForAddress(ctx context.Context, networkID int64, address string) (string, string, error) {
	var username, encrypted string
	err := s.db.QueryRowContext(ctx, `SELECT c.username,c.encrypted_password FROM devices d JOIN credential_profiles c ON c.id=d.credential_profile_id WHERE d.network_id=? AND d.ip_address=?`, networkID, address).Scan(&username, &encrypted)
	return username, encrypted, err
}

func (s *Store) CreateScan(ctx context.Context, networkID int64, trigger string, total int) (Scan, error) {
	now := timestamp(time.Now())
	result, err := s.db.ExecContext(ctx, `INSERT INTO scans (network_id,trigger_type,status,addresses_total,created_at,updated_at) SELECT id,?,'queued',?,?,? FROM networks WHERE id=? AND enabled=1 AND deleted_at IS NULL`, trigger, total, now, now, networkID)
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "unique") {
			return Scan{}, errors.New("a scan is already active for this network")
		}
		return Scan{}, err
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return Scan{}, err
	}
	if changed == 0 {
		return Scan{}, errors.New("network is disabled or deleted")
	}
	id, _ := result.LastInsertId()
	return s.GetScan(ctx, id)
}

func (s *Store) StartScan(ctx context.Context, id int64) error {
	now := timestamp(time.Now())
	_, err := s.db.ExecContext(ctx, `UPDATE scans SET status='running',started_at=?,updated_at=? WHERE id=? AND status='queued'`, now, now, id)
	return err
}

func (s *Store) UpdateScanProgress(ctx context.Context, scan Scan) error {
	_, err := s.db.ExecContext(ctx, `UPDATE scans SET addresses_scanned=?,devices_found=?,new_devices_found=?,updated_devices=?,errors_count=?,error_summary=?,updated_at=? WHERE id=?`, scan.AddressesScanned, scan.DevicesFound, scan.NewDevicesFound, scan.UpdatedDevices, scan.ErrorsCount, scan.ErrorSummary, timestamp(time.Now()), scan.ID)
	return err
}

func (s *Store) CompleteScan(ctx context.Context, scan Scan) error {
	now := timestamp(time.Now())
	_, err := s.db.ExecContext(ctx, `UPDATE scans SET status=?,completed_at=?,addresses_scanned=?,devices_found=?,new_devices_found=?,updated_devices=?,errors_count=?,error_summary=?,updated_at=? WHERE id=?`, scan.Status, now, scan.AddressesScanned, scan.DevicesFound, scan.NewDevicesFound, scan.UpdatedDevices, scan.ErrorsCount, scan.ErrorSummary, now, scan.ID)
	return err
}

func (s *Store) FinalizeFullScan(ctx context.Context, scan Scan, seenIDs []int64, threshold int) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	now := timestamp(time.Now())
	query, args := missedDevicesStatement(scan.NetworkID, seenIDs, threshold, now)
	if _, err := tx.ExecContext(ctx, query, args...); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE scans SET status=?,completed_at=?,addresses_scanned=?,devices_found=?,new_devices_found=?,updated_devices=?,errors_count=?,error_summary=?,updated_at=? WHERE id=?`, scan.Status, now, scan.AddressesScanned, scan.DevicesFound, scan.NewDevicesFound, scan.UpdatedDevices, scan.ErrorsCount, scan.ErrorSummary, now, scan.ID); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) GetScan(ctx context.Context, id int64) (Scan, error) {
	return scanScan(s.db.QueryRowContext(ctx, scanSelect+` WHERE s.id=?`, id))
}

const scanSelect = `SELECT s.id,s.network_id,n.name,s.trigger_type,s.status,s.started_at,s.completed_at,s.addresses_total,s.addresses_scanned,s.devices_found,s.new_devices_found,s.updated_devices,s.errors_count,s.error_summary,s.created_at FROM scans s JOIN networks n ON n.id=s.network_id`

func scanScan(scanner interface{ Scan(...any) error }) (Scan, error) {
	var scan Scan
	var started, completed sql.NullString
	var created string
	err := scanner.Scan(&scan.ID, &scan.NetworkID, &scan.NetworkName, &scan.TriggerType, &scan.Status, &started, &completed, &scan.AddressesTotal, &scan.AddressesScanned, &scan.DevicesFound, &scan.NewDevicesFound, &scan.UpdatedDevices, &scan.ErrorsCount, &scan.ErrorSummary, &created)
	if started.Valid {
		value := parseTime(started.String)
		scan.StartedAt = &value
	}
	if completed.Valid {
		value := parseTime(completed.String)
		scan.CompletedAt = &value
	}
	scan.CreatedAt = parseTime(created)
	return scan, err
}

func (s *Store) ListScans(ctx context.Context, limit int) ([]Scan, error) {
	rows, err := s.db.QueryContext(ctx, scanSelect+` ORDER BY s.created_at DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []Scan
	for rows.Next() {
		scan, err := scanScan(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, scan)
	}
	return result, rows.Err()
}

type DashboardStats struct {
	TotalDevices, OnlineDevices, OfflineDevices, AuthDevices, UnassignedDevices int
	TotalRooms, TotalNetworks, RunningScans                                     int
}

func (s *Store) DashboardStats(ctx context.Context) (DashboardStats, error) {
	var stats DashboardStats
	err := s.db.QueryRowContext(ctx, `SELECT count(*),coalesce(sum(online_status='online'),0),coalesce(sum(online_status='offline'),0),coalesce(sum(online_status='authentication_required'),0),coalesce(sum(room_id=1),0) FROM devices`).Scan(&stats.TotalDevices, &stats.OnlineDevices, &stats.OfflineDevices, &stats.AuthDevices, &stats.UnassignedDevices)
	if err != nil {
		return stats, err
	}
	if err := s.db.QueryRowContext(ctx, `SELECT count(*) FROM rooms`).Scan(&stats.TotalRooms); err != nil {
		return stats, err
	}
	if err := s.db.QueryRowContext(ctx, `SELECT count(*) FROM networks WHERE deleted_at IS NULL`).Scan(&stats.TotalNetworks); err != nil {
		return stats, err
	}
	if err := s.db.QueryRowContext(ctx, `SELECT count(*) FROM scans WHERE status IN ('queued','running')`).Scan(&stats.RunningScans); err != nil {
		return stats, err
	}
	return stats, nil
}

func (s *Store) DueScheduledNetworks(ctx context.Context, now time.Time) ([]Network, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT n.id,n.name,n.cidr,n.enabled,n.automatic_scan_enabled,n.scan_interval_seconds,n.scan_concurrency,n.connection_timeout_ms,n.request_timeout_ms,n.created_at,n.updated_at
		FROM networks n
		WHERE n.deleted_at IS NULL AND n.enabled=1 AND n.automatic_scan_enabled=1
		AND NOT EXISTS (SELECT 1 FROM scans active WHERE active.network_id=n.id AND active.status IN ('queued','running'))
		AND (SELECT max(coalesce(completed_at,created_at)) FROM scans previous WHERE previous.network_id=n.id AND previous.trigger_type IN ('manual','scheduled')) IS NULL
		 OR n.deleted_at IS NULL AND n.enabled=1 AND n.automatic_scan_enabled=1
		AND NOT EXISTS (SELECT 1 FROM scans active WHERE active.network_id=n.id AND active.status IN ('queued','running'))
		AND datetime((SELECT max(coalesce(completed_at,created_at)) FROM scans previous WHERE previous.network_id=n.id AND previous.trigger_type IN ('manual','scheduled')), '+' || n.scan_interval_seconds || ' seconds') <= datetime(?)
		ORDER BY n.id`, timestamp(now))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []Network
	for rows.Next() {
		network, err := scanNetwork(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, network)
	}
	return result, rows.Err()
}

func timestamp(value time.Time) string { return value.UTC().Format(time.RFC3339Nano) }

func parseTime(value string) time.Time {
	result, _ := time.Parse(time.RFC3339Nano, value)
	if result.IsZero() {
		result, _ = time.Parse("2006-01-02 15:04:05", value)
	}
	return result
}
