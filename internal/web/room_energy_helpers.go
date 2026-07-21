package web

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/filipegalo/shelly-energy-dashboard/internal/database"
	energyservice "github.com/filipegalo/shelly-energy-dashboard/internal/energy"
	"github.com/filipegalo/shelly-energy-dashboard/internal/energy/billingcycle"
	roomcore "github.com/filipegalo/shelly-energy-dashboard/internal/energy/rooms"
	"github.com/go-chi/chi/v5"
	"github.com/shopspring/decimal"
)

type roomEnergyLiveSource struct {
	PowerW    *float64
	Online    bool
	Stale     bool
	UpdatedAt time.Time
	Error     string
}

type roomEnergyLiveRoom struct {
	PowerW           *float64
	ExpectedSources  int
	ReportingSources int
	StaleSources     int
	MissingSources   int
	Quality          string
	LastUpdate       time.Time
}

type roomEnergyLiveData struct {
	MainPowerW    *float64
	MainUpdatedAt time.Time
	Rooms         map[int64]roomEnergyLiveRoom
	Sources       map[database.EnergySourceRef]roomEnergyLiveSource
}

func roomAssignmentEffectiveTime(preset, raw string, config database.EnergyConfig, now time.Time) (time.Time, error) {
	location, err := time.LoadLocation(config.Timezone)
	if err != nil {
		return time.Time{}, errors.New("configured energy timezone is invalid")
	}
	localNow := now.In(location)
	switch strings.TrimSpace(preset) {
	case "", "now":
		return localNow, nil
	case "today":
		return time.Date(localNow.Year(), localNow.Month(), localNow.Day(), 0, 0, 0, 0, location), nil
	case "billing_cycle":
		cycle, cycleErr := billingcycle.CycleContaining(localNow, config.BillingCycleStartDay, location)
		if cycleErr != nil {
			return time.Time{}, cycleErr
		}
		return cycle.Start, nil
	case "custom":
		value := strings.TrimSpace(raw)
		if len(value) == 0 || len(value) > 32 {
			return time.Time{}, errors.New("effective time must use a valid local date and time")
		}
		parsed, parseErr := time.ParseInLocation("2006-01-02T15:04", value, location)
		if parseErr != nil || parsed.Format("2006-01-02T15:04") != value {
			return time.Time{}, errors.New("effective time must use a valid local date and time")
		}
		return parsed, nil
	default:
		return time.Time{}, errors.New("select a valid effective-date option")
	}
}

func roomEnergyConfigured(config database.EnergyConfig) bool {
	return config.DeviceID > 0 && strings.TrimSpace(config.ComponentKey) != ""
}

func roomEnergyNow(config database.EnergyConfig) (time.Time, *time.Location, error) {
	location, err := time.LoadLocation(config.Timezone)
	if err != nil {
		return time.Time{}, nil, errors.New("configured energy timezone is invalid")
	}
	return time.Now().In(location), location, nil
}

func (s *Server) roomEnergyReport(ctx context.Context, config database.EnergyConfig, start, end time.Time, interval string) (energyservice.RoomEnergyReport, error) {
	if !roomEnergyConfigured(config) {
		return energyservice.RoomEnergyReport{}, errors.New("configure the whole-house main meter first")
	}
	started := time.Now()
	report, err := energyservice.BuildRoomEnergyReport(ctx, s.store, config, start, end, interval)
	if s.energy != nil {
		s.energy.ObserveRoomEnergyReport(report, time.Since(started), err)
	}
	return report, err
}

func decimalText(value *decimal.Decimal, places int, suffix string) string {
	if value == nil {
		return "—"
	}
	text := value.Round(int32(places)).StringFixed(int32(places))
	if suffix != "" {
		text += " " + suffix
	}
	return text
}

func decimalValueText(value decimal.Decimal, places int, suffix string) string {
	copy := value
	return decimalText(&copy, places, suffix)
}

