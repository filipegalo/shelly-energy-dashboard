package metrics

import "testing"

func TestNewRegistersCollectors(t *testing.T) {
	t.Parallel()
	metrics := New()
	metrics.RunningScans.Inc()
	metrics.AddressesScanned.Inc()
	metrics.DevicesDiscovered.Inc()
	metrics.NewDevices.Inc()
	metrics.ScanCount.WithLabelValues("completed").Inc()
	metrics.ScanDuration.WithLabelValues("completed").Observe(1)
	metrics.APIRequestDuration.WithLabelValues("shelly").Observe(.1)
	metrics.APIFailures.WithLabelValues("timeout").Inc()
	metrics.EnergyCollectorRequests.Inc()
	metrics.EnergyCollectorErrors.WithLabelValues("timeout").Inc()
	metrics.EnergyCollectorDuration.Observe(.2)
	metrics.EnergyLastReading.Set(1)
	metrics.EnergyCurrentPower.Set(250)
	metrics.EnergyConsumptionKWh.Add(.5)
	metrics.EnergyCounterResets.Inc()
	metrics.EnergyMissingSamples.Inc()
	metrics.EnergyEstimatedCost.Set(.1)
	values, err := metrics.Registry.Gather()
	if err != nil {
		t.Fatal(err)
	}
	if len(values) != 17 {
		t.Fatalf("gathered %d metric families", len(values))
	}
}
