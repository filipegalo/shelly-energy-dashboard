package database

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

func (s *Store) GetEnergyConfig(ctx context.Context) (EnergyConfig, error) {
	return scanEnergyConfig(s.db.QueryRowContext(ctx, `SELECT id,enabled,coalesce(device_id,0),component_key,component_type,capabilities_json,poll_interval_seconds,billing_cycle_start_day,timezone,currency,maximum_expected_power_w,sample_retention_days,minute_retention_days,hourly_retention_days,created_at,updated_at FROM energy_config WHERE id=1`))
}

func (s *Store) SaveEnergyConfig(ctx context.Context, config EnergyConfig) (EnergyConfig, error) {
	now := timestamp(time.Now())
	var deviceID any
	if config.DeviceID != 0 {
		deviceID = config.DeviceID
	}
	_, err := s.db.ExecContext(ctx, `UPDATE energy_config SET enabled=?,device_id=?,component_key=?,component_type=?,capabilities_json=?,poll_interval_seconds=?,billing_cycle_start_day=?,timezone=?,currency=?,maximum_expected_power_w=?,sample_retention_days=?,minute_retention_days=?,hourly_retention_days=?,updated_at=? WHERE id=1`,
		config.Enabled, deviceID, config.ComponentKey, config.ComponentType, config.CapabilitiesJSON, config.PollIntervalSeconds, config.BillingCycleStartDay, config.Timezone, config.Currency, config.MaximumExpectedPowerW, config.SampleRetentionDays, config.MinuteRetentionDays, config.HourlyRetentionDays, now)
	if err != nil {
		return EnergyConfig{}, err
	}
	return s.GetEnergyConfig(ctx)
}

func (s *Store) DisableEnergyMonitoring(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `UPDATE energy_config SET enabled=0,updated_at=? WHERE id=1`, timestamp(time.Now()))
	return err
}

func (s *Store) MarkEnergyDeviceAuthenticationRequired(ctx context.Context, deviceID int64) error {
	_, err := s.db.ExecContext(ctx, `UPDATE devices SET authentication_required=1,online_status='authentication_required',last_error='meter authentication failed',updated_at=? WHERE id=?`, timestamp(time.Now()), deviceID)
	return err
}

func (s *Store) MarkEnergyDeviceOnline(ctx context.Context, deviceID int64) error {
	now := timestamp(time.Now())
	_, err := s.db.ExecContext(ctx, `UPDATE devices SET authentication_required=0,online_status='online',consecutive_failures=0,last_error='',last_seen_at=?,last_successful_scan_at=?,updated_at=? WHERE id=?`, now, now, now, deviceID)
	return err
}

func (s *Store) SaveEnergyBillingSettings(ctx context.Context, config EnergyConfig, pricing *PricingVersion) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	now := time.Now()
	stamp := timestamp(now)
	if _, err := tx.ExecContext(ctx, `UPDATE energy_config SET billing_cycle_start_day=?,timezone=?,sample_retention_days=?,minute_retention_days=?,hourly_retention_days=?,updated_at=? WHERE id=1`, config.BillingCycleStartDay, config.Timezone, config.SampleRetentionDays, config.MinuteRetentionDays, config.HourlyRetentionDays, stamp); err != nil {
		return err
	}
	if pricing != nil {
		effective := pricing.PriceEffectiveFrom
		if effective.IsZero() {
			effective = now
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO electricity_pricing_versions (energy_price_per_kwh_excluding_vat,reference_price_per_kwh_excluding_vat,contracted_power_kva,reduced_vat_enabled,large_family_enabled,reduced_vat_rate,standard_vat_rate,standard_reduced_vat_limit_kwh_per_30_days,large_family_reduced_vat_limit_kwh_per_30_days,electricity_excise_tax_per_kwh,electricity_excise_tax_vat_rate,include_excise_tax,billing_cycle_start_day,currency,region,price_effective_from,created_at,updated_at) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, pricing.EnergyPricePerKWhExcludingVAT, pricing.ReferencePricePerKWhExcludingVAT, pricing.ContractedPowerKVA, pricing.ReducedVATEnabled, pricing.LargeFamilyEnabled, pricing.ReducedVATRate, pricing.StandardVATRate, pricing.StandardReducedVATLimitKWhPer30Days, pricing.LargeFamilyReducedVATLimitKWhPer30Days, pricing.ElectricityExciseTaxPerKWh, pricing.ElectricityExciseTaxVATRate, pricing.IncludeExciseTax, pricing.BillingCycleStartDay, pricing.Currency, pricing.Region, timestamp(effective), stamp, stamp)
		if err != nil {
			return err
		}
	}
	return tx.Commit()
}

