package energy

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"net/netip"
	"strings"
	"sync"
	"time"

	"github.com/filipegalo/shelly-inventory/internal/config"
	"github.com/filipegalo/shelly-inventory/internal/credentials"
	"github.com/filipegalo/shelly-inventory/internal/database"
	"github.com/filipegalo/shelly-inventory/internal/energy/billingcycle"
	"github.com/filipegalo/shelly-inventory/internal/metrics"
	"github.com/filipegalo/shelly-inventory/internal/shelly"
)

type LiveState struct {
	Configured          bool                 `json:"configured"`
	Enabled             bool                 `json:"enabled"`
	Online              bool                 `json:"online"`
	Stale               bool                 `json:"stale"`
	ConsecutiveFailures int                  `json:"consecutive_failures"`
	LastError           string               `json:"last_error,omitempty"`
	DeviceID            int64                `json:"device_id,omitempty"`
	ComponentKey        string               `json:"component_key,omitempty"`
	PollIntervalSeconds int                  `json:"poll_interval_seconds,omitempty"`
	Reading             shelly.EnergyReading `json:"reading"`
}

var ErrTooManySubscribers = errors.New("energy event subscriber limit reached")

var errEnergyStorage = errors.New("energy storage failure")

const maxSubscribers = 64

type Manager struct {
	appCtx    context.Context
	store     *database.Store
	cfg       config.Config
	logger    *slog.Logger
	metrics   *metrics.Metrics
	cipher    *credentials.Cipher
	newClient func() energyClient

	lifecycleMu        sync.Mutex
	housekeepingOnce   sync.Once
	housekeepingCancel context.CancelFunc
	housekeepingDone   chan struct{}
	mu                 sync.Mutex
	cancel             context.CancelFunc
	done               chan struct{}
	latest             LiveState
	subscribers        map[chan LiveState]struct{}
}

type energyClient interface {
	Identify(context.Context, netip.Addr) (shelly.DeviceInfo, error)
	DiscoverEnergyComponents(context.Context, netip.Addr, *shelly.Credential) ([]shelly.EnergyComponent, error)
	ReadEnergy(context.Context, netip.Addr, string, *shelly.Credential) (shelly.EnergyReading, error)
}

func NewManager(appCtx context.Context, store *database.Store, cfg config.Config, logger *slog.Logger, metricSet *metrics.Metrics, cipher *credentials.Cipher) *Manager {
	manager := &Manager{appCtx: appCtx, store: store, cfg: cfg, logger: logger, metrics: metricSet, cipher: cipher, subscribers: map[chan LiveState]struct{}{}}
	manager.newClient = func() energyClient { return shelly.NewClient(cfg.DefaultConnectionTimeout, cfg.DefaultRequestTimeout) }
	return manager
}

func (m *Manager) Start(ctx context.Context) error {
	m.startHousekeeping()
	return m.Reconfigure(ctx)
}

func (m *Manager) Reconfigure(ctx context.Context) error {
	m.lifecycleMu.Lock()
	defer m.lifecycleMu.Unlock()
	m.stopCurrent()
	energyConfig, err := m.store.GetEnergyConfig(ctx)
	if err != nil {
		return err
	}
	state := LiveState{Configured: energyConfig.DeviceID != 0 && energyConfig.ComponentKey != "", Enabled: energyConfig.Enabled, DeviceID: energyConfig.DeviceID, ComponentKey: energyConfig.ComponentKey, PollIntervalSeconds: energyConfig.PollIntervalSeconds}
	m.publish(state)
	if !energyConfig.Enabled {
		return nil
	}
	if err := validateCollectorConfig(energyConfig); err != nil {
		return err
	}
	collectorCtx, cancel := context.WithCancel(m.appCtx)
	done := make(chan struct{})
	m.mu.Lock()
	m.cancel, m.done = cancel, done
	m.mu.Unlock()
	go m.run(collectorCtx, done, energyConfig)
	return nil
}

