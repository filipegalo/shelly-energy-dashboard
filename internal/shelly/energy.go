package shelly

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/netip"
	"sort"
	"strconv"
	"strings"
	"time"
)

var (
	ErrInvalidEnergyComponent  = errors.New("invalid Shelly energy component")
	ErrEnergyComponentNotFound = errors.New("Shelly energy component not found")
	ErrNoUsableEnergyData      = errors.New("Shelly component returned no usable energy data")
)

type EnergyReading struct {
	Timestamp        time.Time      `json:"timestamp"`
	PowerW           *float64       `json:"power_w,omitempty"`
	ImportedEnergyWh *float64       `json:"imported_energy_wh,omitempty"`
	ExportedEnergyWh *float64       `json:"exported_energy_wh,omitempty"`
	VoltageV         *float64       `json:"voltage_v,omitempty"`
	CurrentA         *float64       `json:"current_a,omitempty"`
	PowerFactor      *float64       `json:"power_factor,omitempty"`
	FrequencyHz      *float64       `json:"frequency_hz,omitempty"`
	PhaseReadings    []PhaseReading `json:"phase_readings,omitempty"`
}

type PhaseReading struct {
	Phase            string   `json:"phase"`
	PowerW           *float64 `json:"power_w,omitempty"`
	ImportedEnergyWh *float64 `json:"imported_energy_wh,omitempty"`
	ExportedEnergyWh *float64 `json:"exported_energy_wh,omitempty"`
	VoltageV         *float64 `json:"voltage_v,omitempty"`
	CurrentA         *float64 `json:"current_a,omitempty"`
	PowerFactor      *float64 `json:"power_factor,omitempty"`
	FrequencyHz      *float64 `json:"frequency_hz,omitempty"`
}

type EnergyCapabilities struct {
	PowerW           bool
	ImportedEnergyWh bool
	ExportedEnergyWh bool
	VoltageV         bool
	CurrentA         bool
	PowerFactor      bool
	FrequencyHz      bool
	PhaseReadings    bool
}

func (c EnergyCapabilities) Usable() bool { return c.PowerW || c.ImportedEnergyWh }

type EnergyComponent struct {
	Key          string
	Type         string
	Label        string
	Capabilities EnergyCapabilities
}

// DiscoverEnergyComponents reads only Shelly's fixed status endpoints. Returned
// keys are safe to persist and later pass to ReadEnergy.
func (c *Client) DiscoverEnergyComponents(ctx context.Context, address netip.Addr, credential *Credential) ([]EnergyComponent, error) {
	if !address.Is4() {
		return nil, errors.New("Shelly energy discovery requires a literal IPv4 address")
	}
	httpClient := c.httpClient(address)
	info, err := c.get(ctx, httpClient, address, "/shelly", nil)
	if err != nil {
		return nil, err
	}
	if !resemblesShelly(info) {
		return nil, ErrNotShelly
	}
	path := "/status"
	if detectGeneration(info) > 1 {
		path = "/rpc/Shelly.GetStatus"
	}
	raw, err := c.get(ctx, httpClient, address, path, credential)
	if err != nil {
		return nil, err
	}
	status, err := decodeEnergyObject(raw)
	if err != nil {
		return nil, err
	}
	if path == "/status" {
		return discoverGen1Energy(status), nil
	}
	return discoverRPCComponents(status), nil
}

