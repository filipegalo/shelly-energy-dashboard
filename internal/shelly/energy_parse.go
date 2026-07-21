package shelly

import (
	"strings"
	"time"
)

func parseEnergyObject(kind string, object map[string]any, fallback time.Time) EnergyReading {
	if energyObjectInvalid(object) {
		return EnergyReading{Timestamp: energyTimestamp(object, fallback)}
	}
	reading := EnergyReading{
		Timestamp:        energyTimestamp(object, fallback),
		PowerW:           firstNumeric(object, []string{"total_act_power"}, []string{"total_apower"}, []string{"act_power"}, []string{"apower"}, []string{"power"}),
		ImportedEnergyWh: firstNumeric(object, []string{"total_act"}, []string{"total_act_energy"}, []string{"total_energy"}, []string{"aenergy", "total"}),
		ExportedEnergyWh: firstNumeric(object, []string{"total_act_ret"}, []string{"total_act_ret_energy"}, []string{"total_returned"}, []string{"ret_aenergy", "total"}),
		VoltageV:         firstNumeric(object, []string{"voltage"}),
		CurrentA:         firstNumeric(object, []string{"current"}, []string{"total_current"}),
		PowerFactor:      firstNumeric(object, []string{"pf"}, []string{"power_factor"}),
		FrequencyHz:      firstNumeric(object, []string{"freq"}, []string{"frequency"}),
	}
	if kind == "meter" && reading.ImportedEnergyWh == nil {
		// Gen1 relay meters expose total in watt-minutes, unlike EM-family
		// total counters, which are already expressed in Wh.
		if wattMinutes := firstNumeric(object, []string{"total"}); wattMinutes != nil && *wattMinutes >= 0 {
			converted := *wattMinutes / 60
			reading.ImportedEnergyWh = &converted
		}
	}
	if kind == "emeter" && reading.ImportedEnergyWh == nil {
		reading.ImportedEnergyWh = firstNumeric(object, []string{"total"})
	}
	for _, phase := range []struct{ label, prefix string }{{"A", "a"}, {"B", "b"}, {"C", "c"}} {
		if nonEmptyStringArray(object[phase.prefix+"_errors"]) {
			continue
		}
		phaseReading := PhaseReading{
			Phase:            phase.label,
			PowerW:           firstNumeric(object, []string{phase.prefix + "_act_power"}, []string{phase.prefix + "_apower"}),
			ImportedEnergyWh: firstNumeric(object, []string{phase.prefix + "_total_act_energy"}),
			ExportedEnergyWh: firstNumeric(object, []string{phase.prefix + "_total_act_ret_energy"}),
			VoltageV:         firstNumeric(object, []string{phase.prefix + "_voltage"}),
			CurrentA:         firstNumeric(object, []string{phase.prefix + "_current"}),
			PowerFactor:      firstNumeric(object, []string{phase.prefix + "_pf"}),
			FrequencyHz:      firstNumeric(object, []string{phase.prefix + "_freq"}),
		}
		phaseReading.PowerW = sensible(phaseReading.PowerW, -1e9, 1e9)
		phaseReading.ImportedEnergyWh = sensible(phaseReading.ImportedEnergyWh, 0, 1e18)
		phaseReading.ExportedEnergyWh = sensible(phaseReading.ExportedEnergyWh, 0, 1e18)
		phaseReading.VoltageV = sensible(phaseReading.VoltageV, 0, 1e6)
		phaseReading.CurrentA = sensible(phaseReading.CurrentA, 0, 1e6)
		phaseReading.PowerFactor = sensible(phaseReading.PowerFactor, -1, 1)
		phaseReading.FrequencyHz = sensible(phaseReading.FrequencyHz, 0, 1e4)
		if phaseReading.PowerW != nil || phaseReading.ImportedEnergyWh != nil || phaseReading.VoltageV != nil || phaseReading.CurrentA != nil {
			reading.PhaseReadings = append(reading.PhaseReadings, phaseReading)
		}
	}
	reading = fillPhaseTotals(reading)
	supportsExport := reading.ExportedEnergyWh != nil || kind == "emeter" || kind == "em" || kind == "emdata" || kind == "em1" || kind == "em1data"
	minimumPower := 0.0
	if supportsExport {
		minimumPower = -1e9
	}
	reading.PowerW = sensible(reading.PowerW, minimumPower, 1e9)
	reading.ImportedEnergyWh = sensible(reading.ImportedEnergyWh, 0, 1e18)
	reading.ExportedEnergyWh = sensible(reading.ExportedEnergyWh, 0, 1e18)
	reading.VoltageV = sensible(reading.VoltageV, 0, 1e6)
	reading.CurrentA = sensible(reading.CurrentA, 0, 1e6)
	reading.PowerFactor = sensible(reading.PowerFactor, -1, 1)
	reading.FrequencyHz = sensible(reading.FrequencyHz, 0, 1e4)
	return reading
}

func energyObjectInvalid(object map[string]any) bool {
	if valid, present := object["is_valid"].(bool); present && !valid {
		return true
	}
	for _, value := range stringArray(object["errors"]) {
		switch strings.ToLower(value) {
		case "power_meter_failure", "database_error", "ct_type_not_set":
			return true
		}
	}
	return false
}

func nonEmptyStringArray(value any) bool { return len(stringArray(value)) > 0 }

func stringArray(value any) []string {
	items, ok := value.([]any)
	if !ok {
		return nil
	}
	result := make([]string, 0, len(items))
	for _, item := range items {
		if text, ok := item.(string); ok {
			result = append(result, text)
		}
	}
	return result
}