func scanEnergyConfig(scanner interface{ Scan(...any) error }) (EnergyConfig, error) {
	var config EnergyConfig
	var createdAt, updatedAt string
	err := scanner.Scan(&config.ID, &config.Enabled, &config.DeviceID, &config.ComponentKey, &config.ComponentType, &config.CapabilitiesJSON, &config.PollIntervalSeconds, &config.BillingCycleStartDay, &config.Timezone, &config.Currency, &config.MaximumExpectedPowerW, &config.SampleRetentionDays, &config.MinuteRetentionDays, &config.HourlyRetentionDays, &createdAt, &updatedAt)
	config.CreatedAt = parseTime(createdAt)
	config.UpdatedAt = parseTime(updatedAt)
	return config, err
}

func (s *Store) CreatePricingVersion(ctx context.Context, pricing PricingVersion) (PricingVersion, error) {
	now := time.Now()
	if pricing.PriceEffectiveFrom.IsZero() {
		pricing.PriceEffectiveFrom = now
	}
	stamp := timestamp(now)
	result, err := s.db.ExecContext(ctx, `INSERT INTO electricity_pricing_versions (energy_price_per_kwh_excluding_vat,reference_price_per_kwh_excluding_vat,contracted_power_kva,reduced_vat_enabled,large_family_enabled,reduced_vat_rate,standard_vat_rate,standard_reduced_vat_limit_kwh_per_30_days,large_family_reduced_vat_limit_kwh_per_30_days,electricity_excise_tax_per_kwh,electricity_excise_tax_vat_rate,include_excise_tax,billing_cycle_start_day,currency,region,price_effective_from,created_at,updated_at) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		pricing.EnergyPricePerKWhExcludingVAT, pricing.ReferencePricePerKWhExcludingVAT, pricing.ContractedPowerKVA, pricing.ReducedVATEnabled, pricing.LargeFamilyEnabled, pricing.ReducedVATRate, pricing.StandardVATRate, pricing.StandardReducedVATLimitKWhPer30Days, pricing.LargeFamilyReducedVATLimitKWhPer30Days, pricing.ElectricityExciseTaxPerKWh, pricing.ElectricityExciseTaxVATRate, pricing.IncludeExciseTax, pricing.BillingCycleStartDay, pricing.Currency, pricing.Region, timestamp(pricing.PriceEffectiveFrom), stamp, stamp)
	if err != nil {
		return PricingVersion{}, err
	}
	pricing.ID, err = result.LastInsertId()
	if err != nil {
		return PricingVersion{}, err
	}
	return s.pricingVersionByID(ctx, pricing.ID)
}

func (s *Store) ActivePricingVersionAt(ctx context.Context, at time.Time) (PricingVersion, error) {
	if at.IsZero() {
		at = time.Now()
	}
	return scanPricingVersion(s.db.QueryRowContext(ctx, pricingVersionSelect+` WHERE price_effective_from<=? ORDER BY price_effective_from DESC,id DESC LIMIT 1`, timestamp(at)))
}

func (s *Store) ListPricingVersions(ctx context.Context) ([]PricingVersion, error) {
	rows, err := s.db.QueryContext(ctx, pricingVersionSelect+` ORDER BY price_effective_from DESC,id DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]PricingVersion, 0)
	for rows.Next() {
		pricing, err := scanPricingVersion(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, pricing)
	}
	return result, rows.Err()
}