func (m *Manager) Stop() {
	m.lifecycleMu.Lock()
	defer m.lifecycleMu.Unlock()
	m.stopCurrent()
	if m.housekeepingCancel != nil {
		m.housekeepingCancel()
		<-m.housekeepingDone
		m.housekeepingCancel, m.housekeepingDone = nil, nil
	}
}

func (m *Manager) startHousekeeping() {
	m.housekeepingOnce.Do(func() {
		ctx, cancel := context.WithCancel(m.appCtx)
		done := make(chan struct{})
		m.housekeepingCancel, m.housekeepingDone = cancel, done
		go func() {
			defer close(done)
			m.cleanupRetention(ctx, time.Now())
			ticker := time.NewTicker(24 * time.Hour)
			defer ticker.Stop()
			for {
				select {
				case at := <-ticker.C:
					m.cleanupRetention(ctx, at)
				case <-ctx.Done():
					return
				}
			}
		}()
	})
}

func (m *Manager) stopCurrent() {
	m.mu.Lock()
	cancel, done := m.cancel, m.done
	m.cancel, m.done = nil, nil
	m.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	if done != nil {
		<-done
	}
}

func (m *Manager) DiscoverMeter(ctx context.Context, deviceID int64) (database.Device, []shelly.EnergyComponent, error) {
	device, address, credential, err := m.deviceAccess(ctx, deviceID)
	if err != nil {
		return database.Device{}, nil, err
	}
	client := m.newClient()
	identity, err := client.Identify(ctx, address)
	if err != nil {
		return device, nil, err
	}
	if err := validateMeterIdentity(device, identity); err != nil {
		return device, nil, err
	}
	components, err := client.DiscoverEnergyComponents(ctx, address, credential)
	if err != nil {
		return device, nil, err
	}
	usable := make([]shelly.EnergyComponent, 0, len(components))
	for _, component := range components {
		if component.Capabilities.Usable() {
			usable = append(usable, component)
		}
	}
	if len(usable) == 0 {
		return device, nil, shelly.ErrNoUsableEnergyData
	}
	return device, usable, nil
}

func (m *Manager) TestMeter(ctx context.Context, deviceID int64, componentKey string) (database.Device, shelly.EnergyComponent, shelly.EnergyReading, error) {
	device, components, err := m.DiscoverMeter(ctx, deviceID)
	if err != nil {
		return database.Device{}, shelly.EnergyComponent{}, shelly.EnergyReading{}, err
	}
	var selected shelly.EnergyComponent
	for _, component := range components {
		if component.Key == componentKey {
			selected = component
			break
		}
	}
	if selected.Key == "" {
		return device, shelly.EnergyComponent{}, shelly.EnergyReading{}, errors.New("select a valid discovered energy component")
	}
	_, address, credential, err := m.deviceAccess(ctx, deviceID)
	if err != nil {
		return device, selected, shelly.EnergyReading{}, err
	}
	client := m.newClient()
	identity, err := client.Identify(ctx, address)
	if err != nil {
		return device, selected, shelly.EnergyReading{}, err
	}
	if err := validateMeterIdentity(device, identity); err != nil {
		return device, selected, shelly.EnergyReading{}, err
	}
	reading, err := client.ReadEnergy(ctx, address, selected.Key, credential)
	if err != nil {
		return device, selected, shelly.EnergyReading{}, err
	}
	if err := validateEnergyReading(reading, 0, 10*time.Minute); err != nil {
		return device, selected, shelly.EnergyReading{}, err
	}
	return device, selected, reading, nil
}

func (m *Manager) Subscribe() (<-chan LiveState, func(), error) {
	channel := make(chan LiveState, 4)
	m.mu.Lock()
	if len(m.subscribers) >= maxSubscribers {
		m.mu.Unlock()
		return nil, func() {}, ErrTooManySubscribers
	}
	m.subscribers[channel] = struct{}{}
	latest := m.latest
	m.mu.Unlock()
	channel <- latest
	return channel, func() {
		m.mu.Lock()
		delete(m.subscribers, channel)
		m.mu.Unlock()
	}, nil
}