func decimalJSON(value *decimal.Decimal, places int) *string {
	if value == nil {
		return nil
	}
	text := value.Round(int32(places)).StringFixed(int32(places))
	return &text
}

func decimalValueJSON(value decimal.Decimal, places int) *string {
	copy := value
	return decimalJSON(&copy, places)
}

func floatPowerText(value *float64) string {
	if value == nil {
		return "—"
	}
	if *value >= 1000 {
		return fmt.Sprintf("%.2f kW", *value/1000)
	}
	return fmt.Sprintf("%.0f W", *value)
}

func decimalPowerText(value *decimal.Decimal) string {
	if value == nil {
		return "—"
	}
	floating, _ := value.Float64()
	return floatPowerText(&floating)
}

func percentageText(value *decimal.Decimal) string {
	if value == nil {
		return "—"
	}
	return value.Mul(decimal.NewFromInt(100)).Round(1).StringFixed(1) + "%"
}

func qualityText(value fmt.Stringer) string {
	if value == nil {
		return "Missing"
	}
	return titleWords(value.String())
}

func titleWords(value string) string {
	value = strings.ReplaceAll(strings.TrimSpace(value), "_", " ")
	if value == "" {
		return "Missing"
	}
	words := strings.Fields(value)
	for index := range words {
		words[index] = strings.ToUpper(words[index][:1]) + words[index][1:]
	}
	return strings.Join(words, " ")
}

func relativeReadingTime(value time.Time) string {
	if value.IsZero() {
		return "No reading"
	}
	age := time.Since(value)
	if age < 0 {
		age = 0
	}
	if age < time.Minute {
		return fmt.Sprintf("%ds ago", int(age.Seconds()))
	}
	if age < time.Hour {
		return fmt.Sprintf("%dm ago", int(age.Minutes()))
	}
	return value.Local().Format("2 Jan 15:04")
}

func roomEnergySeries(report energyservice.RoomEnergyReport, roomID int64) *energyservice.RoomEnergySeries {
	for index := range report.Rooms {
		if report.Rooms[index].RoomID == roomID {
			copy := report.Rooms[index]
			return &copy
		}
	}
	return nil
}

func sumRoomEnergy(report energyservice.RoomEnergyReport) *decimal.Decimal {
	total := decimal.Zero
	found := false
	for _, room := range report.Rooms {
		if room.EnergyKWh == nil {
			continue
		}
		total = total.Add(*room.EnergyKWh)
		found = true
	}
	if !found {
		return nil
	}
	return &total
}

func sumRoomCosts(report energyservice.RoomEnergyReport) decimal.Decimal {
	total := decimal.Zero
	for _, room := range report.Rooms {
		total = total.Add(room.AllocatedCost)
	}
	return total
}

func reportCoverage(report energyservice.RoomEnergyReport) *decimal.Decimal {
	main := report.WholeHouse.Consumption
	rooms := sumRoomEnergy(report)
	if rooms == nil || !main.IsPositive() {
		return nil
	}
	value := rooms.Div(main)
	return &value
}

func reportReconciliationStatus(report energyservice.RoomEnergyReport) string {
	status := "Missing main-meter data"
	for _, point := range report.Reconciliation {
		switch point.Status {
		case roomcore.ReconciliationWarning:
			return "Warning"
		case roomcore.ReconciliationWithinTolerance:
			status = "Within tolerance"
		case roomcore.ReconciliationHealthy:
			if status == "Missing main-meter data" {
				status = "Healthy"
			}
		}
	}
	return status
}

func reportQuality(report energyservice.RoomEnergyReport) string {
	worst := roomcore.QualityComplete
	for _, room := range report.Rooms {
		worst = worseWebQuality(worst, room.Quality)
	}
	for _, point := range report.Reconciliation {
		worst = worseWebQuality(worst, point.Quality)
	}
	return titleWords(string(worst))
}

