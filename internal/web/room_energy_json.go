package web

import (
	"database/sql"
	"errors"
	"net/http"
	"time"

	"github.com/filipegalo/shelly-energy-dashboard/internal/database"
	energyservice "github.com/filipegalo/shelly-energy-dashboard/internal/energy"
	roomcore "github.com/filipegalo/shelly-energy-dashboard/internal/energy/rooms"
	"github.com/shopspring/decimal"
)

type roomEnergyChartResponse struct {
	Range                roomEnergyChartRange                 `json:"range"`
	Interval             string                               `json:"interval"`
	Mode                 string                               `json:"mode"`
	Currency             string                               `json:"currency"`
	Series               []roomEnergyChartSeries              `json:"series"`
	WholeHousePoints     []roomEnergyWholeHouseChartPoint     `json:"whole_house_points"`
	ReconciliationPoints []roomEnergyReconciliationChartPoint `json:"reconciliation_points"`
}

type roomEnergyChartRange struct {
	Start    time.Time `json:"start"`
	End      time.Time `json:"end"`
	Timezone string    `json:"timezone"`
}

type roomEnergyChartSeries struct {
	RoomID   int64                  `json:"room_id,omitempty"`
	Name     string                 `json:"name"`
	Virtual  bool                   `json:"virtual"`
	Archived bool                   `json:"archived"`
	Points   []roomEnergyChartPoint `json:"points"`
}

type roomEnergyChartPoint struct {
	Start            time.Time `json:"start"`
	End              time.Time `json:"end"`
	EnergyKWh        *string   `json:"energy_kwh"`
	AllocatedCostEUR *string   `json:"allocated_cost_eur"`
	AveragePowerW    *string   `json:"average_power_w"`
	PeakPowerW       *string   `json:"peak_power_w"`
	HouseShare       *string   `json:"house_share"`
	CoverageRatio    *string   `json:"coverage_ratio"`
	Quality          string    `json:"quality"`
	ExpectedSources  int       `json:"expected_sources"`
	ReportingSources int       `json:"reporting_sources"`
}

type roomEnergyWholeHouseChartPoint struct {
	Start     time.Time `json:"start"`
	End       time.Time `json:"end"`
	EnergyKWh *string   `json:"energy_kwh"`
	Quality   string    `json:"quality"`
}

type roomEnergyReconciliationChartPoint struct {
	Start               time.Time `json:"start"`
	End                 time.Time `json:"end"`
	WholeHouseEnergyKWh *string   `json:"whole_house_energy_kwh"`
	RoomEnergyKWh       *string   `json:"room_energy_kwh"`
	UnmeteredEnergyKWh  *string   `json:"unmetered_energy_kwh"`
	DifferenceKWh       *string   `json:"difference_kwh"`
	CoverageRatio       *string   `json:"coverage_ratio"`
	Status              string    `json:"status"`
	Quality             string    `json:"quality"`
}

func (s *Server) roomEnergyOverviewChart(w http.ResponseWriter, r *http.Request) {
	report, config, mode, err := s.roomEnergyChartReport(r)
	if err != nil {
		s.roomEnergyJSONError(w, r, err)
		return
	}
	jsonResponse(w, http.StatusOK, roomEnergyChartPayload(report, config, mode, 0))
}

func (s *Server) roomEnergyRoomChart(w http.ResponseWriter, r *http.Request) {
	roomID, err := roomIDParam(r)
	if err != nil {
		jsonResponse(w, http.StatusNotFound, map[string]string{"error": "room not found"})
		return
	}
	if _, err := s.store.GetRoom(r.Context(), roomID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			jsonResponse(w, http.StatusNotFound, map[string]string{"error": "room not found"})
			return
		}
		s.roomEnergyJSONError(w, r, err)
		return
	}
	report, config, mode, err := s.roomEnergyChartReport(r)
	if err != nil {
		s.roomEnergyJSONError(w, r, err)
		return
	}
	jsonResponse(w, http.StatusOK, roomEnergyChartPayload(report, config, mode, roomID))
}

func (s *Server) roomEnergyReconciliationData(w http.ResponseWriter, r *http.Request) {
	report, config, _, err := s.roomEnergyChartReport(r)
	if err != nil {
		s.roomEnergyJSONError(w, r, err)
		return
	}
	payload := roomEnergyChartPayload(report, config, "consumption", 0)
	payload.Series = nil
	jsonResponse(w, http.StatusOK, payload)
}