func (m *Manager) Latest() LiveState {
	m.mu.Lock()
	defer m.mu.Unlock()
	state := m.latest
	if state.Online && !state.Reading.Timestamp.IsZero() && time.Since(state.Reading.Timestamp) > 2*time.Duration(state.PollIntervalSeconds)*time.Second {
		state.Stale = true
	}
	return state
}

func (m *Manager) run(ctx context.Context, done chan struct{}, energyConfig database.EnergyConfig) {
	defer close(done)
	defer m.recordEvent(energyConfig, "collection_stopped", nil)
	m.recordEvent(energyConfig, "collection_started", nil)

	state := DeltaState{}
	if previous, err := m.store.LatestEnergySample(ctx, energyConfig.DeviceID, energyConfig.ComponentKey); err == nil {
		state = DeltaState{RecordedAt: previous.RecordedAt, PowerW: copyFloat(previous.PowerW), ImportedTotalWh: copyFloat(previous.ImportedEnergyTotalWh)}
	}
	accumulators := map[string]Accumulator{}
	interval := time.Duration(energyConfig.PollIntervalSeconds) * time.Second
	failures, wasOffline := 0, false
	nextCostUpdate := time.Time{}

	for {
		started := time.Now()
		storageFailure := false
		m.metrics.EnergyCollectorRequests.Inc()
		device, address, credential, accessErr := m.deviceAccess(ctx, energyConfig.DeviceID)
		var reading shelly.EnergyReading
		err := accessErr
		if err == nil {
			client := m.newClient()
			var identity shelly.DeviceInfo
			identity, err = client.Identify(ctx, address)
			if err == nil {
				err = validateMeterIdentity(device, identity)
			}
			if err == nil {
				reading, err = client.ReadEnergy(ctx, address, energyConfig.ComponentKey, credential)
			}
		}
		m.metrics.EnergyCollectorDuration.Observe(time.Since(started).Seconds())
		if err == nil {
			err = validateEnergyReading(reading, energyConfig.MaximumExpectedPowerW, 2*interval)
		}
		if err == nil {
			var delta DeltaResult
			var nextState DeltaState
			nextState, delta, err = CalculateDelta(state, reading.Timestamp, reading.PowerW, reading.ImportedEnergyWh, interval, energyConfig.MaximumExpectedPowerW)
			if err == nil {
				quality := delta.Quality
				if quality == "" {
					quality = QualityMeasured
				}
				var nextAccumulators map[string]Accumulator
				var aggregateUpdates []database.EnergyAggregate
				nextAccumulators, aggregateUpdates, err = m.prepareAggregates(ctx, energyConfig, reading, delta, accumulators)
				storageFailure = errors.Is(err, errEnergyStorage)
				if err == nil {
					_, err = m.store.PersistEnergyReading(ctx, database.EnergySample{DeviceID: energyConfig.DeviceID, ComponentKey: energyConfig.ComponentKey, RecordedAt: reading.Timestamp, PowerW: reading.PowerW, ImportedEnergyTotalWh: reading.ImportedEnergyWh, ExportedEnergyTotalWh: reading.ExportedEnergyWh, VoltageV: reading.VoltageV, CurrentA: reading.CurrentA, PowerFactor: reading.PowerFactor, FrequencyHz: reading.FrequencyHz, ReadingQuality: quality}, aggregateUpdates)
					if err != nil {
						err = fmt.Errorf("%w: %v", errEnergyStorage, err)
						storageFailure = true
					}
				}
				if err == nil {
					state, accumulators = nextState, nextAccumulators
				}
				if err == nil && delta.CounterReset {
					m.metrics.EnergyCounterResets.Inc()
					m.recordEvent(energyConfig, "energy_counter_reset", map[string]any{"recorded_at": reading.Timestamp})
				}
				if err == nil {
					m.metrics.EnergyConsumptionKWh.Add(delta.EnergyWh / 1000)
				}
			}
		}

		wait := interval
		if err != nil {
			if !storageFailure {
				failures++
			}
			errorType := classifyCollectorError(err)
			m.metrics.EnergyCollectorErrors.WithLabelValues(errorType).Inc()
			m.metrics.EnergyMissingSamples.Inc()
			if !storageFailure {
				accumulators = m.recordMissing(ctx, energyConfig, time.Now(), accumulators)
			}
			if errors.Is(err, shelly.ErrAuthenticationRequired) {
				_ = m.store.MarkEnergyDeviceAuthenticationRequired(ctx, energyConfig.DeviceID)
			}
			if errorType == "invalid_reading" && failures == 1 {
				m.recordEvent(energyConfig, "invalid_reading", nil)
			}
			offline := !storageFailure && failures >= m.cfg.OfflineFailureThreshold
			if offline && !wasOffline {
				m.recordEvent(energyConfig, "meter_offline", map[string]any{"error_type": errorType})
				wasOffline = true
			}
			m.publish(LiveState{Configured: true, Enabled: true, Online: !offline, Stale: true, ConsecutiveFailures: failures, LastError: userCollectorError(err), DeviceID: energyConfig.DeviceID, ComponentKey: energyConfig.ComponentKey, PollIntervalSeconds: energyConfig.PollIntervalSeconds, Reading: m.Latest().Reading})
			wait = boundedBackoff(interval, failures)
			m.logger.Warn("energy collection failed", "device_id", energyConfig.DeviceID, "component_key", energyConfig.ComponentKey, "operation", "collect_energy", "duration", time.Since(started), "result", "failure", "error_type", errorType)
		} else {
			if wasOffline {
				m.recordEvent(energyConfig, "meter_online", nil)
			}
			failures, wasOffline = 0, false
			_ = m.store.MarkEnergyDeviceOnline(ctx, energyConfig.DeviceID)
			m.metrics.EnergyLastReading.Set(float64(reading.Timestamp.Unix()))
			if reading.PowerW != nil {
				m.metrics.EnergyCurrentPower.Set(*reading.PowerW)
			}
			stale := time.Since(reading.Timestamp) > 2*interval
			m.publish(LiveState{Configured: true, Enabled: true, Online: true, Stale: stale, DeviceID: device.ID, ComponentKey: energyConfig.ComponentKey, PollIntervalSeconds: energyConfig.PollIntervalSeconds, Reading: reading})
			if !reading.Timestamp.Before(nextCostUpdate) {
				m.updateCostMetric(ctx, energyConfig, reading.Timestamp)
				nextCostUpdate = reading.Timestamp.Add(time.Minute)
			}
			m.logger.Debug("energy reading collected", "device_id", energyConfig.DeviceID, "component_key", energyConfig.ComponentKey, "operation", "collect_energy", "reading_timestamp", reading.Timestamp, "duration", time.Since(started), "result", "success")
		}

		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
	}
}

