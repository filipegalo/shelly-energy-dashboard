PRAGMA foreign_keys = ON;

CREATE TABLE IF NOT EXISTS schema_migrations (
  version INTEGER PRIMARY KEY,
  applied_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS rooms (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  name TEXT NOT NULL COLLATE NOCASE UNIQUE,
  description TEXT NOT NULL DEFAULT '',
  icon TEXT NOT NULL DEFAULT 'home',
  sort_order INTEGER NOT NULL DEFAULT 0,
  is_system INTEGER NOT NULL DEFAULT 0 CHECK (is_system IN (0,1)),
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);

INSERT OR IGNORE INTO rooms (id,name,description,icon,sort_order,is_system,created_at,updated_at)
VALUES (1,'Unassigned','Devices awaiting room assignment','inbox',0,1,datetime('now'),datetime('now'));

CREATE TABLE IF NOT EXISTS networks (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  name TEXT NOT NULL COLLATE NOCASE,
  cidr TEXT NOT NULL,
  enabled INTEGER NOT NULL DEFAULT 1 CHECK (enabled IN (0,1)),
  automatic_scan_enabled INTEGER NOT NULL DEFAULT 0 CHECK (automatic_scan_enabled IN (0,1)),
  scan_interval_seconds INTEGER NOT NULL DEFAULT 300 CHECK (scan_interval_seconds BETWEEN 10 AND 604800),
  scan_concurrency INTEGER NOT NULL DEFAULT 32 CHECK (scan_concurrency BETWEEN 1 AND 256),
  connection_timeout_ms INTEGER NOT NULL DEFAULT 750 CHECK (connection_timeout_ms BETWEEN 50 AND 10000),
  request_timeout_ms INTEGER NOT NULL DEFAULT 2000 CHECK (request_timeout_ms BETWEEN 100 AND 30000),
  deleted_at TEXT,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_networks_active_name ON networks(lower(name)) WHERE deleted_at IS NULL;
CREATE UNIQUE INDEX IF NOT EXISTS idx_networks_active_cidr ON networks(cidr) WHERE deleted_at IS NULL;

CREATE TABLE IF NOT EXISTS credential_profiles (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  name TEXT NOT NULL COLLATE NOCASE UNIQUE,
  username TEXT NOT NULL,
  encrypted_password TEXT NOT NULL,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS devices (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  shelly_device_id TEXT,
  name TEXT NOT NULL DEFAULT '',
  custom_name TEXT NOT NULL DEFAULT '',
  model TEXT NOT NULL DEFAULT '',
  generation INTEGER NOT NULL DEFAULT 0,
  device_type TEXT NOT NULL DEFAULT '',
  mac_address TEXT,
  ip_address TEXT NOT NULL,
  hostname TEXT NOT NULL DEFAULT '',
  firmware_version TEXT NOT NULL DEFAULT '',
  firmware_build TEXT NOT NULL DEFAULT '',
  hardware_revision TEXT NOT NULL DEFAULT '',
  room_id INTEGER NOT NULL DEFAULT 1 REFERENCES rooms(id) ON DELETE SET DEFAULT,
  network_id INTEGER REFERENCES networks(id) ON DELETE SET NULL,
  credential_profile_id INTEGER REFERENCES credential_profiles(id) ON DELETE SET NULL,
  authentication_required INTEGER NOT NULL DEFAULT 0 CHECK (authentication_required IN (0,1)),
  online_status TEXT NOT NULL DEFAULT 'unknown' CHECK (online_status IN ('online','offline','unknown','authentication_required')),
  consecutive_failures INTEGER NOT NULL DEFAULT 0 CHECK (consecutive_failures >= 0),
  capabilities_json TEXT NOT NULL DEFAULT '[]' CHECK (json_valid(capabilities_json)),
  first_seen_at TEXT NOT NULL,
  last_seen_at TEXT NOT NULL,
  last_successful_scan_at TEXT,
  last_error TEXT NOT NULL DEFAULT '',
  raw_device_info TEXT CHECK (raw_device_info IS NULL OR json_valid(raw_device_info)),
  raw_status TEXT CHECK (raw_status IS NULL OR json_valid(raw_status)),
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_devices_shelly_id ON devices(lower(shelly_device_id)) WHERE shelly_device_id IS NOT NULL AND shelly_device_id <> '';
CREATE UNIQUE INDEX IF NOT EXISTS idx_devices_mac ON devices(mac_address) WHERE mac_address IS NOT NULL AND mac_address <> '';
CREATE INDEX IF NOT EXISTS idx_devices_network_ip ON devices(network_id, ip_address);
CREATE INDEX IF NOT EXISTS idx_devices_room_status ON devices(room_id, online_status);

CREATE TABLE IF NOT EXISTS scans (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  network_id INTEGER NOT NULL REFERENCES networks(id) ON DELETE RESTRICT,
  trigger_type TEXT NOT NULL DEFAULT 'manual' CHECK (trigger_type IN ('manual','scheduled','refresh','manual_device')),
  status TEXT NOT NULL CHECK (status IN ('queued','running','completed','completed_with_errors','failed','cancelled')),
  started_at TEXT,
  completed_at TEXT,
  addresses_total INTEGER NOT NULL DEFAULT 0,
  addresses_scanned INTEGER NOT NULL DEFAULT 0,
  devices_found INTEGER NOT NULL DEFAULT 0,
  new_devices_found INTEGER NOT NULL DEFAULT 0,
  updated_devices INTEGER NOT NULL DEFAULT 0,
  errors_count INTEGER NOT NULL DEFAULT 0,
  error_summary TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_scans_one_active_network ON scans(network_id) WHERE status IN ('queued','running');
CREATE INDEX IF NOT EXISTS idx_scans_network_created ON scans(network_id, created_at DESC);

INSERT OR IGNORE INTO schema_migrations (version,applied_at) VALUES (1,datetime('now'));

CREATE TABLE IF NOT EXISTS energy_config (
  id INTEGER PRIMARY KEY CHECK (id = 1),
  enabled INTEGER NOT NULL DEFAULT 0 CHECK (enabled IN (0,1)),
  device_id INTEGER REFERENCES devices(id) ON DELETE SET NULL,
  component_key TEXT NOT NULL DEFAULT '',
  component_type TEXT NOT NULL DEFAULT '',
  capabilities_json TEXT NOT NULL DEFAULT '{}' CHECK (json_valid(capabilities_json)),
  poll_interval_seconds INTEGER NOT NULL DEFAULT 10 CHECK (poll_interval_seconds BETWEEN 5 AND 300),
  billing_cycle_start_day INTEGER NOT NULL DEFAULT 11 CHECK (billing_cycle_start_day BETWEEN 1 AND 31),
  timezone TEXT NOT NULL DEFAULT 'Europe/Lisbon',
  currency TEXT NOT NULL DEFAULT 'EUR' CHECK (length(currency) = 3),
  maximum_expected_power_w REAL NOT NULL DEFAULT 30000 CHECK (maximum_expected_power_w > 0),
  sample_retention_days INTEGER NOT NULL DEFAULT 7 CHECK (sample_retention_days >= 1),
  minute_retention_days INTEGER NOT NULL DEFAULT 90 CHECK (minute_retention_days >= 1),
  hourly_retention_days INTEGER NOT NULL DEFAULT 730 CHECK (hourly_retention_days >= 1),
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);

INSERT OR IGNORE INTO energy_config (id,created_at,updated_at)
VALUES (1,strftime('%Y-%m-%dT%H:%M:%fZ','now'),strftime('%Y-%m-%dT%H:%M:%fZ','now'));

CREATE TABLE IF NOT EXISTS electricity_pricing_versions (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  energy_price_per_kwh_excluding_vat TEXT NOT NULL DEFAULT '0.137378' CHECK (trim(energy_price_per_kwh_excluding_vat) <> ''),
  reference_price_per_kwh_excluding_vat TEXT NOT NULL DEFAULT '0.165516' CHECK (trim(reference_price_per_kwh_excluding_vat) <> ''),
  contracted_power_kva TEXT NOT NULL DEFAULT '5.75' CHECK (trim(contracted_power_kva) <> ''),
  reduced_vat_enabled INTEGER NOT NULL DEFAULT 1 CHECK (reduced_vat_enabled IN (0,1)),
  large_family_enabled INTEGER NOT NULL DEFAULT 0 CHECK (large_family_enabled IN (0,1)),
  reduced_vat_rate TEXT NOT NULL DEFAULT '0.06' CHECK (trim(reduced_vat_rate) <> ''),
  standard_vat_rate TEXT NOT NULL DEFAULT '0.23' CHECK (trim(standard_vat_rate) <> ''),
  standard_reduced_vat_limit_kwh_per_30_days TEXT NOT NULL DEFAULT '200' CHECK (trim(standard_reduced_vat_limit_kwh_per_30_days) <> ''),
  large_family_reduced_vat_limit_kwh_per_30_days TEXT NOT NULL DEFAULT '300' CHECK (trim(large_family_reduced_vat_limit_kwh_per_30_days) <> ''),
  electricity_excise_tax_per_kwh TEXT NOT NULL DEFAULT '0.001000' CHECK (trim(electricity_excise_tax_per_kwh) <> ''),
  electricity_excise_tax_vat_rate TEXT NOT NULL DEFAULT '0.23' CHECK (trim(electricity_excise_tax_vat_rate) <> ''),
  include_excise_tax INTEGER NOT NULL DEFAULT 1 CHECK (include_excise_tax IN (0,1)),
  billing_cycle_start_day INTEGER NOT NULL DEFAULT 11 CHECK (billing_cycle_start_day BETWEEN 1 AND 31),
  currency TEXT NOT NULL DEFAULT 'EUR' CHECK (length(currency) = 3),
  region TEXT NOT NULL DEFAULT 'Portugal Continental',
  price_effective_from TEXT NOT NULL,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_electricity_pricing_effective
  ON electricity_pricing_versions(price_effective_from DESC, id DESC);

CREATE UNIQUE INDEX IF NOT EXISTS idx_electricity_pricing_effective_time
  ON electricity_pricing_versions(price_effective_from);

INSERT OR IGNORE INTO electricity_pricing_versions (
  id,price_effective_from,created_at,updated_at
) VALUES (1,'1970-01-01T00:00:00Z',strftime('%Y-%m-%dT%H:%M:%fZ','now'),strftime('%Y-%m-%dT%H:%M:%fZ','now'));

-- The seeded tariff is the fallback for the partial hour/day buckets created
-- immediately after first setup. User-created pricing versions remain unchanged.
UPDATE OR IGNORE electricity_pricing_versions
SET price_effective_from='1970-01-01T00:00:00Z'
WHERE id=1 AND price_effective_from<>'1970-01-01T00:00:00Z';

CREATE TABLE IF NOT EXISTS energy_samples (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  device_id INTEGER NOT NULL REFERENCES devices(id) ON DELETE RESTRICT,
  component_key TEXT NOT NULL,
  recorded_at TEXT NOT NULL,
  power_w REAL,
  imported_energy_total_wh REAL,
  exported_energy_total_wh REAL,
  voltage_v REAL,
  current_a REAL,
  power_factor REAL,
  frequency_hz REAL,
  reading_quality TEXT NOT NULL CHECK (reading_quality IN ('measured','calculated','partial','stale','counter_reset')),
  created_at TEXT NOT NULL,
  UNIQUE(device_id, component_key, recorded_at)
);

CREATE INDEX IF NOT EXISTS idx_energy_samples_source_time
  ON energy_samples(device_id, component_key, recorded_at DESC);

CREATE TABLE IF NOT EXISTS energy_interval_aggregates (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  interval_type TEXT NOT NULL CHECK (interval_type IN ('minute','hour','day')),
  interval_start TEXT NOT NULL,
  interval_end TEXT NOT NULL CHECK (interval_end > interval_start),
  device_id INTEGER NOT NULL REFERENCES devices(id) ON DELETE RESTRICT,
  component_key TEXT NOT NULL,
  energy_consumed_wh REAL NOT NULL DEFAULT 0 CHECK (energy_consumed_wh >= 0),
  average_power_w REAL,
  minimum_power_w REAL,
  maximum_power_w REAL,
  sample_count INTEGER NOT NULL DEFAULT 0 CHECK (sample_count >= 0),
  missing_sample_count INTEGER NOT NULL DEFAULT 0 CHECK (missing_sample_count >= 0),
  calculation_quality TEXT NOT NULL CHECK (calculation_quality IN ('measured','calculated','partial','stale','counter_reset','missing')),
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  UNIQUE(interval_type, interval_start, device_id, component_key)
);

CREATE INDEX IF NOT EXISTS idx_energy_aggregates_source_type_time
  ON energy_interval_aggregates(device_id, component_key, interval_type, interval_start);

CREATE TABLE IF NOT EXISTS meter_events (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  device_id INTEGER REFERENCES devices(id) ON DELETE SET NULL,
  component_key TEXT NOT NULL DEFAULT '',
  event_type TEXT NOT NULL CHECK (event_type IN ('meter_selected','meter_changed','meter_offline','meter_online','energy_counter_reset','energy_counter_rollover','invalid_reading','collection_started','collection_stopped')),
  occurred_at TEXT NOT NULL,
  details_json TEXT NOT NULL DEFAULT '{}' CHECK (json_valid(details_json)),
  created_at TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_meter_events_source_time
  ON meter_events(device_id, component_key, occurred_at DESC);

INSERT OR IGNORE INTO schema_migrations (version,applied_at) VALUES (2,datetime('now'));