const pricingVersionSelect = `SELECT id,energy_price_per_kwh_excluding_vat,reference_price_per_kwh_excluding_vat,contracted_power_kva,reduced_vat_enabled,large_family_enabled,reduced_vat_rate,standard_vat_rate,standard_reduced_vat_limit_kwh_per_30_days,large_family_reduced_vat_limit_kwh_per_30_days,electricity_excise_tax_per_kwh,electricity_excise_tax_vat_rate,include_excise_tax,billing_cycle_start_day,currency,region,price_effective_from,created_at,updated_at FROM electricity_pricing_versions`

func (s *Store) pricingVersionByID(ctx context.Context, id int64) (PricingVersion, error) {
	return scanPricingVersion(s.db.QueryRowContext(ctx, pricingVersionSelect+` WHERE id=?`, id))
}

func scanPricingVersion(scanner interface{ Scan(...any) error }) (PricingVersion, error) {
	var pricing PricingVersion
	var effectiveFrom, createdAt, updatedAt string
	err := scanner.Scan(&pricing.ID, &pricing.EnergyPricePerKWhExcludingVAT, &pricing.ReferencePricePerKWhExcludingVAT, &pricing.ContractedPowerKVA, &pricing.ReducedVATEnabled, &pricing.LargeFamilyEnabled, &pricing.ReducedVATRate, &pricing.StandardVATRate, &pricing.StandardReducedVATLimitKWhPer30Days, &pricing.LargeFamilyReducedVATLimitKWhPer30Days, &pricing.ElectricityExciseTaxPerKWh, &pricing.ElectricityExciseTaxVATRate, &pricing.IncludeExciseTax, &pricing.BillingCycleStartDay, &pricing.Currency, &pricing.Region, &effectiveFrom, &createdAt, &updatedAt)
	pricing.PriceEffectiveFrom = parseTime(effectiveFrom)
	pricing.CreatedAt = parseTime(createdAt)
	pricing.UpdatedAt = parseTime(updatedAt)
	return pricing, err
}

func (s *Store) InsertEnergySample(ctx context.Context, sample EnergySample) (EnergySample, error) {
	now := time.Now()
	if sample.RecordedAt.IsZero() {
		sample.RecordedAt = now
	}
	if sample.ReadingQuality == "" {
		sample.ReadingQuality = "measured"
	}
	result, err := s.db.ExecContext(ctx, `INSERT INTO energy_samples (device_id,component_key,recorded_at,power_w,imported_energy_total_wh,exported_energy_total_wh,voltage_v,current_a,power_factor,frequency_hz,reading_quality,created_at) VALUES (?,?,?,?,?,?,?,?,?,?,?,?)`, sample.DeviceID, sample.ComponentKey, timestamp(sample.RecordedAt), sample.PowerW, sample.ImportedEnergyTotalWh, sample.ExportedEnergyTotalWh, sample.VoltageV, sample.CurrentA, sample.PowerFactor, sample.FrequencyHz, sample.ReadingQuality, timestamp(now))
	if err != nil {
		return EnergySample{}, err
	}
	sample.ID, err = result.LastInsertId()
	if err != nil {
		return EnergySample{}, err
	}
	return s.energySampleByID(ctx, sample.ID)
}

