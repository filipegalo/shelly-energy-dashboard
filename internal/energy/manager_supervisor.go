package energy

import (
	"context"
	"errors"
	"sort"
	"time"

	"github.com/filipegalo/shelly-energy-dashboard/internal/database"
)

const (
	maximumCollectorConcurrency = 8
	supervisorFallbackInterval  = time.Minute
	supervisorBoundaryGrace     = 5 * time.Millisecond
)

var maximumAssignmentTime = time.Date(9999, time.December, 31, 23, 59, 59, 0, time.UTC)

type collectorSource struct {
	Key                 SourceKey
	MainMeter           bool
	Assignments         []RoomSourceAssignment
	PollIntervalSeconds int
	Config              database.EnergyConfig
}

type collectorSelection struct {
	Sources      map[SourceKey]collectorSource
	NextBoundary time.Time
	MainState    LiveState
}

type collectorWorker struct {
	source collectorSource
	cancel context.CancelFunc
	done   chan struct{}
}

func selectCollectorSources(mainConfig database.EnergyConfig, roomConfig database.RoomEnergyConfig, assignments []database.RoomEnergyAssignment, now time.Time) (collectorSelection, error) {
	if now.IsZero() {
		return collectorSelection{}, errors.New("collector source selection time is required")
	}
	now = now.UTC()
	selection := collectorSelection{
		Sources: make(map[SourceKey]collectorSource),
		MainState: LiveState{
			Configured:          mainConfig.DeviceID > 0 && mainConfig.ComponentKey != "",
			Enabled:             mainConfig.Enabled,
			DeviceID:            mainConfig.DeviceID,
			ComponentKey:        mainConfig.ComponentKey,
			PollIntervalSeconds: mainConfig.PollIntervalSeconds,
		},
	}
	mainKey := SourceKey{DeviceID: mainConfig.DeviceID, ComponentKey: mainConfig.ComponentKey}
	if mainConfig.Enabled {
		if err := validateCollectorConfig(mainConfig); err != nil {
			return collectorSelection{}, err
		}
		selection.Sources[mainKey] = collectorSource{Key: mainKey, MainMeter: true, PollIntervalSeconds: mainConfig.PollIntervalSeconds, Config: mainConfig}
	}

	for _, assignment := range assignments {
		if !assignment.Enabled || assignment.RoomArchived {
			continue
		}
		collectRole := assignment.AggregationRole == database.RoomEnergyRolePrimary || assignment.AggregationRole == database.RoomEnergyRoleSubmeter || (assignment.AggregationRole == database.RoomEnergyRoleExcluded && roomConfig.DiagnosticCollection)
		if !collectRole {
			continue
		}
		if assignment.EffectiveFrom.After(now) {
			selection.NextBoundary = earlierBoundary(selection.NextBoundary, assignment.EffectiveFrom)
			continue
		}
		if assignment.EffectiveTo != nil {
			if !assignment.EffectiveTo.After(now) {
				continue
			}
			selection.NextBoundary = earlierBoundary(selection.NextBoundary, *assignment.EffectiveTo)
		}

		key := SourceKey{DeviceID: assignment.DeviceID, ComponentKey: assignment.ComponentKey}
		if key == mainKey && mainConfig.Enabled && !assignment.MainMeterAcknowledged {
			return collectorSelection{}, errors.New("room assignment reusing the main meter requires confirmation")
		}
		pollInterval := assignment.PollIntervalSeconds
		if pollInterval == 0 {
			pollInterval = mainConfig.PollIntervalSeconds
		}
		source, found := selection.Sources[key]
		if !found {
			config := mainConfig
			config.Enabled = true
			config.DeviceID = assignment.DeviceID
			config.ComponentKey = assignment.ComponentKey
			config.ComponentType = assignment.ComponentType
			config.PollIntervalSeconds = pollInterval
			if err := validateCollectorConfig(config); err != nil {
				return collectorSelection{}, err
			}
			source = collectorSource{Key: key, PollIntervalSeconds: pollInterval, Config: config}
		} else if pollInterval < source.PollIntervalSeconds {
			source.PollIntervalSeconds = pollInterval
			source.Config.PollIntervalSeconds = pollInterval
		}
		source.Assignments = append(source.Assignments, roomSourceAssignment(assignment))
		selection.Sources[key] = source
	}

	for key, source := range selection.Sources {
		sort.Slice(source.Assignments, func(left, right int) bool {
			return source.Assignments[left].AssignmentID < source.Assignments[right].AssignmentID
		})
		selection.Sources[key] = source
	}
	return selection, nil
}