// ReadEnergy maps a validated component key to a fixed, allowlisted endpoint.
// Browser input can therefore never choose an RPC method or URL.
func (c *Client) ReadEnergy(ctx context.Context, address netip.Addr, componentKey string, credential *Credential) (EnergyReading, error) {
	if !address.Is4() {
		return EnergyReading{}, errors.New("Shelly energy read requires a literal IPv4 address")
	}
	kind, id, total, err := parseEnergyComponentKey(componentKey)
	if err != nil {
		return EnergyReading{}, err
	}
	httpClient := c.httpClient(address)
	now := time.Now().UTC()
	if kind == "meter" || kind == "emeter" {
		raw, getErr := c.get(ctx, httpClient, address, "/status", credential)
		if getErr != nil {
			return EnergyReading{}, getErr
		}
		status, decodeErr := decodeEnergyObject(raw)
		if decodeErr != nil {
			return EnergyReading{}, decodeErr
		}
		return readGen1Energy(status, kind, id, total, now)
	}

	method, companion := rpcEnergyMethods(kind)
	if method == "" {
		return EnergyReading{}, ErrInvalidEnergyComponent
	}
	path := "/rpc/" + method + "?id=" + strconv.Itoa(id)
	raw, err := c.get(ctx, httpClient, address, path, credential)
	if err != nil {
		return EnergyReading{}, err
	}
	object, err := decodeEnergyObject(raw)
	if err != nil {
		return EnergyReading{}, err
	}
	reading := parseEnergyObject(kind, object, now)
	if companion != "" {
		companionPath := "/rpc/" + companion + "?id=" + strconv.Itoa(id)
		if extraRaw, extraErr := c.get(ctx, httpClient, address, companionPath, credential); extraErr == nil {
			if extra, decodeErr := decodeEnergyObject(extraRaw); decodeErr == nil {
				reading = mergeEnergyReadings(reading, parseEnergyObject(kind+"data", extra, now))
			}
		}
	}
	if !capabilitiesFor(reading).Usable() {
		return EnergyReading{}, ErrNoUsableEnergyData
	}
	return reading, nil
}

func rpcEnergyMethods(kind string) (string, string) {
	switch kind {
	case "switch":
		return "Switch.GetStatus", ""
	case "em":
		return "EM.GetStatus", "EMData.GetStatus"
	case "emdata":
		return "EMData.GetStatus", ""
	case "em1":
		return "EM1.GetStatus", "EM1Data.GetStatus"
	case "em1data":
		return "EM1Data.GetStatus", ""
	case "pm":
		return "PM.GetStatus", "PMData.GetStatus"
	case "pmdata":
		return "PMData.GetStatus", ""
	case "pm1":
		return "PM1.GetStatus", "PM1Data.GetStatus"
	case "pm1data":
		return "PM1Data.GetStatus", ""
	default:
		return "", ""
	}
}

func parseEnergyComponentKey(key string) (string, int, bool, error) {
	parts := strings.Split(key, ":")
	if len(parts) != 2 {
		return "", 0, false, ErrInvalidEnergyComponent
	}
	allowed := map[string]bool{"meter": true, "emeter": true, "switch": true, "em": true, "emdata": true, "em1": true, "em1data": true, "pm": true, "pmdata": true, "pm1": true, "pm1data": true}
	if !allowed[parts[0]] {
		return "", 0, false, ErrInvalidEnergyComponent
	}
	if parts[1] == "total" {
		if parts[0] != "emeter" {
			return "", 0, false, ErrInvalidEnergyComponent
		}
		return parts[0], 0, true, nil
	}
	id, err := strconv.Atoi(parts[1])
	if err != nil || id < 0 || id > 128 || strconv.Itoa(id) != parts[1] {
		return "", 0, false, ErrInvalidEnergyComponent
	}
	return parts[0], id, false, nil
}

// ParseEnergyComponentKey validates a persisted component identity against the
// same fixed allowlist used by ReadEnergy. Callers may store the returned kind,
// but must never turn it into an arbitrary RPC method or URL.
func ParseEnergyComponentKey(key string) (kind string, id int, total bool, err error) {
	return parseEnergyComponentKey(key)
}

func discoverGen1Energy(status map[string]any) []EnergyComponent {
	result := []EnergyComponent{}
	for _, family := range []string{"meters", "emeters"} {
		items, _ := status[family].([]any)
		kind := strings.TrimSuffix(family, "s")
		readings := make([]EnergyReading, 0, len(items))
		for index, item := range items {
			object, ok := item.(map[string]any)
			if !ok {
				continue
			}
			reading := parseEnergyObject(kind, object, time.Time{})
			readings = append(readings, reading)
			capability := capabilitiesFor(reading)
			if capability.ImportedEnergyWh {
				result = append(result, EnergyComponent{Key: fmt.Sprintf("%s:%d", kind, index), Type: kind, Label: fmt.Sprintf("%s %d", titleEnergyKind(kind), index), Capabilities: capability})
			}
		}
		if kind == "emeter" && len(readings) > 1 {
			total := aggregatePhaseReadings(readings, time.Time{})
			label := "Total meter"
			if len(readings) >= 3 {
				label = "Total three-phase meter"
			}
			capability := capabilitiesFor(total)
			if capability.Usable() {
				result = append(result, EnergyComponent{Key: "emeter:total", Type: kind, Label: label, Capabilities: capability})
			}
		}
	}
	return result
}