func worseWebQuality(left, right roomcore.Quality) roomcore.Quality {
	rank := map[roomcore.Quality]int{
		roomcore.QualityComplete: 0, roomcore.QualityEstimated: 1, roomcore.QualityPartial: 2,
		roomcore.QualityStale: 3, roomcore.QualityMissing: 4, roomcore.QualityReconciliationWarning: 5,
	}
	if rank[right] > rank[left] {
		return right
	}
	return left
}

func assignmentState(assignment database.RoomEnergyAssignment, now time.Time) string {
	if assignment.EffectiveFrom.After(now) {
		return "future"
	}
	if assignment.EffectiveTo != nil && !assignment.EffectiveTo.After(now) {
		return "historical"
	}
	if !assignment.Enabled {
		return "disabled"
	}
	return "active"
}

func parsePositiveFormID(raw, label string) (int64, error) {
	id, err := strconv.ParseInt(strings.TrimSpace(raw), 10, 64)
	if err != nil || id < 1 {
		return 0, fmt.Errorf("select a valid %s", label)
	}
	return id, nil
}

func assignmentIDParam(r *http.Request) (int64, error) {
	return parsePositiveFormID(chi.URLParam(r, "assignmentID"), "assignment")
}

func roomIDParam(r *http.Request) (int64, error) {
	return parsePositiveFormID(chi.URLParam(r, "roomID"), "room")
}