func (s *Store) PersistEnergyReading(ctx context.Context, sample EnergySample, aggregates []EnergyAggregate) (EnergySample, error) {
	now := time.Now()
	if sample.RecordedAt.IsZero() {
		sample.RecordedAt = now
	}
	if sample.ReadingQuality == "" {
		sample.ReadingQuality = "measured"
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return EnergySample{}, err
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, `INSERT INTO energy_samples (device_id,component_key,recorded_at,power_w,imported_energy_total_wh,exported_energy_total_wh,voltage_v,current_a,power_factor,frequency_hz,reading_quality,created_at) VALUES (?,?,?,?,?,?,?,?,?,?,?,?)`, sample.DeviceID, sample.ComponentKey, timestamp(sample.RecordedAt), sample.PowerW, sample.ImportedEnergyTotalWh, sample.ExportedEnergyTotalWh, sample.VoltageV, sample.CurrentA, sample.PowerFactor, sample.FrequencyHz, sample.ReadingQuality, timestamp(now))
	if err != nil {
		return EnergySample{}, err
	}
	for _, aggregate := range aggregates {
		if aggregate.CalculationQuality == "" {
			aggregate.CalculationQuality = "measured"
		}
		if err := upsertEnergyAggregate(ctx, tx, aggregate, timestamp(now)); err != nil {
			return EnergySample{}, err
		}
	}
	if sample.ID, err = result.LastInsertId(); err != nil {
		return EnergySample{}, err
	}
	if err := tx.Commit(); err != nil {
		return EnergySample{}, err
	}
	sample.CreatedAt = now
	return sample, nil
}

func (s *Store) LatestEnergySample(ctx context.Context, deviceID int64, componentKey string) (EnergySample, error) {
	return scanEnergySample(s.db.QueryRowContext(ctx, energySampleSelect+` WHERE device_id=? AND component_key=? ORDER BY recorded_at DESC,id DESC LIMIT 1`, deviceID, componentKey))
}

func (s *Store) EnergyHistoryStart(ctx context.Context, deviceID int64, componentKey string) (time.Time, error) {
	var value sql.NullString
	err := s.db.QueryRowContext(ctx, `SELECT min(recorded_at) FROM (
		SELECT recorded_at FROM energy_samples WHERE device_id=? AND component_key=?
		UNION ALL SELECT interval_start FROM energy_interval_aggregates WHERE device_id=? AND component_key=?
		UNION ALL SELECT occurred_at FROM meter_events WHERE device_id=? AND component_key=? AND event_type IN ('meter_selected','meter_changed','collection_started')
	)`, deviceID, componentKey, deviceID, componentKey, deviceID, componentKey).Scan(&value)
	if err != nil {
		return time.Time{}, err
	}
	if !value.Valid {
		return time.Time{}, sql.ErrNoRows
	}
	return parseTime(value.String), nil
}

const energySampleSelect = `SELECT id,device_id,component_key,recorded_at,power_w,imported_energy_total_wh,exported_energy_total_wh,voltage_v,current_a,power_factor,frequency_hz,reading_quality,created_at FROM energy_samples`

func (s *Store) energySampleByID(ctx context.Context, id int64) (EnergySample, error) {
	return scanEnergySample(s.db.QueryRowContext(ctx, energySampleSelect+` WHERE id=?`, id))
}

func scanEnergySample(scanner interface{ Scan(...any) error }) (EnergySample, error) {
	var sample EnergySample
	var recordedAt, createdAt string
	var power, imported, exported, voltage, current, factor, frequency sql.NullFloat64
	err := scanner.Scan(&sample.ID, &sample.DeviceID, &sample.ComponentKey, &recordedAt, &power, &imported, &exported, &voltage, &current, &factor, &frequency, &sample.ReadingQuality, &createdAt)
	sample.RecordedAt = parseTime(recordedAt)
	sample.CreatedAt = parseTime(createdAt)
	sample.PowerW = floatPointer(power)
	sample.ImportedEnergyTotalWh = floatPointer(imported)
	sample.ExportedEnergyTotalWh = floatPointer(exported)
	sample.VoltageV = floatPointer(voltage)
	sample.CurrentA = floatPointer(current)
	sample.PowerFactor = floatPointer(factor)
	sample.FrequencyHz = floatPointer(frequency)
	return sample, err
}

