package shelly

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/netip"
	"testing"
	"time"
)

func TestDiscoverEnergyComponentsGen1(t *testing.T) {
	t.Parallel()
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/shelly":
			_, _ = io.WriteString(w, `{"type":"SHEM-3","mac":"AABBCCDDEEFF","fw":"v1.14.0"}`)
		case "/status":
			_, _ = io.WriteString(w, `{"emeters":[{"power":100,"total":1200,"voltage":230,"current":0.45,"pf":0.96},{"power":200,"total":2400,"voltage":231,"current":0.90,"pf":0.97},{"power":300,"total":3600,"voltage":229,"current":1.35,"pf":0.98}]}`)
		default:
			http.NotFound(w, r)
		}
	})
	client := NewClient(time.Second, time.Second)
	client.dialContext = pipeDial(handler)

	components, err := client.DiscoverEnergyComponents(context.Background(), netip.MustParseAddr("192.168.1.20"), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(components) != 4 {
		t.Fatalf("components = %+v", components)
	}
	total := components[3]
	if total.Key != "emeter:total" || total.Type != "emeter" || total.Label != "Total three-phase meter" {
		t.Fatalf("total component = %+v", total)
	}
	if !total.Capabilities.PowerW || !total.Capabilities.ImportedEnergyWh || !total.Capabilities.PhaseReadings {
		t.Fatalf("total capabilities = %+v", total.Capabilities)
	}
}

func TestDiscoverEnergyComponentsGen2MergesDataCompanion(t *testing.T) {
	t.Parallel()
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/shelly":
			_, _ = io.WriteString(w, `{"id":"shellypro3em-a","model":"SPEM-003CEBEU","gen":2}`)
		case "/rpc/Shelly.GetStatus":
			_, _ = io.WriteString(w, `{"em:0":{"id":0,"a_act_power":100,"b_act_power":200,"c_act_power":300,"total_act_power":600,"a_voltage":230,"b_voltage":231,"c_voltage":229},"emdata:0":{"id":0,"a_total_act_energy":1000,"b_total_act_energy":2000,"c_total_act_energy":3000,"total_act":6000},"switch:0":{"id":0,"apower":14.5,"aenergy":{"total":5432.1},"voltage":230.2}}`)
		default:
			http.NotFound(w, r)
		}
	})
	client := NewClient(time.Second, time.Second)
	client.dialContext = pipeDial(handler)

	components, err := client.DiscoverEnergyComponents(context.Background(), netip.MustParseAddr("192.168.1.21"), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(components) != 2 {
		t.Fatalf("components = %+v", components)
	}
	if components[0].Key != "em:0" || !components[0].Capabilities.ImportedEnergyWh || !components[0].Capabilities.PhaseReadings {
		t.Fatalf("merged EM component = %+v", components[0])
	}
	if components[1].Key != "switch:0" || !components[1].Capabilities.VoltageV {
		t.Fatalf("switch component = %+v", components[1])
	}
}

func TestReadEnergyGen1MeterConvertsWattMinutes(t *testing.T) {
	t.Parallel()
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/status" {
			http.NotFound(w, r)
			return
		}
		_, _ = io.WriteString(w, `{"meters":[{"power":125.5,"total":600}]}`)
	})
	client := NewClient(time.Second, time.Second)
	client.dialContext = pipeDial(handler)

	reading, err := client.ReadEnergy(context.Background(), netip.MustParseAddr("192.168.1.22"), "meter:0", nil)
	if err != nil {
		t.Fatal(err)
	}
	assertFloatPointer(t, reading.PowerW, 125.5)
	assertFloatPointer(t, reading.ImportedEnergyWh, 10)
}

func TestReadEnergyGen2UsesOnlyFixedMethodsAndMergesCompanion(t *testing.T) {
	t.Parallel()
	var paths []string
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.RequestURI())
		switch r.URL.Path {
		case "/rpc/EM.GetStatus":
			_, _ = io.WriteString(w, `{"id":0,"a_act_power":100,"b_act_power":200,"c_act_power":300,"total_act_power":600,"a_current":0.5,"b_current":1,"c_current":1.5}`)
		case "/rpc/EMData.GetStatus":
			_, _ = io.WriteString(w, `{"id":0,"total_act":12345,"total_act_ret":21}`)
		default:
			http.NotFound(w, r)
		}
	})
	client := NewClient(time.Second, time.Second)
	client.dialContext = pipeDial(handler)

	reading, err := client.ReadEnergy(context.Background(), netip.MustParseAddr("192.168.1.23"), "em:0", nil)
	if err != nil {
		t.Fatal(err)
	}
	assertFloatPointer(t, reading.PowerW, 600)
	assertFloatPointer(t, reading.ImportedEnergyWh, 12345)
	assertFloatPointer(t, reading.ExportedEnergyWh, 21)
	if len(reading.PhaseReadings) != 3 || len(paths) != 2 || paths[0] != "/rpc/EM.GetStatus?id=0" || paths[1] != "/rpc/EMData.GetStatus?id=0" {
		t.Fatalf("reading=%+v paths=%v", reading, paths)
	}
}

func TestReadEnergyRejectsUntrustedComponentKeyBeforeRequest(t *testing.T) {
	t.Parallel()
	requested := false
	client := NewClient(time.Second, time.Second)
	client.dialContext = pipeDial(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { requested = true }))

	_, err := client.ReadEnergy(context.Background(), netip.MustParseAddr("192.168.1.24"), "Shelly.Reboot:0?delay_ms=0", nil)
	if !errors.Is(err, ErrInvalidEnergyComponent) {
		t.Fatalf("error = %v", err)
	}
	if requested {
		t.Fatal("invalid component caused a network request")
	}
}

func TestParseEnergyObjectRejectsNonNumericAndNonFiniteValues(t *testing.T) {
	t.Parallel()
	object, err := decodeEnergyObject([]byte(`{"apower":"100","aenergy":{"total":1e999},"voltage":null}`))
	if err != nil {
		t.Fatal(err)
	}
	reading := parseEnergyObject("switch", object, time.Unix(100, 0).UTC())
	if reading.PowerW != nil || reading.ImportedEnergyWh != nil || reading.VoltageV != nil {
		t.Fatalf("reading = %+v", reading)
	}
}

func TestParseEnergyObjectRejectsExplicitlyInvalidMeterData(t *testing.T) {
	t.Parallel()
	for _, raw := range []string{
		`{"power":100,"total":600,"is_valid":false}`,
		`{"apower":100,"aenergy":{"total":10},"errors":["power_meter_failure"]}`,
	} {
		object, err := decodeEnergyObject([]byte(raw))
		if err != nil {
			t.Fatal(err)
		}
		reading := parseEnergyObject("meter", object, time.Unix(100, 0).UTC())
		if capabilitiesFor(reading).Usable() {
			t.Fatalf("invalid object produced reading: %+v", reading)
		}
	}
}

func assertFloatPointer(t *testing.T, got *float64, want float64) {
	t.Helper()
	if got == nil || *got != want {
		t.Fatalf("value = %v, want %v", got, want)
	}
}