func (m *Manager) prepareAggregates(ctx context.Context, config database.EnergyConfig, reading shelly.EnergyReading, delta DeltaResult, accumulators map[string]Accumulator) (map[string]Accumulator, []database.EnergyAggregate, error) {
	location, err := time.LoadLocation(config.Timezone)
	if err != nil {
		return nil, nil, errors.New("configured energy timezone is invalid")
	}
	next := cloneAccumulators(accumulators)
	updates := make([]database.EnergyAggregate, 0, 3)
	for _, intervalType := range []string{"minute", "hour", "day"} {
		start, end, err := IntervalBounds(reading.Timestamp, intervalType, location)
		if err != nil {
			return nil, nil, err
		}
		key := intervalType + ":" + start.UTC().Format(time.RFC3339Nano)
		accumulator, ok := next[key]
		if !ok {
			accumulator, err = m.restoreAccumulator(ctx, config, intervalType, start, end)
			if err != nil {
				return nil, nil, fmt.Errorf("%w: %v", errEnergyStorage, err)
			}
		}
		accumulator, err = accumulator.Add(reading.Timestamp.In(location), reading.PowerW, delta.EnergyWh, delta.Quality)
		if err != nil {
			return nil, nil, err
		}
		next[key] = accumulator
		updates = append(updates, aggregateRecord(config, intervalType, accumulator))
	}
	pruneAccumulators(next, reading.Timestamp)
	return next, updates, nil
}