func (s *Server) roomEnergyChartReport(r *http.Request) (energyservice.RoomEnergyReport, database.EnergyConfig, string, error) {
	config, err := s.store.GetEnergyConfig(r.Context())
	if err != nil {
		return energyservice.RoomEnergyReport{}, database.EnergyConfig{}, "", err
	}
	if !roomEnergyConfigured(config) {
		return energyservice.RoomEnergyReport{}, config, "", roomEnergyRequestError("configure the whole-house main meter first")
	}
	now, _, err := roomEnergyNow(config)
	if err != nil {
		return energyservice.RoomEnergyReport{}, config, "", err
	}
	start, end, interval, err := energyRange(selectedEnergyRange(r), r, config, now)
	if err != nil {
		return energyservice.RoomEnergyReport{}, config, "", roomEnergyRequestError(err.Error())
	}
	mode := r.URL.Query().Get("mode")
	if mode == "" {
		mode = "consumption"
	}
	validModes := map[string]bool{"consumption": true, "allocated_cost": true, "average_power": true, "peak_power": true, "house_share": true}
	if !validModes[mode] {
		return energyservice.RoomEnergyReport{}, config, "", roomEnergyRequestError("select a valid room chart display mode")
	}
	report, err := s.roomEnergyReport(r.Context(), config, start, end, interval)
	return report, config, mode, err
}

func roomEnergyChartPayload(report energyservice.RoomEnergyReport, config database.EnergyConfig, mode string, roomID int64) roomEnergyChartResponse {
	payload := roomEnergyChartResponse{
		Range:    roomEnergyChartRange{Start: report.Start, End: report.End, Timezone: config.Timezone},
		Interval: report.Interval, Mode: mode, Currency: config.Currency,
		Series: []roomEnergyChartSeries{}, WholeHousePoints: make([]roomEnergyWholeHouseChartPoint, 0, len(report.WholeHouse.Points)),
		ReconciliationPoints: make([]roomEnergyReconciliationChartPoint, 0, len(report.Reconciliation)),
	}
	for _, series := range report.Rooms {
		if roomID != 0 && series.RoomID != roomID {
			continue
		}
		payload.Series = append(payload.Series, chartSeries(series))
	}
	if roomID != 0 && len(payload.Series) == 0 {
		points := make([]roomEnergyChartPoint, 0, len(report.WholeHouse.Points))
		for _, whole := range report.WholeHouse.Points {
			points = append(points, roomEnergyChartPoint{Start: whole.Start, End: whole.End, Quality: string(roomcore.QualityMissing)})
		}
		payload.Series = append(payload.Series, roomEnergyChartSeries{RoomID: roomID, Name: "Room", Points: points})
	}
	if roomID == 0 {
		payload.Series = append(payload.Series, chartSeries(report.Unmetered))
	}
	for _, point := range report.WholeHouse.Points {
		payload.WholeHousePoints = append(payload.WholeHousePoints, roomEnergyWholeHouseChartPoint{Start: point.Start, End: point.End, EnergyKWh: point.EnergyKWh, Quality: normalizedChartQuality(point.Quality)})
	}
	presence := roomDataPresence(report)
	for _, point := range report.Reconciliation {
		var roomEnergy *string
		if presence[point.Start.UTC().Format(time.RFC3339Nano)] {
			roomEnergy = decimalValueJSON(point.RoomEnergyKWh, 3)
		}
		var unmetered, difference *string
		if point.MainEnergyKWh != nil {
			unmetered = decimalValueJSON(point.UnmeteredEnergyKWh, 3)
			difference = decimalValueJSON(point.DifferenceKWh, 3)
		}
		payload.ReconciliationPoints = append(payload.ReconciliationPoints, roomEnergyReconciliationChartPoint{
			Start: point.Start, End: point.End, WholeHouseEnergyKWh: decimalJSON(point.MainEnergyKWh, 3), RoomEnergyKWh: roomEnergy,
			UnmeteredEnergyKWh: unmetered, DifferenceKWh: difference, CoverageRatio: ratioPercentJSON(point.CoverageRatio),
			Status: string(point.Status), Quality: string(point.Quality),
		})
	}
	return payload
}

func chartSeries(series energyservice.RoomEnergySeries) roomEnergyChartSeries {
	result := roomEnergyChartSeries{RoomID: series.RoomID, Name: series.Name, Virtual: series.Virtual, Archived: series.Archived, Points: make([]roomEnergyChartPoint, 0, len(series.Points))}
	for _, point := range series.Points {
		result.Points = append(result.Points, roomEnergyChartPoint{
			Start: point.Start, End: point.End, EnergyKWh: decimalJSON(point.EnergyKWh, 3), AllocatedCostEUR: decimalJSON(point.AllocatedCost, 4),
			AveragePowerW: decimalJSON(point.AveragePowerW, 2), PeakPowerW: decimalJSON(point.PeakPowerW, 2), HouseShare: ratioPercentJSON(point.HouseShare),
			CoverageRatio: ratioPercentJSON(point.CoverageRatio), Quality: string(point.Quality), ExpectedSources: point.ExpectedSources, ReportingSources: point.ReportingSources,
		})
	}
	return result
}