func fillPhaseTotals(reading EnergyReading) EnergyReading {
	if len(reading.PhaseReadings) == 0 {
		return reading
	}
	if reading.PowerW == nil {
		reading.PowerW = sumCompletePhaseValues(reading.PhaseReadings, func(phase PhaseReading) *float64 { return phase.PowerW })
	}
	if reading.ImportedEnergyWh == nil {
		reading.ImportedEnergyWh = sumCompletePhaseValues(reading.PhaseReadings, func(phase PhaseReading) *float64 { return phase.ImportedEnergyWh })
	}
	if reading.ExportedEnergyWh == nil {
		reading.ExportedEnergyWh = sumCompletePhaseValues(reading.PhaseReadings, func(phase PhaseReading) *float64 { return phase.ExportedEnergyWh })
	}
	return reading
}

func sumCompletePhaseValues(phases []PhaseReading, selectValue func(PhaseReading) *float64) *float64 {
	var total float64
	for _, phase := range phases {
		value := selectValue(phase)
		if value == nil {
			return nil
		}
		total += *value
	}
	return &total
}

func sensible(value *float64, minimum, maximum float64) *float64 {
	if value == nil || *value < minimum || *value > maximum {
		return nil
	}
	return value
}

func energyTimestamp(object map[string]any, fallback time.Time) time.Time {
	for _, key := range []string{"timestamp", "ts"} {
		if raw := numeric(object[key]); raw != nil && *raw > 0 {
			seconds := int64(*raw)
			candidate := time.Unix(seconds, 0).UTC()
			if candidate.Year() >= 2000 && candidate.Year() <= 2200 {
				return candidate
			}
		}
	}
	return fallback
}

func mergeEnergyReadings(primary, extra EnergyReading) EnergyReading {
	result := primary
	if result.PowerW == nil {
		result.PowerW = extra.PowerW
	}
	if result.ImportedEnergyWh == nil {
		result.ImportedEnergyWh = extra.ImportedEnergyWh
	}
	if result.ExportedEnergyWh == nil {
		result.ExportedEnergyWh = extra.ExportedEnergyWh
	}
	if result.VoltageV == nil {
		result.VoltageV = extra.VoltageV
	}
	if result.CurrentA == nil {
		result.CurrentA = extra.CurrentA
	}
	if result.PowerFactor == nil {
		result.PowerFactor = extra.PowerFactor
	}
	if result.FrequencyHz == nil {
		result.FrequencyHz = extra.FrequencyHz
	}
	if len(result.PhaseReadings) == 0 && len(extra.PhaseReadings) > 0 {
		result.PhaseReadings = append([]PhaseReading(nil), extra.PhaseReadings...)
	} else if len(extra.PhaseReadings) > 0 {
		result.PhaseReadings = mergePhases(result.PhaseReadings, extra.PhaseReadings)
	}
	return result
}

func mergePhases(primary, extra []PhaseReading) []PhaseReading {
	result := append([]PhaseReading(nil), primary...)
	for _, addition := range extra {
		found := false
		for index, existing := range result {
			if existing.Phase != addition.Phase {
				continue
			}
			merged := existing
			if merged.PowerW == nil {
				merged.PowerW = addition.PowerW
			}
			if merged.ImportedEnergyWh == nil {
				merged.ImportedEnergyWh = addition.ImportedEnergyWh
			}
			if merged.ExportedEnergyWh == nil {
				merged.ExportedEnergyWh = addition.ExportedEnergyWh
			}
			if merged.VoltageV == nil {
				merged.VoltageV = addition.VoltageV
			}
			if merged.CurrentA == nil {
				merged.CurrentA = addition.CurrentA
			}
			if merged.PowerFactor == nil {
				merged.PowerFactor = addition.PowerFactor
			}
			if merged.FrequencyHz == nil {
				merged.FrequencyHz = addition.FrequencyHz
			}
			result[index], found = merged, true
			break
		}
		if !found {
			result = append(result, addition)
		}
	}
	return result
}

func aggregatePhaseReadings(readings []EnergyReading, timestamp time.Time) EnergyReading {
	result := EnergyReading{Timestamp: timestamp}
	var powers, imports, exports float64
	var powerCount, importCount, exportCount int
	for index, reading := range readings {
		phase := PhaseReading{Phase: phaseName(index), PowerW: reading.PowerW, ImportedEnergyWh: reading.ImportedEnergyWh, ExportedEnergyWh: reading.ExportedEnergyWh, VoltageV: reading.VoltageV, CurrentA: reading.CurrentA, PowerFactor: reading.PowerFactor, FrequencyHz: reading.FrequencyHz}
		result.PhaseReadings = append(result.PhaseReadings, phase)
		if reading.PowerW != nil {
			powers += *reading.PowerW
			powerCount++
		}
		if reading.ImportedEnergyWh != nil {
			imports += *reading.ImportedEnergyWh
			importCount++
		}
		if reading.ExportedEnergyWh != nil {
			exports += *reading.ExportedEnergyWh
			exportCount++
		}
	}
	if powerCount == len(readings) {
		result.PowerW = &powers
	}
	if importCount == len(readings) {
		result.ImportedEnergyWh = &imports
	}
	if exportCount == len(readings) {
		result.ExportedEnergyWh = &exports
	}
	return result
}

func phaseName(index int) string {
	if index >= 0 && index < 3 {
		return string(rune('A' + index))
	}
	return "Phase " + string(rune('1'+index))
}