func (m *Manager) restoreAccumulator(ctx context.Context, config database.EnergyConfig, intervalType string, start, end time.Time) (Accumulator, error) {
	accumulator, err := NewAccumulator(start, end)
	if err != nil {
		return Accumulator{}, err
	}
	items, err := m.store.ListEnergyAggregates(ctx, config.DeviceID, config.ComponentKey, start, end, intervalType)
	if err != nil || len(items) == 0 {
		return accumulator, err
	}
	existing := items[0]
	accumulator.energyWh = existing.EnergyConsumedWh
	accumulator.samples = existing.SampleCount
	accumulator.missing = existing.MissingSampleCount
	accumulator.quality = existing.CalculationQuality
	if existing.AveragePowerW != nil && existing.SampleCount > 0 {
		accumulator.powerSum = *existing.AveragePowerW * float64(existing.SampleCount)
	}
	if existing.MinimumPowerW != nil {
		accumulator.minimumPower = *existing.MinimumPowerW
	}
	if existing.MaximumPowerW != nil {
		accumulator.maximumPower = *existing.MaximumPowerW
	}
	return accumulator, nil
}

func (m *Manager) persistAccumulator(ctx context.Context, config database.EnergyConfig, intervalType string, accumulator Accumulator) error {
	_, err := m.store.UpsertEnergyAggregate(ctx, aggregateRecord(config, intervalType, accumulator))
	return err
}

func aggregateRecord(config database.EnergyConfig, intervalType string, accumulator Accumulator) database.EnergyAggregate {
	value := accumulator.Result()
	var average, minimum, maximum *float64
	if value.SampleCount > 0 {
		average, minimum, maximum = &value.AveragePowerW, &value.MinimumPowerW, &value.MaximumPowerW
	}
	return database.EnergyAggregate{IntervalType: intervalType, IntervalStart: value.Start, IntervalEnd: value.End, DeviceID: config.DeviceID, ComponentKey: config.ComponentKey, EnergyConsumedWh: value.EnergyConsumedWh, AveragePowerW: average, MinimumPowerW: minimum, MaximumPowerW: maximum, SampleCount: value.SampleCount, MissingSampleCount: value.MissingSampleCount, CalculationQuality: value.Quality}
}

func (m *Manager) recordMissing(ctx context.Context, config database.EnergyConfig, at time.Time, accumulators map[string]Accumulator) map[string]Accumulator {
	location, err := time.LoadLocation(config.Timezone)
	if err != nil {
		return accumulators
	}
	next := cloneAccumulators(accumulators)
	updates := make([]database.EnergyAggregate, 0, 3)
	for _, intervalType := range []string{"minute", "hour", "day"} {
		start, end, boundsErr := IntervalBounds(at, intervalType, location)
		if boundsErr != nil {
			continue
		}
		key := intervalType + ":" + start.UTC().Format(time.RFC3339Nano)
		accumulator, ok := next[key]
		if !ok {
			accumulator, err = m.restoreAccumulator(ctx, config, intervalType, start, end)
			if err != nil {
				continue
			}
		}
		accumulator = accumulator.Missing(1)
		next[key] = accumulator
		updates = append(updates, aggregateRecord(config, intervalType, accumulator))
	}
	if err := m.store.UpsertEnergyAggregates(ctx, updates); err != nil {
		return accumulators
	}
	pruneAccumulators(next, at)
	return next
}