func ratioPercentJSON(value *decimal.Decimal) *string {
	if value == nil {
		return nil
	}
	percent := value.Mul(decimal.NewFromInt(100))
	return decimalValueJSON(percent, 2)
}

func roomDataPresence(report energyservice.RoomEnergyReport) map[string]bool {
	result := map[string]bool{}
	for _, room := range report.Rooms {
		for _, point := range room.Points {
			if point.EnergyKWh != nil {
				result[point.Start.UTC().Format(time.RFC3339Nano)] = true
			}
		}
	}
	return result
}

func normalizedChartQuality(value string) string {
	switch value {
	case "", "measured":
		return string(roomcore.QualityComplete)
	case "calculated", "counter_reset":
		return string(roomcore.QualityEstimated)
	default:
		return value
	}
}

type roomEnergyRequestError string

func (e roomEnergyRequestError) Error() string { return string(e) }

func (s *Server) roomEnergyJSONError(w http.ResponseWriter, r *http.Request, err error) {
	var requestError roomEnergyRequestError
	if errors.As(err, &requestError) {
		jsonResponse(w, http.StatusUnprocessableEntity, map[string]string{"error": requestError.Error()})
		return
	}
	if s.logger != nil {
		s.logger.Error("room energy JSON request failed", "operation", r.URL.Path, "error", err)
	}
	jsonResponse(w, http.StatusInternalServerError, map[string]string{"error": "An internal error occurred."})
}

type roomEnergySourceJSON struct {
	AssignmentID       int64   `json:"assignment_id"`
	DeviceID           int64   `json:"device_id"`
	ComponentKey       string  `json:"component_key"`
	DisplayName        string  `json:"display_name"`
	Role               string  `json:"role"`
	ParentAssignmentID int64   `json:"parent_assignment_id,omitempty"`
	PowerW             *string `json:"power_w"`
	EnergyKWh          *string `json:"energy_kwh"`
	Online             bool    `json:"online"`
	Stale              bool    `json:"stale"`
	Quality            string  `json:"quality"`
}

func (s *Server) roomEnergyRoomSources(w http.ResponseWriter, r *http.Request) {
	roomID, err := roomIDParam(r)
	if err != nil {
		jsonResponse(w, http.StatusNotFound, map[string]string{"error": "room not found"})
		return
	}
	if _, err := s.store.GetRoom(r.Context(), roomID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			jsonResponse(w, http.StatusNotFound, map[string]string{"error": "room not found"})
			return
		}
		s.roomEnergyJSONError(w, r, err)
		return
	}
	report, config, _, err := s.roomEnergyChartReport(r)
	if err != nil {
		s.roomEnergyJSONError(w, r, err)
		return
	}
	now, _, _ := roomEnergyNow(config)
	live, err := s.roomEnergyLive(r.Context(), now, config)
	if err != nil {
		s.roomEnergyJSONError(w, r, err)
		return
	}
	assignments, err := s.store.ListRoomEnergyAssignmentsOverlapping(r.Context(), report.Start, report.End)
	if err != nil {
		s.roomEnergyJSONError(w, r, err)
		return
	}
	energyByAssignment := assignmentEnergyMap(roomEnergySeries(report, roomID))
	items := make([]roomEnergySourceJSON, 0)
	for _, assignment := range assignments {
		if assignment.RoomID != roomID {
			continue
		}
		state := live.Sources[database.EnergySourceRef{DeviceID: assignment.DeviceID, ComponentKey: assignment.ComponentKey}]
		var power *string
		if state.PowerW != nil {
			value := decimal.NewFromFloat(*state.PowerW)
			power = decimalValueJSON(value, 2)
		}
		quality := string(roomcore.QualityMissing)
		if state.Stale {
			quality = string(roomcore.QualityStale)
		} else if state.Online {
			quality = string(roomcore.QualityComplete)
		}
		items = append(items, roomEnergySourceJSON{AssignmentID: assignment.ID, DeviceID: assignment.DeviceID, ComponentKey: assignment.ComponentKey, DisplayName: assignment.DisplayName, Role: assignment.AggregationRole, ParentAssignmentID: assignment.ParentAssignmentID, PowerW: power, EnergyKWh: decimalJSON(energyByAssignment[assignment.ID], 3), Online: state.Online, Stale: state.Stale, Quality: quality})
	}
	jsonResponse(w, http.StatusOK, map[string]any{"range": roomEnergyChartRange{Start: report.Start, End: report.End, Timezone: config.Timezone}, "room_id": roomID, "sources": items})
}
