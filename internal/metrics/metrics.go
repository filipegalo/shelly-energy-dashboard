package metrics

import "github.com/prometheus/client_golang/prometheus"

type Metrics struct {
	Registry                              *prometheus.Registry
	ScanDuration                          *prometheus.HistogramVec
	ScanCount                             *prometheus.CounterVec
	AddressesScanned                      prometheus.Counter
	DevicesDiscovered                     prometheus.Counter
	NewDevices                            prometheus.Counter
	APIRequestDuration                    *prometheus.HistogramVec
	APIFailures                           *prometheus.CounterVec
	RunningScans                          prometheus.Gauge
	EnergyCollectorRequests               prometheus.Counter
	EnergyCollectorErrors                 *prometheus.CounterVec
	EnergyCollectorDuration               prometheus.Histogram
	EnergyLastReading                     prometheus.Gauge
	EnergyCurrentPower                    prometheus.Gauge
	EnergyConsumptionKWh                  prometheus.Counter
	EnergyCounterResets                   prometheus.Counter
	EnergyMissingSamples                  prometheus.Counter
	EnergyEstimatedCost                   prometheus.Gauge
	RoomEnergySourcesTotal                prometheus.Gauge
	RoomEnergySourcesOnline               prometheus.Gauge
	RoomEnergySourcesStale                prometheus.Gauge
	RoomEnergyConsumptionKWh              prometheus.Gauge
	RoomEnergyUnmeteredKWh                prometheus.Gauge
	RoomEnergyCoverageRatio               prometheus.Gauge
	RoomEnergyReconciliationDifferenceKWh prometheus.Gauge
	RoomEnergyReconciliationWarnings      prometheus.Counter
	RoomEnergyAllocationErrors            prometheus.Counter
	RoomEnergyAggregationDuration         prometheus.Histogram
}