func earlierBoundary(current, candidate time.Time) time.Time {
	if candidate.IsZero() || (!current.IsZero() && !candidate.Before(current)) {
		return current
	}
	return candidate.UTC()
}

func (m *Manager) loadCollectorSelection(ctx context.Context, now time.Time) (collectorSelection, error) {
	mainConfig, err := m.store.GetEnergyConfig(ctx)
	if err != nil {
		return collectorSelection{}, err
	}
	roomConfig, err := m.store.GetRoomEnergyConfig(ctx)
	if err != nil {
		return collectorSelection{}, err
	}
	assignments, err := m.store.ListRoomEnergyAssignmentsOverlapping(ctx, now, maximumAssignmentTime)
	if err != nil {
		return collectorSelection{}, err
	}
	return selectCollectorSources(mainConfig, roomConfig, assignments, now)
}

func (m *Manager) Start(ctx context.Context) error {
	m.startHousekeeping()
	m.lifecycleMu.Lock()
	m.ensureRuntimeLocked()
	m.stopped = false
	if m.supervisorCancel == nil {
		supervisorCtx, cancel := context.WithCancel(m.appCtx)
		done := make(chan struct{})
		m.supervisorCancel, m.supervisorDone = cancel, done
		go m.runSourceSupervisor(supervisorCtx, done)
	}
	m.lifecycleMu.Unlock()
	if err := m.Reconfigure(ctx); err != nil {
		return err
	}
	m.wakeSupervisor()
	return nil
}

func (m *Manager) Reconfigure(ctx context.Context) error {
	m.lifecycleMu.Lock()
	defer m.lifecycleMu.Unlock()
	if m.stopped {
		return nil
	}
	m.ensureRuntimeLocked()
	selection, err := m.loadCollectorSelection(ctx, time.Now())
	if err != nil {
		return err
	}
	m.applyCollectorSelectionLocked(selection)
	m.nextBoundary = selection.NextBoundary
	m.publishConfiguredMain(selection)
	m.wakeSupervisorLocked()
	return nil
}

func (m *Manager) Stop() {
	m.lifecycleMu.Lock()
	m.stopped = true
	supervisorCancel, supervisorDone := m.supervisorCancel, m.supervisorDone
	m.supervisorCancel, m.supervisorDone = nil, nil
	m.lifecycleMu.Unlock()
	if supervisorCancel != nil {
		supervisorCancel()
	}
	if supervisorDone != nil {
		<-supervisorDone
	}

	m.lifecycleMu.Lock()
	keys := sortedCollectorSourceKeys(m.workers)
	for _, key := range keys {
		m.stopWorkerLocked(key)
	}
	m.nextBoundary = time.Time{}
	housekeepingCancel, housekeepingDone := m.housekeepingCancel, m.housekeepingDone
	m.housekeepingCancel, m.housekeepingDone = nil, nil
	m.lifecycleMu.Unlock()
	if housekeepingCancel != nil {
		housekeepingCancel()
	}
	if housekeepingDone != nil {
		<-housekeepingDone
	}
}

func (m *Manager) ensureRuntimeLocked() {
	if m.appCtx == nil {
		m.appCtx = context.Background()
	}
	if m.workers == nil {
		m.workers = make(map[SourceKey]*collectorWorker)
	}
	if m.supervisorWake == nil {
		m.supervisorWake = make(chan struct{}, 1)
	}
	if m.requestSlots == nil {
		m.requestSlots = make(chan struct{}, maximumCollectorConcurrency)
	}
}

func (m *Manager) applyCollectorSelectionLocked(selection collectorSelection) {
	for _, key := range sortedCollectorSourceKeys(m.workers) {
		desired, keep := selection.Sources[key]
		if keep && collectorSourcesEqual(m.workers[key].source, desired) {
			continue
		}
		m.stopWorkerLocked(key)
	}
	m.replaceSourceDefinitions(selection.Sources)
	for _, key := range sortedCollectorSources(selection.Sources) {
		if _, running := m.workers[key]; running {
			continue
		}
		m.startWorkerLocked(selection.Sources[key])
	}
}

func (m *Manager) startWorkerLocked(source collectorSource) {
	workerCtx, cancel := context.WithCancel(m.appCtx)
	worker := &collectorWorker{source: source, cancel: cancel, done: make(chan struct{})}
	m.workers[source.Key] = worker
	if m.onWorkerStart != nil {
		m.onWorkerStart(source.Key)
	}
	go m.run(workerCtx, worker.done, source)
}

