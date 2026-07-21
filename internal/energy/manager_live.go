package energy

import (
	"errors"
	"sort"
	"time"

	"github.com/filipegalo/shelly-energy-dashboard/internal/database"
	"github.com/filipegalo/shelly-energy-dashboard/internal/shelly"
)

const (
	RoomLiveEventSnapshot      = "room-energy-snapshot"
	RoomLiveEventReading       = "room-energy-reading"
	RoomLiveEventSourceOffline = "room-source-offline"
	RoomLiveEventSourceStale   = "room-source-stale"
	RoomLiveEventAssignments   = "assignment-changed"
	maxRoomSubscribers         = 64
)

var ErrTooManyRoomSubscribers = errors.New("room energy event subscriber limit reached")

type SourceKey struct {
	DeviceID     int64  `json:"device_id"`
	ComponentKey string `json:"component_key"`
}

type RoomSourceAssignment struct {
	AssignmentID    int64  `json:"assignment_id"`
	RoomID          int64  `json:"room_id"`
	DisplayName     string `json:"display_name"`
	AggregationRole string `json:"aggregation_role"`
}

type SourceLiveState struct {
	Key         SourceKey              `json:"source"`
	MainMeter   bool                   `json:"main_meter"`
	Assignments []RoomSourceAssignment `json:"assignments"`
	Live        LiveState              `json:"live"`
}

type RoomLiveSnapshot struct {
	Version   uint64            `json:"version"`
	UpdatedAt time.Time         `json:"updated_at"`
	Sources   []SourceLiveState `json:"sources"`
}

type RoomLiveEvent struct {
	Type     string           `json:"type"`
	Source   SourceLiveState  `json:"source,omitempty"`
	Snapshot RoomLiveSnapshot `json:"snapshot"`
}

func (m *Manager) SubscribeRoomEnergy() (<-chan RoomLiveEvent, func(), error) {
	channel := make(chan RoomLiveEvent, 4)
	m.mu.Lock()
	if m.roomSubscribers == nil {
		m.roomSubscribers = make(map[chan RoomLiveEvent]struct{})
	}
	if len(m.roomSubscribers) >= maxRoomSubscribers {
		m.mu.Unlock()
		return nil, func() {}, ErrTooManyRoomSubscribers
	}
	m.roomSubscribers[channel] = struct{}{}
	snapshot := m.roomLiveSnapshotLocked(time.Now())
	m.mu.Unlock()
	channel <- RoomLiveEvent{Type: RoomLiveEventSnapshot, Snapshot: snapshot}
	return channel, func() {
		m.mu.Lock()
		delete(m.roomSubscribers, channel)
		m.mu.Unlock()
	}, nil
}

func (m *Manager) RoomLiveSnapshot() RoomLiveSnapshot {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.roomLiveSnapshotLocked(time.Now())
}

func (m *Manager) roomLiveSnapshotLocked(now time.Time) RoomLiveSnapshot {
	snapshot := RoomLiveSnapshot{Version: m.roomVersion, UpdatedAt: m.roomUpdatedAt}
	if snapshot.UpdatedAt.IsZero() {
		snapshot.UpdatedAt = now.UTC()
	}
	for _, state := range m.sourceStates {
		if len(state.Assignments) == 0 {
			continue
		}
		copy := cloneSourceLiveState(state)
		copy.Live = liveStateAt(copy.Live, now)
		snapshot.Sources = append(snapshot.Sources, copy)
	}
	sort.Slice(snapshot.Sources, func(left, right int) bool {
		if snapshot.Sources[left].Key.DeviceID != snapshot.Sources[right].Key.DeviceID {
			return snapshot.Sources[left].Key.DeviceID < snapshot.Sources[right].Key.DeviceID
		}
		return snapshot.Sources[left].Key.ComponentKey < snapshot.Sources[right].Key.ComponentKey
	})
	return snapshot
}

func (m *Manager) publishSource(source collectorSource, live LiveState) {
	live = cloneLiveState(live)
	state := SourceLiveState{Key: source.Key, MainMeter: source.MainMeter, Assignments: cloneRoomAssignments(source.Assignments), Live: live}
	now := time.Now().UTC()

	m.mu.Lock()
	if m.sourceStates == nil {
		m.sourceStates = make(map[SourceKey]SourceLiveState)
	}
	m.sourceStates[source.Key] = state
	if source.MainMeter {
		m.publishMainLocked(live)
	}
	if len(source.Assignments) == 0 {
		m.mu.Unlock()
		return
	}
	m.roomVersion++
	m.roomUpdatedAt = now
	eventType := RoomLiveEventReading
	if !live.Online {
		eventType = RoomLiveEventSourceOffline
	} else if live.Stale {
		eventType = RoomLiveEventSourceStale
	}
	event := RoomLiveEvent{Type: eventType, Source: cloneSourceLiveState(state), Snapshot: m.roomLiveSnapshotLocked(now)}
	m.publishRoomLocked(event)
	m.updateRoomSourceMetricsLocked(now)
	m.mu.Unlock()
}

