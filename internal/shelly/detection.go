package shelly

import (
	"encoding/json"
	"errors"
	"sort"
	"strings"

	"github.com/filipegalo/shelly-inventory/internal/devices"
)

func Normalize(rawInfo, rawConfig, rawStatus json.RawMessage) (DeviceInfo, error) {
	var info map[string]any
	if err := decodeObject(rawInfo, &info); err != nil {
		return DeviceInfo{}, err
	}
	config := map[string]any{}
	status := map[string]any{}
	if len(rawConfig) > 0 {
		if err := decodeObject(rawConfig, &config); err != nil {
			return DeviceInfo{}, err
		}
	}
	if len(rawStatus) > 0 {
		if err := decodeObject(rawStatus, &status); err != nil {
			return DeviceInfo{}, err
		}
	}

	generation := intValue(info["gen"])
	if generation == 0 {
		generation = 1
	}
	mac := firstString(info["mac"], nested(config, "device", "mac"), nested(status, "sys", "mac"))
	if normalized, err := devices.NormalizeMAC(mac); err == nil {
		mac = normalized
	}
	firmware := firstString(info["ver"], info["fw"], info["fw_id"], nested(config, "device", "fw_id"))
	version := firmwareVersion(firmware)
	name := firstString(config["name"], nested(config, "device", "name"), nested(config, "sys", "device", "name"), info["name"])
	model := firstString(info["model"], info["type"], nested(config, "device", "type"))
	hostname := firstString(nested(config, "device", "hostname"), nested(config, "wifi", "sta", "ssid"))
	hardware := firstString(info["hw_revision"], nested(config, "hwinfo", "hw_revision"))

	return DeviceInfo{
		ShellyDeviceID:         firstString(info["id"]),
		Name:                   name,
		Model:                  model,
		Generation:             generation,
		DeviceType:             firstString(info["app"], info["type"], info["profile"]),
		MACAddress:             mac,
		Hostname:               hostname,
		FirmwareVersion:        version,
		FirmwareBuild:          firmware,
		HardwareRevision:       hardware,
		AuthenticationRequired: boolValue(info["auth_en"]) || boolValue(info["auth"]),
		Capabilities:           capabilities(status),
		RawDeviceInfo:          append(json.RawMessage(nil), rawInfo...),
		RawStatus:              append(json.RawMessage(nil), rawStatus...),
	}, nil
}

func decodeObject(raw json.RawMessage, target *map[string]any) error {
	if len(raw) == 0 {
		return errors.New("device response is empty")
	}
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.UseNumber()
	if err := decoder.Decode(target); err != nil {
		return errors.New("device returned invalid JSON")
	}
	return nil
}

func nested(value any, path ...string) any {
	current := value
	for _, key := range path {
		object, ok := current.(map[string]any)
		if !ok {
			return nil
		}
		current = object[key]
	}
	return current
}

func firstString(values ...any) string {
	for _, value := range values {
		if text, ok := value.(string); ok && strings.TrimSpace(text) != "" {
			return strings.TrimSpace(text)
		}
	}
	return ""
}

func intValue(value any) int {
	switch typed := value.(type) {
	case json.Number:
		result, _ := typed.Int64()
		return int(result)
	case float64:
		return int(typed)
	}
	return 0
}

func boolValue(value any) bool {
	result, _ := value.(bool)
	return result
}

func firmwareVersion(raw string) string {
	if slash := strings.LastIndex(raw, "/"); slash >= 0 {
		raw = raw[slash+1:]
	}
	raw = strings.TrimPrefix(raw, "v")
	if dash := strings.Index(raw, "-"); dash > 0 {
		raw = raw[:dash]
	}
	return raw
}

func capabilities(status map[string]any) []string {
	set := map[string]struct{}{}
	for key, value := range status {
		base := strings.SplitN(key, ":", 2)[0]
		switch base {
		case "relays", "switch":
			set["switches"] = struct{}{}
		case "inputs", "input":
			set["inputs"] = struct{}{}
		case "covers", "cover":
			set["covers"] = struct{}{}
		case "lights", "light", "rgb":
			set["lights"] = struct{}{}
		case "meters", "em", "em1", "pm1":
			set["power meters"] = struct{}{}
		case "temperature", "temperature:0":
			set["temperature sensors"] = struct{}{}
		case "humidity":
			set["humidity sensors"] = struct{}{}
		}
		if object, ok := value.(map[string]any); ok {
			if _, ok := object["apower"]; ok {
				set["power meters"] = struct{}{}
			}
			if _, ok := object["voltage"]; ok {
				set["voltage measurements"] = struct{}{}
			}
			if _, ok := object["current"]; ok {
				set["current measurements"] = struct{}{}
			}
		}
	}
	result := make([]string, 0, len(set))
	for capability := range set {
		result = append(result, capability)
	}
	sort.Strings(result)
	return result
}