func (m *Manager) stopWorkerLocked(key SourceKey) {
	worker, found := m.workers[key]
	if !found {
		return
	}
	worker.cancel()
	<-worker.done
	delete(m.workers, key)
	if m.onWorkerStop != nil {
		m.onWorkerStop(key)
	}
}

func (m *Manager) RunningSourceKeys() []SourceKey {
	m.lifecycleMu.Lock()
	defer m.lifecycleMu.Unlock()
	return sortedCollectorSourceKeys(m.workers)
}

func sortedCollectorSources(values map[SourceKey]collectorSource) []SourceKey {
	keys := make([]SourceKey, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sortSourceKeys(keys)
	return keys
}

func sortedCollectorSourceKeys(values map[SourceKey]*collectorWorker) []SourceKey {
	keys := make([]SourceKey, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sortSourceKeys(keys)
	return keys
}

func sortSourceKeys(keys []SourceKey) {
	sort.Slice(keys, func(left, right int) bool {
		if keys[left].DeviceID != keys[right].DeviceID {
			return keys[left].DeviceID < keys[right].DeviceID
		}
		return keys[left].ComponentKey < keys[right].ComponentKey
	})
}

func collectorSourcesEqual(left, right collectorSource) bool {
	return left.Key == right.Key && left.MainMeter == right.MainMeter && left.PollIntervalSeconds == right.PollIntervalSeconds && left.Config.ComponentType == right.Config.ComponentType && left.Config.Timezone == right.Config.Timezone && left.Config.BillingCycleStartDay == right.Config.BillingCycleStartDay && left.Config.MaximumExpectedPowerW == right.Config.MaximumExpectedPowerW && roomAssignmentsEqual(left.Assignments, right.Assignments)
}

func (m *Manager) runSourceSupervisor(ctx context.Context, done chan struct{}) {
	defer close(done)
	timer := time.NewTimer(supervisorFallbackInterval)
	defer timer.Stop()
	for {
		delay := m.nextSupervisorDelay()
		if !timer.Stop() {
			select {
			case <-timer.C:
			default:
			}
		}
		timer.Reset(delay)
		select {
		case <-ctx.Done():
			return
		case <-m.supervisorWake:
			continue
		case <-timer.C:
			_ = m.Reconfigure(ctx)
		}
	}
}

func (m *Manager) nextSupervisorDelay() time.Duration {
	m.lifecycleMu.Lock()
	boundary := m.nextBoundary
	m.lifecycleMu.Unlock()
	if boundary.IsZero() {
		return supervisorFallbackInterval
	}
	delay := time.Until(boundary) + supervisorBoundaryGrace
	if delay < supervisorBoundaryGrace {
		return supervisorBoundaryGrace
	}
	if delay > supervisorFallbackInterval {
		return supervisorFallbackInterval
	}
	return delay
}

func (m *Manager) wakeSupervisor() {
	m.lifecycleMu.Lock()
	m.wakeSupervisorLocked()
	m.lifecycleMu.Unlock()
}

func (m *Manager) wakeSupervisorLocked() {
	if m.supervisorWake == nil {
		return
	}
	select {
	case m.supervisorWake <- struct{}{}:
	default:
	}
}

func (m *Manager) publishConfiguredMain(selection collectorSelection) {
	state := selection.MainState
	key := SourceKey{DeviceID: state.DeviceID, ComponentKey: state.ComponentKey}
	m.mu.Lock()
	if source, found := m.sourceStates[key]; found && source.MainMeter {
		state = cloneLiveState(source.Live)
		state.Configured = selection.MainState.Configured
		state.Enabled = selection.MainState.Enabled
		state.DeviceID = selection.MainState.DeviceID
		state.ComponentKey = selection.MainState.ComponentKey
		state.PollIntervalSeconds = selection.MainState.PollIntervalSeconds
	}
	m.publishMainLocked(state)
	m.mu.Unlock()
}

func (m *Manager) withCollectionSlot(ctx context.Context, operation func() error) error {
	select {
	case m.requestSlots <- struct{}{}:
		defer func() { <-m.requestSlots }()
		return operation()
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (m *Manager) latestForSource(key SourceKey) LiveState {
	m.mu.Lock()
	defer m.mu.Unlock()
	if state, found := m.sourceStates[key]; found {
		return cloneLiveState(state.Live)
	}
	return LiveState{Configured: true, Enabled: true, DeviceID: key.DeviceID, ComponentKey: key.ComponentKey}
}
