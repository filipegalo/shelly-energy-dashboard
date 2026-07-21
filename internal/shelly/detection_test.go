package shelly

import (
	"encoding/json"
	"testing"
)

func TestDetectAndNormalizeGen1(t *testing.T) {
	t.Parallel()
	info := json.RawMessage(`{"type":"SHSW-1","mac":"AA:BB:CC:DD:EE:FF","auth":false,"fw":"20230913-112003/v1.14.0-gcb84623"}`)
	settings := json.RawMessage(`{"name":"Garage Relay","device":{"hostname":"shelly1-ABC","type":"SHSW-1","mac":"AABBCCDDEEFF"},"hwinfo":{"hw_revision":"prod-2020"}}`)
	status := json.RawMessage(`{"has_update":false,"meters":[{"power":12.3}],"relays":[{"ison":true}]}`)

	got, err := Normalize(info, settings, status)
	if err != nil {
		t.Fatal(err)
	}
	if got.Generation != 1 || got.Model != "SHSW-1" || got.MACAddress != "aabbccddeeff" {
		t.Fatalf("unexpected normalized device: %+v", got)
	}
	if got.Name != "Garage Relay" || got.FirmwareVersion != "1.14.0" {
		t.Fatalf("unexpected name/firmware: %+v", got)
	}
	if len(got.Capabilities) != 2 {
		t.Fatalf("capabilities = %v", got.Capabilities)
	}
}

func TestDetectAndNormalizeRPC(t *testing.T) {
	t.Parallel()
	info := json.RawMessage(`{"id":"shellyplus1-aabbccddeeff","model":"SNSW-001X16EU","gen":2,"mac":"AABBCCDDEEFF","fw_id":"20241011-114449/1.4.4-g6d2a586","ver":"1.4.4","auth_en":true,"app":"Plus1","profile":"switch"}`)
	config := json.RawMessage(`{"device":{"name":"Boiler","mac":"AABBCCDDEEFF","fw_id":"20241011-114449/1.4.4-g6d2a586"},"sys":{"device":{"name":"Boiler"}}}`)
	status := json.RawMessage(`{"sys":{"mac":"AABBCCDDEEFF","uptime":100},"switch:0":{"output":true,"apower":45.2,"voltage":231.5},"input:0":{"state":false}}`)

	got, err := Normalize(info, config, status)
	if err != nil {
		t.Fatal(err)
	}
	if got.Generation != 2 || got.Name != "Boiler" || got.FirmwareVersion != "1.4.4" {
		t.Fatalf("unexpected normalized device: %+v", got)
	}
	if !got.AuthenticationRequired {
		t.Fatal("expected authentication requirement")
	}
	if len(got.Capabilities) < 3 {
		t.Fatalf("capabilities = %v", got.Capabilities)
	}
}

func TestNormalizeRejectsInvalidJSON(t *testing.T) {
	t.Parallel()
	if _, err := Normalize(json.RawMessage(`{"model":`), nil, nil); err == nil {
		t.Fatal("expected malformed JSON error")
	}
}