func New() *Metrics {
	registry := prometheus.NewRegistry()
	value := &Metrics{
		Registry:                              registry,
		ScanDuration:                          prometheus.NewHistogramVec(prometheus.HistogramOpts{Name: "shelly_inventory_scan_duration_seconds", Help: "Duration of discovery scans."}, []string{"status"}),
		ScanCount:                             prometheus.NewCounterVec(prometheus.CounterOpts{Name: "shelly_inventory_scans_total", Help: "Discovery scans by terminal status."}, []string{"status"}),
		AddressesScanned:                      prometheus.NewCounter(prometheus.CounterOpts{Name: "shelly_inventory_addresses_scanned_total", Help: "Addresses checked by discovery."}),
		DevicesDiscovered:                     prometheus.NewCounter(prometheus.CounterOpts{Name: "shelly_inventory_devices_discovered_total", Help: "Shelly devices discovered."}),
		NewDevices:                            prometheus.NewCounter(prometheus.CounterOpts{Name: "shelly_inventory_new_devices_total", Help: "New Shelly devices discovered."}),
		APIRequestDuration:                    prometheus.NewHistogramVec(prometheus.HistogramOpts{Name: "shelly_inventory_api_request_duration_seconds", Help: "Duration of Shelly API probes."}, []string{"result"}),
		APIFailures:                           prometheus.NewCounterVec(prometheus.CounterOpts{Name: "shelly_inventory_api_failures_total", Help: "Shelly API failures by bounded type."}, []string{"type"}),
		RunningScans:                          prometheus.NewGauge(prometheus.GaugeOpts{Name: "shelly_inventory_running_scans", Help: "Currently running discovery scans."}),
		EnergyCollectorRequests:               prometheus.NewCounter(prometheus.CounterOpts{Name: "shelly_energy_collector_requests_total", Help: "Energy meter collection requests."}),
		EnergyCollectorErrors:                 prometheus.NewCounterVec(prometheus.CounterOpts{Name: "shelly_energy_collector_errors_total", Help: "Energy meter collection errors by bounded type."}, []string{"type"}),
		EnergyCollectorDuration:               prometheus.NewHistogram(prometheus.HistogramOpts{Name: "shelly_energy_collector_request_duration_seconds", Help: "Duration of energy meter collection requests."}),
		EnergyLastReading:                     prometheus.NewGauge(prometheus.GaugeOpts{Name: "shelly_energy_last_reading_timestamp_seconds", Help: "Unix timestamp of the last successful energy reading."}),
		EnergyCurrentPower:                    prometheus.NewGauge(prometheus.GaugeOpts{Name: "shelly_energy_current_power_watts", Help: "Latest whole-house active power in watts."}),
		EnergyConsumptionKWh:                  prometheus.NewCounter(prometheus.CounterOpts{Name: "shelly_energy_consumption_kwh_total", Help: "Whole-house consumption recorded by this process in kWh."}),
		EnergyCounterResets:                   prometheus.NewCounter(prometheus.CounterOpts{Name: "shelly_energy_counter_resets_total", Help: "Detected energy counter resets."}),
		EnergyMissingSamples:                  prometheus.NewCounter(prometheus.CounterOpts{Name: "shelly_energy_missing_samples_total", Help: "Expected energy samples missed due to collection failures."}),
		EnergyEstimatedCost:                   prometheus.NewGauge(prometheus.GaugeOpts{Name: "shelly_energy_estimated_cost_eur", Help: "Estimated variable energy cost for the current billing cycle in EUR."}),
		RoomEnergySourcesTotal:                prometheus.NewGauge(prometheus.GaugeOpts{Name: "shelly_room_energy_sources_total", Help: "Configured room energy sources currently selected for collection."}),
		RoomEnergySourcesOnline:               prometheus.NewGauge(prometheus.GaugeOpts{Name: "shelly_room_energy_sources_online", Help: "Configured room energy sources with a current online reading."}),
		RoomEnergySourcesStale:                prometheus.NewGauge(prometheus.GaugeOpts{Name: "shelly_room_energy_sources_stale", Help: "Configured room energy sources whose latest reading is stale."}),
		RoomEnergyConsumptionKWh:              prometheus.NewGauge(prometheus.GaugeOpts{Name: "shelly_room_energy_consumption_kwh", Help: "Measured room energy in the latest completed room report."}),
		RoomEnergyUnmeteredKWh:                prometheus.NewGauge(prometheus.GaugeOpts{Name: "shelly_room_energy_unmetered_kwh", Help: "Unmetered energy in the latest completed room report."}),
		RoomEnergyCoverageRatio:               prometheus.NewGauge(prometheus.GaugeOpts{Name: "shelly_room_energy_coverage_ratio", Help: "Room-meter coverage ratio in the latest completed room report."}),
		RoomEnergyReconciliationDifferenceKWh: prometheus.NewGauge(prometheus.GaugeOpts{Name: "shelly_room_energy_reconciliation_difference_kwh", Help: "Signed room-minus-main energy difference in the latest completed room report."}),
		RoomEnergyReconciliationWarnings:      prometheus.NewCounter(prometheus.CounterOpts{Name: "shelly_room_energy_reconciliation_warnings_total", Help: "Reconciliation-warning intervals produced by completed room reports."}),
		RoomEnergyAllocationErrors:            prometheus.NewCounter(prometheus.CounterOpts{Name: "shelly_room_energy_allocation_errors_total", Help: "Room cost-allocation failures."}),
		RoomEnergyAggregationDuration:         prometheus.NewHistogram(prometheus.HistogramOpts{Name: "shelly_room_energy_aggregation_duration_seconds", Help: "Duration of room report aggregation and cost allocation."}),
	}
	registry.MustRegister(value.ScanDuration, value.ScanCount, value.AddressesScanned, value.DevicesDiscovered, value.NewDevices, value.APIRequestDuration, value.APIFailures, value.RunningScans, value.EnergyCollectorRequests, value.EnergyCollectorErrors, value.EnergyCollectorDuration, value.EnergyLastReading, value.EnergyCurrentPower, value.EnergyConsumptionKWh, value.EnergyCounterResets, value.EnergyMissingSamples, value.EnergyEstimatedCost, value.RoomEnergySourcesTotal, value.RoomEnergySourcesOnline, value.RoomEnergySourcesStale, value.RoomEnergyConsumptionKWh, value.RoomEnergyUnmeteredKWh, value.RoomEnergyCoverageRatio, value.RoomEnergyReconciliationDifferenceKWh, value.RoomEnergyReconciliationWarnings, value.RoomEnergyAllocationErrors, value.RoomEnergyAggregationDuration)
	return value
}