func (m *Manager) replaceSourceDefinitions(sources map[SourceKey]collectorSource) {
	now := time.Now().UTC()
	m.mu.Lock()
	if m.sourceStates == nil {
		m.sourceStates = make(map[SourceKey]SourceLiveState)
	}
	next := make(map[SourceKey]SourceLiveState, len(sources))
	changed := len(sources) != len(m.sourceStates)
	for key, source := range sources {
		live := LiveState{Configured: true, Enabled: true, DeviceID: key.DeviceID, ComponentKey: key.ComponentKey, PollIntervalSeconds: source.PollIntervalSeconds}
		if previous, found := m.sourceStates[key]; found {
			live = cloneLiveState(previous.Live)
			live.Configured = true
			live.Enabled = true
			live.DeviceID = key.DeviceID
			live.ComponentKey = key.ComponentKey
			live.PollIntervalSeconds = source.PollIntervalSeconds
			if previous.MainMeter != source.MainMeter || !roomAssignmentsEqual(previous.Assignments, source.Assignments) {
				changed = true
			}
		} else {
			changed = true
		}
		next[key] = SourceLiveState{Key: key, MainMeter: source.MainMeter, Assignments: cloneRoomAssignments(source.Assignments), Live: live}
	}
	m.sourceStates = next
	if changed {
		m.roomVersion++
		m.roomUpdatedAt = now
		event := RoomLiveEvent{Type: RoomLiveEventAssignments, Snapshot: m.roomLiveSnapshotLocked(now)}
		m.publishRoomLocked(event)
	}
	m.updateRoomSourceMetricsLocked(now)
	m.mu.Unlock()
}

func (m *Manager) publishRoomLocked(event RoomLiveEvent) {
	for subscriber := range m.roomSubscribers {
		copy := cloneRoomLiveEvent(event)
		select {
		case subscriber <- copy:
		default:
			select {
			case <-subscriber:
			default:
			}
			select {
			case subscriber <- copy:
			default:
			}
		}
	}
}

func (m *Manager) updateRoomSourceMetricsLocked(now time.Time) {
	if m.metrics == nil {
		return
	}
	total, online, stale := 0, 0, 0
	for _, source := range m.sourceStates {
		if len(source.Assignments) == 0 {
			continue
		}
		total++
		live := liveStateAt(source.Live, now)
		if live.Online {
			online++
		}
		if live.Stale {
			stale++
		}
	}
	m.metrics.RoomEnergySourcesTotal.Set(float64(total))
	m.metrics.RoomEnergySourcesOnline.Set(float64(online))
	m.metrics.RoomEnergySourcesStale.Set(float64(stale))
}

func liveStateAt(state LiveState, now time.Time) LiveState {
	copy := cloneLiveState(state)
	if copy.Online && !copy.Reading.Timestamp.IsZero() && copy.PollIntervalSeconds > 0 && now.Sub(copy.Reading.Timestamp) > 2*time.Duration(copy.PollIntervalSeconds)*time.Second {
		copy.Stale = true
	}
	return copy
}

func cloneRoomLiveEvent(event RoomLiveEvent) RoomLiveEvent {
	return RoomLiveEvent{Type: event.Type, Source: cloneSourceLiveState(event.Source), Snapshot: cloneRoomLiveSnapshot(event.Snapshot)}
}

func cloneRoomLiveSnapshot(snapshot RoomLiveSnapshot) RoomLiveSnapshot {
	copy := RoomLiveSnapshot{Version: snapshot.Version, UpdatedAt: snapshot.UpdatedAt, Sources: make([]SourceLiveState, len(snapshot.Sources))}
	for index, source := range snapshot.Sources {
		copy.Sources[index] = cloneSourceLiveState(source)
	}
	return copy
}

func cloneSourceLiveState(state SourceLiveState) SourceLiveState {
	return SourceLiveState{Key: state.Key, MainMeter: state.MainMeter, Assignments: cloneRoomAssignments(state.Assignments), Live: cloneLiveState(state.Live)}
}

func cloneRoomAssignments(assignments []RoomSourceAssignment) []RoomSourceAssignment {
	return append([]RoomSourceAssignment(nil), assignments...)
}

func cloneLiveState(state LiveState) LiveState {
	copy := state
	copy.Reading = cloneEnergyReading(state.Reading)
	return copy
}

func cloneEnergyReading(reading shelly.EnergyReading) shelly.EnergyReading {
	copy := reading
	copy.PowerW = copyFloat(reading.PowerW)
	copy.ImportedEnergyWh = copyFloat(reading.ImportedEnergyWh)
	copy.ExportedEnergyWh = copyFloat(reading.ExportedEnergyWh)
	copy.VoltageV = copyFloat(reading.VoltageV)
	copy.CurrentA = copyFloat(reading.CurrentA)
	copy.PowerFactor = copyFloat(reading.PowerFactor)
	copy.FrequencyHz = copyFloat(reading.FrequencyHz)
	copy.PhaseReadings = make([]shelly.PhaseReading, len(reading.PhaseReadings))
	for index, phase := range reading.PhaseReadings {
		copy.PhaseReadings[index] = phase
		copy.PhaseReadings[index].PowerW = copyFloat(phase.PowerW)
		copy.PhaseReadings[index].ImportedEnergyWh = copyFloat(phase.ImportedEnergyWh)
		copy.PhaseReadings[index].ExportedEnergyWh = copyFloat(phase.ExportedEnergyWh)
		copy.PhaseReadings[index].VoltageV = copyFloat(phase.VoltageV)
		copy.PhaseReadings[index].CurrentA = copyFloat(phase.CurrentA)
		copy.PhaseReadings[index].PowerFactor = copyFloat(phase.PowerFactor)
		copy.PhaseReadings[index].FrequencyHz = copyFloat(phase.FrequencyHz)
	}
	return copy
}

func roomAssignmentsEqual(left, right []RoomSourceAssignment) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func roomSourceAssignment(value database.RoomEnergyAssignment) RoomSourceAssignment {
	return RoomSourceAssignment{AssignmentID: value.ID, RoomID: value.RoomID, DisplayName: value.DisplayName, AggregationRole: value.AggregationRole}
}