func cloneAccumulators(values map[string]Accumulator) map[string]Accumulator {
	cloned := make(map[string]Accumulator, len(values))
	for key, value := range values {
		cloned[key] = value
	}
	return cloned
}

func pruneAccumulators(accumulators map[string]Accumulator, at time.Time) {
	for key, accumulator := range accumulators {
		if !accumulator.end.After(at) {
			delete(accumulators, key)
		}
	}
}

func (m *Manager) cleanupRetention(ctx context.Context, now time.Time) {
	result, err := m.store.CleanupEnergyData(ctx, now)
	if err != nil {
		if m.logger != nil {
			m.logger.Error("clean energy retention", "operation", "energy_retention", "error", err)
		}
		return
	}
	if m.logger != nil && (result.SamplesDeleted > 0 || result.MinuteAggregatesDeleted > 0 || result.HourlyAggregatesDeleted > 0) {
		m.logger.Info("energy retention cleaned", "operation", "energy_retention", "samples_deleted", result.SamplesDeleted, "minute_aggregates_deleted", result.MinuteAggregatesDeleted, "hourly_aggregates_deleted", result.HourlyAggregatesDeleted)
	}
}

func (m *Manager) updateCostMetric(ctx context.Context, config database.EnergyConfig, at time.Time) {
	location, err := time.LoadLocation(config.Timezone)
	if err != nil {
		return
	}
	cycle, err := billingcycle.CycleContaining(at.In(location), config.BillingCycleStartDay, location)
	if err != nil {
		return
	}
	report, err := BuildReport(ctx, m.store, config, cycle.Start, at.Add(time.Nanosecond), "day")
	if err == nil {
		m.metrics.EnergyEstimatedCost.Set(report.Cost.TotalIncludingVAT.InexactFloat64())
	}
}

func (m *Manager) deviceAccess(ctx context.Context, deviceID int64) (database.Device, netip.Addr, *shelly.Credential, error) {
	device, err := m.store.GetDevice(ctx, deviceID)
	if err != nil {
		return database.Device{}, netip.Addr{}, nil, err
	}
	address, err := netip.ParseAddr(device.IPAddress)
	if err != nil || !address.Is4() {
		return device, netip.Addr{}, nil, errors.New("selected meter has an invalid IPv4 address")
	}
	if device.CredentialProfileID == 0 {
		return device, address, nil, nil
	}
	if m.cipher == nil {
		return device, address, nil, errors.New("selected meter credentials cannot be decrypted")
	}
	username, encrypted, err := m.store.CredentialSecret(ctx, device.CredentialProfileID)
	if err != nil {
		return device, address, nil, err
	}
	password, err := m.cipher.Decrypt(encrypted)
	if err != nil {
		return device, address, nil, errors.New("selected meter credentials cannot be decrypted")
	}
	return device, address, &shelly.Credential{Username: username, Password: password}, nil
}

func (m *Manager) recordEvent(config database.EnergyConfig, eventType string, details map[string]any) {
	raw := []byte(`{}`)
	if details != nil {
		raw, _ = json.Marshal(details)
	}
	_, err := m.store.InsertMeterEvent(context.Background(), database.MeterEvent{DeviceID: config.DeviceID, ComponentKey: config.ComponentKey, EventType: eventType, OccurredAt: time.Now(), DetailsJSON: string(raw)})
	if err != nil && m.logger != nil {
		m.logger.Error("persist meter event", "device_id", config.DeviceID, "component_key", config.ComponentKey, "operation", "meter_event", "error", err)
	}
}

func (m *Manager) publish(state LiveState) {
	m.mu.Lock()
	m.latest = state
	for subscriber := range m.subscribers {
		select {
		case subscriber <- state:
		default:
			select {
			case <-subscriber:
			default:
			}
			select {
			case subscriber <- state:
			default:
			}
		}
	}
	m.mu.Unlock()
}