func (s *Store) UpsertEnergyAggregate(ctx context.Context, aggregate EnergyAggregate) (EnergyAggregate, error) {
	now := timestamp(time.Now())
	if aggregate.CalculationQuality == "" {
		aggregate.CalculationQuality = "measured"
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO energy_interval_aggregates (interval_type,interval_start,interval_end,device_id,component_key,energy_consumed_wh,average_power_w,minimum_power_w,maximum_power_w,sample_count,missing_sample_count,calculation_quality,created_at,updated_at) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?) ON CONFLICT(interval_type,interval_start,device_id,component_key) DO UPDATE SET interval_end=excluded.interval_end,energy_consumed_wh=excluded.energy_consumed_wh,average_power_w=excluded.average_power_w,minimum_power_w=excluded.minimum_power_w,maximum_power_w=excluded.maximum_power_w,sample_count=excluded.sample_count,missing_sample_count=excluded.missing_sample_count,calculation_quality=excluded.calculation_quality,updated_at=excluded.updated_at`, aggregate.IntervalType, timestamp(aggregate.IntervalStart), timestamp(aggregate.IntervalEnd), aggregate.DeviceID, aggregate.ComponentKey, aggregate.EnergyConsumedWh, aggregate.AveragePowerW, aggregate.MinimumPowerW, aggregate.MaximumPowerW, aggregate.SampleCount, aggregate.MissingSampleCount, aggregate.CalculationQuality, now, now)
	if err != nil {
		return EnergyAggregate{}, err
	}
	return scanEnergyAggregate(s.db.QueryRowContext(ctx, energyAggregateSelect+` WHERE interval_type=? AND interval_start=? AND device_id=? AND component_key=?`, aggregate.IntervalType, timestamp(aggregate.IntervalStart), aggregate.DeviceID, aggregate.ComponentKey))
}

func (s *Store) UpsertEnergyAggregates(ctx context.Context, aggregates []EnergyAggregate) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	now := timestamp(time.Now())
	for _, aggregate := range aggregates {
		if aggregate.CalculationQuality == "" {
			aggregate.CalculationQuality = "measured"
		}
		if err := upsertEnergyAggregate(ctx, tx, aggregate, now); err != nil {
			return err
		}
	}
	return tx.Commit()
}

type energyAggregateExecer interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}

func upsertEnergyAggregate(ctx context.Context, execer energyAggregateExecer, aggregate EnergyAggregate, now string) error {
	_, err := execer.ExecContext(ctx, `INSERT INTO energy_interval_aggregates (interval_type,interval_start,interval_end,device_id,component_key,energy_consumed_wh,average_power_w,minimum_power_w,maximum_power_w,sample_count,missing_sample_count,calculation_quality,created_at,updated_at) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?) ON CONFLICT(interval_type,interval_start,device_id,component_key) DO UPDATE SET interval_end=excluded.interval_end,energy_consumed_wh=excluded.energy_consumed_wh,average_power_w=excluded.average_power_w,minimum_power_w=excluded.minimum_power_w,maximum_power_w=excluded.maximum_power_w,sample_count=excluded.sample_count,missing_sample_count=excluded.missing_sample_count,calculation_quality=excluded.calculation_quality,updated_at=excluded.updated_at`, aggregate.IntervalType, timestamp(aggregate.IntervalStart), timestamp(aggregate.IntervalEnd), aggregate.DeviceID, aggregate.ComponentKey, aggregate.EnergyConsumedWh, aggregate.AveragePowerW, aggregate.MinimumPowerW, aggregate.MaximumPowerW, aggregate.SampleCount, aggregate.MissingSampleCount, aggregate.CalculationQuality, now, now)
	return err
}

func (s *Store) ListEnergyAggregates(ctx context.Context, deviceID int64, componentKey string, start, end time.Time, intervalType string) ([]EnergyAggregate, error) {
	rows, err := s.db.QueryContext(ctx, energyAggregateSelect+` WHERE device_id=? AND component_key=? AND interval_type=? AND interval_start>=? AND interval_start<? ORDER BY interval_start`, deviceID, componentKey, intervalType, timestamp(start), timestamp(end))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]EnergyAggregate, 0)
	for rows.Next() {
		aggregate, err := scanEnergyAggregate(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, aggregate)
	}
	return result, rows.Err()
}

func (s *Store) SumEnergyWh(ctx context.Context, deviceID int64, componentKey string, start, end time.Time) (float64, error) {
	return s.SumEnergyWhByInterval(ctx, deviceID, componentKey, start, end, "minute")
}

func (s *Store) SumEnergyWhByInterval(ctx context.Context, deviceID int64, componentKey string, start, end time.Time, intervalType string) (float64, error) {
	if intervalType != "minute" && intervalType != "hour" && intervalType != "day" {
		return 0, errors.New("energy interval must be minute, hour, or day")
	}
	var total float64
	err := s.db.QueryRowContext(ctx, `SELECT coalesce(sum(energy_consumed_wh),0) FROM energy_interval_aggregates WHERE device_id=? AND component_key=? AND interval_type=? AND interval_start>=? AND interval_start<?`, deviceID, componentKey, intervalType, timestamp(start), timestamp(end)).Scan(&total)
	return total, err
}

const energyAggregateSelect = `SELECT id,interval_type,interval_start,interval_end,device_id,component_key,energy_consumed_wh,average_power_w,minimum_power_w,maximum_power_w,sample_count,missing_sample_count,calculation_quality,created_at,updated_at FROM energy_interval_aggregates`

func scanEnergyAggregate(scanner interface{ Scan(...any) error }) (EnergyAggregate, error) {
	var aggregate EnergyAggregate
	var intervalStart, intervalEnd, createdAt, updatedAt string
	var average, minimum, maximum sql.NullFloat64
	err := scanner.Scan(&aggregate.ID, &aggregate.IntervalType, &intervalStart, &intervalEnd, &aggregate.DeviceID, &aggregate.ComponentKey, &aggregate.EnergyConsumedWh, &average, &minimum, &maximum, &aggregate.SampleCount, &aggregate.MissingSampleCount, &aggregate.CalculationQuality, &createdAt, &updatedAt)
	aggregate.IntervalStart = parseTime(intervalStart)
	aggregate.IntervalEnd = parseTime(intervalEnd)
	aggregate.CreatedAt = parseTime(createdAt)
	aggregate.UpdatedAt = parseTime(updatedAt)
	aggregate.AveragePowerW = floatPointer(average)
	aggregate.MinimumPowerW = floatPointer(minimum)
	aggregate.MaximumPowerW = floatPointer(maximum)
	return aggregate, err
}

func (s *Store) InsertMeterEvent(ctx context.Context, event MeterEvent) (MeterEvent, error) {
	now := time.Now()
	if event.OccurredAt.IsZero() {
		event.OccurredAt = now
	}
	if event.DetailsJSON == "" {
		event.DetailsJSON = `{}`
	}
	var deviceID any
	if event.DeviceID != 0 {
		deviceID = event.DeviceID
	}
	result, err := s.db.ExecContext(ctx, `INSERT INTO meter_events (device_id,component_key,event_type,occurred_at,details_json,created_at) VALUES (?,?,?,?,?,?)`, deviceID, event.ComponentKey, event.EventType, timestamp(event.OccurredAt), event.DetailsJSON, timestamp(now))
	if err != nil {
		return MeterEvent{}, err
	}
	event.ID, err = result.LastInsertId()
	if err != nil {
		return MeterEvent{}, err
	}
	return s.meterEventByID(ctx, event.ID)
}

func (s *Store) ListMeterEvents(ctx context.Context, deviceID int64, componentKey string, limit int) ([]MeterEvent, error) {
	if limit <= 0 || limit > 1000 {
		limit = 100
	}
	query := meterEventSelect + ` WHERE 1=1`
	args := make([]any, 0, 3)
	if deviceID != 0 {
		query += ` AND device_id=?`
		args = append(args, deviceID)
	}
	if componentKey != "" {
		query += ` AND component_key=?`
		args = append(args, componentKey)
	}
	query += ` ORDER BY occurred_at DESC,id DESC LIMIT ?`
	args = append(args, limit)
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]MeterEvent, 0)
	for rows.Next() {
		event, err := scanMeterEvent(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, event)
	}
	return result, rows.Err()
}

const meterEventSelect = `SELECT id,coalesce(device_id,0),component_key,event_type,occurred_at,details_json,created_at FROM meter_events`

func (s *Store) meterEventByID(ctx context.Context, id int64) (MeterEvent, error) {
	return scanMeterEvent(s.db.QueryRowContext(ctx, meterEventSelect+` WHERE id=?`, id))
}

func scanMeterEvent(scanner interface{ Scan(...any) error }) (MeterEvent, error) {
	var event MeterEvent
	var occurredAt, createdAt string
	err := scanner.Scan(&event.ID, &event.DeviceID, &event.ComponentKey, &event.EventType, &occurredAt, &event.DetailsJSON, &createdAt)
	event.OccurredAt = parseTime(occurredAt)
	event.CreatedAt = parseTime(createdAt)
	return event, err
}

func (s *Store) CleanupEnergyData(ctx context.Context, now time.Time) (EnergyCleanupResult, error) {
	config, err := s.GetEnergyConfig(ctx)
	if err != nil {
		return EnergyCleanupResult{}, err
	}
	if now.IsZero() {
		now = time.Now()
	}
	location, err := time.LoadLocation(config.Timezone)
	if err != nil {
		location = time.UTC
	}
	cycleStart := currentBillingCycleStart(now.In(location), config.BillingCycleStartDay)
	sampleCutoff := protectedRetentionCutoff(now.AddDate(0, 0, -config.SampleRetentionDays), cycleStart)
	minuteCutoff := protectedRetentionCutoff(now.AddDate(0, 0, -config.MinuteRetentionDays), cycleStart)
	hourlyCutoff := protectedRetentionCutoff(now.AddDate(0, 0, -config.HourlyRetentionDays), cycleStart)

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return EnergyCleanupResult{}, err
	}
	defer tx.Rollback()
	var result EnergyCleanupResult
	if result.SamplesDeleted, err = deleteBefore(ctx, tx, `DELETE FROM energy_samples WHERE recorded_at<?`, sampleCutoff); err != nil {
		return EnergyCleanupResult{}, err
	}
	if result.MinuteAggregatesDeleted, err = deleteBefore(ctx, tx, `DELETE FROM energy_interval_aggregates WHERE interval_type='minute' AND interval_start<?`, minuteCutoff); err != nil {
		return EnergyCleanupResult{}, err
	}
	if result.HourlyAggregatesDeleted, err = deleteBefore(ctx, tx, `DELETE FROM energy_interval_aggregates WHERE interval_type='hour' AND interval_start<?`, hourlyCutoff); err != nil {
		return EnergyCleanupResult{}, err
	}
	if err := tx.Commit(); err != nil {
		return EnergyCleanupResult{}, err
	}
	return result, nil
}

func floatPointer(value sql.NullFloat64) *float64 {
	if !value.Valid {
		return nil
	}
	result := value.Float64
	return &result
}

func deleteBefore(ctx context.Context, tx *Tx, query string, cutoff time.Time) (int64, error) {
	result, err := tx.ExecContext(ctx, query, timestamp(cutoff))
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

func protectedRetentionCutoff(retentionCutoff, cycleStart time.Time) time.Time {
	if retentionCutoff.After(cycleStart) {
		return cycleStart
	}
	return retentionCutoff
}

func currentBillingCycleStart(now time.Time, startDay int) time.Time {
	year, month := now.Year(), now.Month()
	day := min(startDay, daysInMonth(year, month, now.Location()))
	start := time.Date(year, month, day, 0, 0, 0, 0, now.Location())
	if !now.Before(start) {
		return start
	}
	previous := time.Date(year, month-1, 1, 0, 0, 0, 0, now.Location())
	day = min(startDay, daysInMonth(previous.Year(), previous.Month(), now.Location()))
	return time.Date(previous.Year(), previous.Month(), day, 0, 0, 0, 0, now.Location())
}

func daysInMonth(year int, month time.Month, location *time.Location) int {
	return time.Date(year, month+1, 0, 0, 0, 0, 0, location).Day()
}
