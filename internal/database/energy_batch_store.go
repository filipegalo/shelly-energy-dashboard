package database

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

const maximumBatchedEnergySources = 1000

type EnergySourceRef struct {
	DeviceID     int64
	ComponentKey string
}

func (s *Store) ListEnergyAggregatesForSources(ctx context.Context, sources []EnergySourceRef, start, end time.Time, intervalType string) ([]EnergyAggregate, error) {
	if start.IsZero() || end.IsZero() || !start.Before(end) {
		return nil, errors.New("energy aggregate range must have increasing bounds")
	}
	if intervalType != "minute" && intervalType != "hour" && intervalType != "day" {
		return nil, errors.New("energy interval must be minute, hour, or day")
	}
	unique, err := normalizeEnergySourceRefs(sources)
	if err != nil || len(unique) == 0 {
		return []EnergyAggregate{}, err
	}

	values, args := energySourceValues(unique)
	args = append(args, intervalType, timestamp(end), timestamp(start))
	query := `WITH requested(device_id,component_key) AS (VALUES ` + values + `)
		SELECT a.id,a.interval_type,a.interval_start,a.interval_end,a.device_id,a.component_key,a.energy_consumed_wh,a.average_power_w,a.minimum_power_w,a.maximum_power_w,a.sample_count,a.missing_sample_count,a.calculation_quality,a.created_at,a.updated_at
		FROM energy_interval_aggregates a
		JOIN requested requested_source ON requested_source.device_id=a.device_id AND requested_source.component_key=a.component_key
		WHERE a.interval_type=? AND a.interval_start<? AND a.interval_end>?
		ORDER BY a.device_id,a.component_key,a.interval_start`
	rows, err := s.db.QueryContext(ctx, query, args...)
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

func (s *Store) ListLatestEnergySamplesForSources(ctx context.Context, sources []EnergySourceRef) (map[EnergySourceRef]EnergySample, error) {
	unique, err := normalizeEnergySourceRefs(sources)
	if err != nil || len(unique) == 0 {
		return map[EnergySourceRef]EnergySample{}, err
	}

	values, args := energySourceValues(unique)
	query := `WITH requested(device_id,component_key) AS (VALUES ` + values + `),
		ranked AS (
			SELECT sample.id,sample.device_id,sample.component_key,sample.recorded_at,sample.power_w,sample.imported_energy_total_wh,sample.exported_energy_total_wh,sample.voltage_v,sample.current_a,sample.power_factor,sample.frequency_hz,sample.reading_quality,sample.created_at,
				row_number() OVER (PARTITION BY sample.device_id,sample.component_key ORDER BY julianday(sample.recorded_at) DESC,sample.recorded_at DESC,sample.id DESC) AS source_rank
			FROM energy_samples sample
			JOIN requested requested_source ON requested_source.device_id=sample.device_id AND requested_source.component_key=sample.component_key
		)
		SELECT id,device_id,component_key,recorded_at,power_w,imported_energy_total_wh,exported_energy_total_wh,voltage_v,current_a,power_factor,frequency_hz,reading_quality,created_at
		FROM ranked WHERE source_rank=1`
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make(map[EnergySourceRef]EnergySample, len(unique))
	for rows.Next() {
		sample, err := scanEnergySample(rows)
		if err != nil {
			return nil, err
		}
		result[EnergySourceRef{DeviceID: sample.DeviceID, ComponentKey: sample.ComponentKey}] = sample
	}
	return result, rows.Err()
}

func normalizeEnergySourceRefs(sources []EnergySourceRef) ([]EnergySourceRef, error) {
	if len(sources) > maximumBatchedEnergySources {
		return nil, fmt.Errorf("energy source batch exceeds %d sources", maximumBatchedEnergySources)
	}
	seen := make(map[EnergySourceRef]struct{}, len(sources))
	result := make([]EnergySourceRef, 0, len(sources))
	for _, source := range sources {
		source.ComponentKey = strings.TrimSpace(source.ComponentKey)
		if source.DeviceID < 1 || source.ComponentKey == "" || len(source.ComponentKey) > 64 {
			return nil, errors.New("energy source reference is invalid")
		}
		if _, exists := seen[source]; exists {
			continue
		}
		seen[source] = struct{}{}
		result = append(result, source)
	}
	return result, nil
}

func energySourceValues(sources []EnergySourceRef) (string, []any) {
	placeholders := make([]string, 0, len(sources))
	args := make([]any, 0, len(sources)*2)
	for _, source := range sources {
		placeholders = append(placeholders, `(?,?)`)
		args = append(args, source.DeviceID, source.ComponentKey)
	}
	return strings.Join(placeholders, ","), args
}