func validateCollectorConfig(config database.EnergyConfig) error {
	if config.DeviceID < 1 || strings.TrimSpace(config.ComponentKey) == "" {
		return errors.New("main energy meter and component are required")
	}
	if config.PollIntervalSeconds < 5 || config.PollIntervalSeconds > 300 {
		return errors.New("energy polling interval must be between 5 and 300 seconds")
	}
	if config.BillingCycleStartDay < 1 || config.BillingCycleStartDay > 31 {
		return errors.New("billing-cycle start day must be between 1 and 31")
	}
	if config.MaximumExpectedPowerW <= 0 || config.MaximumExpectedPowerW > 1_000_000 {
		return errors.New("maximum expected power is outside safe limits")
	}
	if _, err := time.LoadLocation(config.Timezone); err != nil {
		return errors.New("energy timezone must be a valid IANA timezone")
	}
	return nil
}

func validateEnergyReading(reading shelly.EnergyReading, maximumPowerW float64, maximumAge time.Duration) error {
	if reading.Timestamp.IsZero() || reading.Timestamp.After(time.Now().Add(5*time.Minute)) {
		return errors.New("energy reading timestamp is invalid")
	}
	if maximumAge > 0 && reading.Timestamp.Before(time.Now().Add(-maximumAge)) {
		return errors.New("energy reading timestamp is stale")
	}
	if reading.PowerW == nil && reading.ImportedEnergyWh == nil {
		return shelly.ErrNoUsableEnergyData
	}
	for name, value := range map[string]*float64{"power": reading.PowerW, "imported energy": reading.ImportedEnergyWh, "voltage": reading.VoltageV, "current": reading.CurrentA, "power factor": reading.PowerFactor, "frequency": reading.FrequencyHz} {
		if value != nil && (math.IsNaN(*value) || math.IsInf(*value, 0) || *value < 0) {
			return fmt.Errorf("%s reading is invalid", name)
		}
	}
	if maximumPowerW > 0 && reading.PowerW != nil && *reading.PowerW > maximumPowerW*1.5 {
		return errors.New("power reading exceeds configured maximum")
	}
	return nil
}

func validateMeterIdentity(device database.Device, identity shelly.DeviceInfo) error {
	if device.ShellyDeviceID != "" {
		if identity.ShellyDeviceID == "" || !strings.EqualFold(device.ShellyDeviceID, identity.ShellyDeviceID) {
			return errors.New("meter identity does not match the selected inventory device")
		}
		if device.MACAddress != "" {
			if identity.MACAddress == "" || !strings.EqualFold(device.MACAddress, identity.MACAddress) {
				return errors.New("meter MAC address does not match the selected inventory device")
			}
		}
		return nil
	}
	if device.MACAddress != "" && identity.MACAddress != "" && strings.EqualFold(device.MACAddress, identity.MACAddress) {
		return nil
	}
	return errors.New("meter identity cannot be verified")
}

func boundedBackoff(base time.Duration, failures int) time.Duration {
	if failures < 1 {
		return base
	}
	shift := min(failures-1, 6)
	wait := base * time.Duration(1<<shift)
	if wait > 5*time.Minute {
		return 5 * time.Minute
	}
	return wait
}

func classifyCollectorError(err error) string {
	switch {
	case errors.Is(err, errEnergyStorage):
		return "storage"
	case errors.Is(err, shelly.ErrAuthenticationRequired):
		return "authentication"
	case errors.Is(err, context.DeadlineExceeded), strings.Contains(strings.ToLower(err.Error()), "timeout"):
		return "timeout"
	case errors.Is(err, shelly.ErrNoUsableEnergyData), strings.Contains(strings.ToLower(err.Error()), "invalid"):
		return "invalid_reading"
	case errors.Is(err, sql.ErrNoRows):
		return "configuration"
	default:
		return "request"
	}
}

func userCollectorError(err error) string {
	if errors.Is(err, shelly.ErrAuthenticationRequired) {
		return "meter authentication failed"
	}
	return "meter reading failed: " + classifyCollectorError(err)
}
