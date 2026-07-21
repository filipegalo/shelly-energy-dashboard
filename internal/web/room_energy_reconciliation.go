package web

import (
	"fmt"
	"net/http"
	"sort"

	energyservice "github.com/filipegalo/shelly-energy-dashboard/internal/energy"
	roomcore "github.com/filipegalo/shelly-energy-dashboard/internal/energy/rooms"
	"github.com/shopspring/decimal"
)

func (s *Server) roomEnergyReconciliationPage(w http.ResponseWriter, r *http.Request) {
	view, err := s.buildRoomEnergyReconciliation(r)
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	s.render(w, r, viewData{Page: "energy-reconciliation", Title: "Room-meter reconciliation", RoomEnergy: &view})
}

func (s *Server) buildRoomEnergyReconciliation(r *http.Request) (roomEnergyViewModel, error) {
	config, err := s.store.GetEnergyConfig(r.Context())
	if err != nil {
		return roomEnergyViewModel{}, err
	}
	view := roomEnergyViewModel{Configured: roomEnergyConfigured(config), SelectedRange: selectedEnergyRange(r)}
	if r.URL.Query().Get("saved") == "1" {
		view.Message = "Reconciliation tolerance saved."
	}
	if !view.Configured {
		return view, nil
	}
	now, _, err := roomEnergyNow(config)
	if err != nil {
		return roomEnergyViewModel{}, err
	}
	start, end, interval, err := energyRange(view.SelectedRange, r, config, now)
	if err != nil {
		return roomEnergyViewModel{}, err
	}
	report, err := s.roomEnergyReport(r.Context(), config, start, end, interval)
	if err != nil {
		return roomEnergyViewModel{}, err
	}
	view.Reconciliation = reconciliationView(report)
	return view, nil
}

func reconciliationView(report energyservice.RoomEnergyReport) roomEnergyReconciliationVM {
	main := reportWholeEnergy(report)
	rooms := sumRoomEnergy(report)
	view := roomEnergyReconciliationVM{
		WholeHouseEnergy: decimalText(main, 2, "kWh"), RoomEnergy: decimalText(rooms, 2, "kWh"),
		UnmeteredEnergy: decimalText(report.Unmetered.EnergyKWh, 2, "kWh"), Coverage: percentageText(reportCoverage(report)),
		Difference: "—", DifferencePercent: "—", Status: reportReconciliationStatus(report), Quality: reportQualityCode(report),
		RelativeTolerance: decimal.NewFromFloat(report.Tolerance.RelativeTolerance).Mul(decimal.NewFromInt(100)).Round(2).String() + "%",
		AbsoluteTolerance: decimal.NewFromFloat(report.Tolerance.AbsoluteToleranceWh).Div(decimal.NewFromInt(1000)).Round(3).String() + " kWh",
	}
	if main != nil && rooms != nil {
		difference := rooms.Sub(*main)
		absolute := difference.Abs()
		view.Difference = decimalValueText(absolute, 2, "kWh")
		if main.IsPositive() {
			percent := absolute.Div(*main)
			view.DifferencePercent = percentageText(&percent) + " of whole-house energy"
		}
	}
	missingByRoom := map[int64]int{}
	roomNames := map[int64]string{}
	for _, room := range report.Rooms {
		roomNames[room.RoomID] = room.Name
		for _, point := range room.Points {
			if point.ExpectedSources > point.ReportingSources {
				missingByRoom[room.RoomID] += point.ExpectedSources - point.ReportingSources
			}
		}
	}
	missingRoomIDs := make([]int64, 0, len(missingByRoom))
	for roomID := range missingByRoom {
		missingRoomIDs = append(missingRoomIDs, roomID)
	}
	sort.Slice(missingRoomIDs, func(left, right int) bool {
		leftName, rightName := roomNames[missingRoomIDs[left]], roomNames[missingRoomIDs[right]]
		if leftName != rightName {
			return leftName < rightName
		}
		return missingRoomIDs[left] < missingRoomIDs[right]
	})
	for _, roomID := range missingRoomIDs {
		count := missingByRoom[roomID]
		view.MissingSources = append(view.MissingSources, fmt.Sprintf("%s — %d missing source-intervals", roomNames[roomID], count))
	}
	for _, point := range report.Reconciliation {
		if point.OverMeasured {
			view.SuspectedDoubleCounting = true
		}
		if point.Status == roomcore.ReconciliationWarning {
			view.WarningIntervals++
		}
	}
	return view
}

func reconciliationPointDifference(main *decimal.Decimal, rooms decimal.Decimal) *decimal.Decimal {
	if main == nil {
		return nil
	}
	value := rooms.Sub(*main).Abs()
	return &value
}