func discoverRPCComponents(status map[string]any) []EnergyComponent {
	keys := make([]string, 0, len(status))
	for key := range status {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	result := []EnergyComponent{}
	for _, key := range keys {
		kind, id, _, err := parseEnergyComponentKey(key)
		if err != nil || kind == "meter" || kind == "emeter" {
			continue
		}
		if strings.HasSuffix(kind, "data") {
			base := strings.TrimSuffix(kind, "data")
			if _, paired := status[fmt.Sprintf("%s:%d", base, id)]; paired {
				continue
			}
		}
		object, ok := status[key].(map[string]any)
		if !ok {
			continue
		}
		reading := parseEnergyObject(kind, object, time.Time{})
		if !strings.HasSuffix(kind, "data") {
			if companion, ok := status[fmt.Sprintf("%sdata:%d", kind, id)].(map[string]any); ok {
				reading = mergeEnergyReadings(reading, parseEnergyObject(kind+"data", companion, time.Time{}))
			}
		}
		capability := capabilitiesFor(reading)
		if capability.Usable() {
			result = append(result, EnergyComponent{Key: key, Type: kind, Label: fmt.Sprintf("%s %d", titleEnergyKind(kind), id), Capabilities: capability})
		}
	}
	return result
}

func readGen1Energy(status map[string]any, kind string, id int, total bool, timestamp time.Time) (EnergyReading, error) {
	items, _ := status[kind+"s"].([]any)
	if total {
		readings := make([]EnergyReading, 0, len(items))
		for _, item := range items {
			if object, ok := item.(map[string]any); ok {
				readings = append(readings, parseEnergyObject(kind, object, timestamp))
			}
		}
		if len(readings) == 0 {
			return EnergyReading{}, ErrEnergyComponentNotFound
		}
		reading := aggregatePhaseReadings(readings, timestamp)
		if !capabilitiesFor(reading).Usable() {
			return EnergyReading{}, ErrNoUsableEnergyData
		}
		return reading, nil
	}
	if id >= len(items) {
		return EnergyReading{}, ErrEnergyComponentNotFound
	}
	object, ok := items[id].(map[string]any)
	if !ok {
		return EnergyReading{}, ErrEnergyComponentNotFound
	}
	reading := parseEnergyObject(kind, object, timestamp)
	if !capabilitiesFor(reading).Usable() {
		return EnergyReading{}, ErrNoUsableEnergyData
	}
	return reading, nil
}

func titleEnergyKind(kind string) string {
	switch kind {
	case "switch":
		return "Switch"
	case "meter":
		return "Meter"
	case "emeter", "em", "em1":
		return "Energy meter"
	case "emdata", "em1data":
		return "Energy data"
	case "pm", "pm1":
		return "Power meter"
	default:
		return "Power data"
	}
}

func decodeEnergyObject(raw []byte) (map[string]any, error) {
	var object map[string]any
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.UseNumber()
	if err := decoder.Decode(&object); err != nil || object == nil {
		return nil, errors.New("device returned invalid energy JSON")
	}
	return object, nil
}

func capabilitiesFor(r EnergyReading) EnergyCapabilities {
	return EnergyCapabilities{PowerW: r.PowerW != nil, ImportedEnergyWh: r.ImportedEnergyWh != nil, ExportedEnergyWh: r.ExportedEnergyWh != nil, VoltageV: r.VoltageV != nil, CurrentA: r.CurrentA != nil, PowerFactor: r.PowerFactor != nil, FrequencyHz: r.FrequencyHz != nil, PhaseReadings: len(r.PhaseReadings) > 0}
}

func numeric(value any) *float64 {
	var number float64
	var err error
	switch typed := value.(type) {
	case json.Number:
		number, err = typed.Float64()
	case float64:
		number = typed
	default:
		return nil
	}
	if err != nil || math.IsNaN(number) || math.IsInf(number, 0) {
		return nil
	}
	return &number
}

func valueAt(object map[string]any, path ...string) any {
	var current any = object
	for _, key := range path {
		next, ok := current.(map[string]any)
		if !ok {
			return nil
		}
		current = next[key]
	}
	return current
}

func firstNumeric(object map[string]any, paths ...[]string) *float64 {
	for _, path := range paths {
		if result := numeric(valueAt(object, path...)); result != nil {
			return result
		}
	}
	return nil
}