func formIntDefault(r *http.Request, name string, fallback int) (int, error) {
	raw := strings.TrimSpace(r.FormValue(name))
	if raw == "" {
		return fallback, nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil {
		return 0, fmt.Errorf("%s must be a number", strings.ReplaceAll(name, "_", " "))
	}
	return value, nil
}

func (s *Server) roomEnergyLive(ctx context.Context, now time.Time, config database.EnergyConfig) (roomEnergyLiveData, error) {
	result := roomEnergyLiveData{Rooms: map[int64]roomEnergyLiveRoom{}, Sources: map[database.EnergySourceRef]roomEnergyLiveSource{}}
	if s.roomEnergy != nil {
		snapshot := s.roomEnergy.RoomLiveSnapshot()
		seenRoomSources := map[int64]map[database.EnergySourceRef]struct{}{}
		for _, source := range snapshot.Sources {
			ref := database.EnergySourceRef{DeviceID: source.Key.DeviceID, ComponentKey: source.Key.ComponentKey}
			live := source.Live
			state := roomEnergyLiveSource{PowerW: live.Reading.PowerW, Online: live.Online, Stale: live.Stale, UpdatedAt: live.Reading.Timestamp, Error: live.LastError}
			result.Sources[ref] = state
			if source.MainMeter {
				result.MainPowerW, result.MainUpdatedAt = live.Reading.PowerW, live.Reading.Timestamp
			}
			for _, assignment := range source.Assignments {
				if assignment.AggregationRole != database.RoomEnergyRolePrimary {
					continue
				}
				if seenRoomSources[assignment.RoomID] == nil {
					seenRoomSources[assignment.RoomID] = map[database.EnergySourceRef]struct{}{}
				}
				if _, duplicate := seenRoomSources[assignment.RoomID][ref]; duplicate {
					continue
				}
				seenRoomSources[assignment.RoomID][ref] = struct{}{}
				room := result.Rooms[assignment.RoomID]
				room.ExpectedSources++
				if live.Stale {
					room.StaleSources++
				} else if live.Online && live.Reading.PowerW != nil {
					room.ReportingSources++
					power := *live.Reading.PowerW
					if room.PowerW != nil {
						power += *room.PowerW
					}
					room.PowerW = &power
				} else {
					room.MissingSources++
				}
				if live.Reading.Timestamp.After(room.LastUpdate) {
					room.LastUpdate = live.Reading.Timestamp
				}
				result.Rooms[assignment.RoomID] = finalizedLiveRoom(room)
			}
		}
		if s.energy != nil && roomEnergyConfigured(config) {
			main := s.energy.Latest()
			if main.DeviceID == config.DeviceID && main.ComponentKey == config.ComponentKey {
				result.MainPowerW, result.MainUpdatedAt = main.Reading.PowerW, main.Reading.Timestamp
			}
		}
		if result.MainUpdatedAt.IsZero() && roomEnergyConfigured(config) {
			if sample, sampleErr := s.store.LatestEnergySample(ctx, config.DeviceID, config.ComponentKey); sampleErr == nil {
				result.MainPowerW, result.MainUpdatedAt = sample.PowerW, sample.RecordedAt
			}
		}
		return result, nil
	}

	assignments, err := s.store.ListEnabledRoomEnergyAssignmentsAt(ctx, now)
	if err != nil {
		return result, err
	}
	refs := make([]database.EnergySourceRef, 0, len(assignments))
	seen := map[database.EnergySourceRef]struct{}{}
	for _, assignment := range assignments {
		ref := database.EnergySourceRef{DeviceID: assignment.DeviceID, ComponentKey: assignment.ComponentKey}
		if _, duplicate := seen[ref]; !duplicate {
			seen[ref] = struct{}{}
			refs = append(refs, ref)
		}
	}
	if roomEnergyConfigured(config) {
		ref := database.EnergySourceRef{DeviceID: config.DeviceID, ComponentKey: config.ComponentKey}
		if _, duplicate := seen[ref]; !duplicate {
			refs = append(refs, ref)
		}
	}
	samples, err := s.store.ListLatestEnergySamplesForSources(ctx, refs)
	if err != nil {
		return result, err
	}
	for _, assignment := range assignments {
		ref := database.EnergySourceRef{DeviceID: assignment.DeviceID, ComponentKey: assignment.ComponentKey}
		sample, found := samples[ref]
		poll := assignment.PollIntervalSeconds
		if poll == 0 {
			poll = config.PollIntervalSeconds
		}
		stale := found && now.Sub(sample.RecordedAt) > 2*time.Duration(max(5, poll))*time.Second
		result.Sources[ref] = roomEnergyLiveSource{PowerW: sample.PowerW, Online: found && !stale, Stale: stale, UpdatedAt: sample.RecordedAt}
		if assignment.AggregationRole != database.RoomEnergyRolePrimary {
			continue
		}
		room := result.Rooms[assignment.RoomID]
		room.ExpectedSources++
		switch {
		case stale:
			room.StaleSources++
		case found && sample.PowerW != nil:
			room.ReportingSources++
			power := *sample.PowerW
			if room.PowerW != nil {
				power += *room.PowerW
			}
			room.PowerW = &power
		default:
			room.MissingSources++
		}
		if sample.RecordedAt.After(room.LastUpdate) {
			room.LastUpdate = sample.RecordedAt
		}
		result.Rooms[assignment.RoomID] = finalizedLiveRoom(room)
	}
	if roomEnergyConfigured(config) {
		main := samples[database.EnergySourceRef{DeviceID: config.DeviceID, ComponentKey: config.ComponentKey}]
		result.MainPowerW, result.MainUpdatedAt = main.PowerW, main.RecordedAt
	}
	return result, nil
}

func finalizedLiveRoom(room roomEnergyLiveRoom) roomEnergyLiveRoom {
	switch {
	case room.ReportingSources == room.ExpectedSources && room.ExpectedSources > 0:
		room.Quality = "complete"
	case room.ReportingSources > 0:
		room.Quality = "partial"
	case room.StaleSources > 0:
		room.Quality = "stale"
	default:
		room.Quality = "missing"
	}
	return room
}

func roomLivePowerTotal(rooms map[int64]roomEnergyLiveRoom) *float64 {
	total := 0.0
	found := false
	for _, room := range rooms {
		if room.PowerW == nil {
			continue
		}
		total += *room.PowerW
		found = true
	}
	if !found {
		return nil
	}
	return &total
}
