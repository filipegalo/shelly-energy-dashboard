package web

import (
	"fmt"
	"net/http"
	"sort"
	"time"

	"github.com/filipegalo/shelly-energy-dashboard/internal/database"
	energyservice "github.com/filipegalo/shelly-energy-dashboard/internal/energy"
	"github.com/filipegalo/shelly-energy-dashboard/internal/energy/billingcycle"
	roomcore "github.com/filipegalo/shelly-energy-dashboard/internal/energy/rooms"
	"github.com/shopspring/decimal"
)

func (s *Server) roomEnergyOverviewPage(w http.ResponseWriter, r *http.Request) {
	view, err := s.buildRoomEnergyOverview(r)
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	s.render(w, r, viewData{Page: "energy-rooms", Title: "Energy by room", RoomEnergy: &view})
}

func (s *Server) buildRoomEnergyOverview(r *http.Request) (roomEnergyViewModel, error) {
	config, err := s.store.GetEnergyConfig(r.Context())
	if err != nil {
		return roomEnergyViewModel{}, err
	}
	view := roomEnergyViewModel{Configured: roomEnergyConfigured(config), SelectedRange: selectedEnergyRange(r)}
	if r.URL.Query().Get("saved") == "1" {
		view.Message = "Room metering settings saved."
	}
	if !view.Configured {
		return view, nil
	}
	now, location, err := roomEnergyNow(config)
	if err != nil {
		return roomEnergyViewModel{}, err
	}
	todayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, location)
	cycle, err := billingcycle.CycleContaining(now, config.BillingCycleStartDay, location)
	if err != nil {
		return roomEnergyViewModel{}, err
	}
	today, err := s.roomEnergyReport(r.Context(), config, todayStart, now, "hour")
	if err != nil {
		return roomEnergyViewModel{}, err
	}
	billing, err := s.roomEnergyReport(r.Context(), config, cycle.Start, now, "day")
	if err != nil {
		return roomEnergyViewModel{}, err
	}
	live, err := s.roomEnergyLive(r.Context(), now, config)
	if err != nil {
		return roomEnergyViewModel{}, err
	}
	assignments, err := s.store.ListRoomEnergyAssignments(r.Context())
	if err != nil {
		return roomEnergyViewModel{}, err
	}
	roomRecords, err := s.store.ListRoomsIncludingArchived(r.Context())
	if err != nil {
		return roomEnergyViewModel{}, err
	}
	roomsByID := make(map[int64]database.Room, len(roomRecords))
	for _, room := range roomRecords {
		roomsByID[room.ID] = room
	}
	roomIDs := configuredRoomIDs(assignments, today, billing)
	view.Rooms = make([]roomEnergyRoomCardVM, 0, len(roomIDs))
	for _, roomID := range roomIDs {
		room := roomsByID[roomID]
		if room.ID == 0 || room.IsSystem {
			continue
		}
		view.Rooms = append(view.Rooms, roomEnergyCard(room, roomEnergySeries(today, roomID), roomEnergySeries(billing, roomID), reportWholeEnergy(billing), live.Rooms[roomID]))
	}
	sort.SliceStable(view.Rooms, func(left, right int) bool {
		return view.Rooms[left].Name < view.Rooms[right].Name
	})
	view.Unmetered = unmeteredEnergyCard(today, billing, live)
	view.Overview = roomEnergyOverviewSummary(today, live)
	return view, nil
}

func configuredRoomIDs(assignments []database.RoomEnergyAssignment, reports ...energyservice.RoomEnergyReport) []int64 {
	seen := map[int64]struct{}{}
	for _, assignment := range assignments {
		seen[assignment.RoomID] = struct{}{}
	}
	for _, report := range reports {
		for _, room := range report.Rooms {
			seen[room.RoomID] = struct{}{}
		}
	}
	ids := make([]int64, 0, len(seen))
	for id := range seen {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(left, right int) bool { return ids[left] < ids[right] })
	return ids
}

func roomEnergyCard(room database.Room, today, billing *energyservice.RoomEnergySeries, billingMain *decimal.Decimal, live roomEnergyLiveRoom) roomEnergyRoomCardVM {
	card := roomEnergyRoomCardVM{
		ID: room.ID, Name: room.Name, Icon: room.Icon, Archived: room.Archived,
		CurrentPower: floatPowerText(live.PowerW), Quality: live.Quality, LastUpdate: relativeReadingTime(live.LastUpdate),
		ReportingSources: live.ReportingSources, ExpectedSources: live.ExpectedSources,
		TodayEnergy: "—", TodayCost: "—", BillingEnergy: "—", BillingCost: "—", HouseShare: "—", PeakPower: "—",
	}
	if card.Quality == "" {
		card.Quality = string(roomcore.QualityMissing)
	}
	if today != nil {
		card.TodayEnergy = decimalText(today.EnergyKWh, 2, "kWh")
		if today.EnergyKWh != nil {
			card.TodayCost = "€" + today.AllocatedCost.Round(2).StringFixed(2)
		}
		card.Quality = string(today.Quality)
	}
	if billing != nil {
		card.BillingEnergy = decimalText(billing.EnergyKWh, 2, "kWh")
		if billing.EnergyKWh != nil {
			card.BillingCost = "€" + billing.AllocatedCost.Round(2).StringFixed(2)
		}
		card.PeakPower = decimalPowerText(seriesPeakPower(billing))
		if billing.EnergyKWh != nil && billingMain != nil && billingMain.IsPositive() {
			share := billing.EnergyKWh.Div(*billingMain)
			card.HouseShare = percentageText(&share)
		}
	}
	return card
}

func unmeteredEnergyCard(today, billing energyservice.RoomEnergyReport, live roomEnergyLiveData) roomEnergyRoomCardVM {
	roomPower := roomLivePowerTotal(live.Rooms)
	var unmeteredPower *float64
	if live.MainPowerW != nil && roomPower != nil {
		value := *live.MainPowerW - *roomPower
		if value < 0 {
			value = 0
		}
		unmeteredPower = &value
	}
	card := roomEnergyRoomCardVM{
		Name: "Unmetered / Other", Icon: "∑", Virtual: true, CurrentPower: floatPowerText(unmeteredPower),
		TodayEnergy: decimalText(today.Unmetered.EnergyKWh, 2, "kWh"), BillingEnergy: decimalText(billing.Unmetered.EnergyKWh, 2, "kWh"),
		TodayCost: "—", BillingCost: "—", HouseShare: "—", Quality: string(today.Unmetered.Quality), LastUpdate: relativeReadingTime(live.MainUpdatedAt), PeakPower: "—",
	}
	if today.Unmetered.EnergyKWh != nil {
		card.TodayCost = "€" + today.Unmetered.AllocatedCost.Round(2).StringFixed(2)
	}
	if billing.Unmetered.EnergyKWh != nil {
		card.BillingCost = "€" + billing.Unmetered.AllocatedCost.Round(2).StringFixed(2)
	}
	if coverage := reportCoverage(billing); coverage != nil {
		uncovered := decimal.NewFromInt(1).Sub(*coverage)
		if uncovered.IsNegative() {
			uncovered = decimal.Zero
		}
		card.HouseShare = percentageText(&uncovered)
	}
	return card
}

func roomEnergyOverviewSummary(today energyservice.RoomEnergyReport, live roomEnergyLiveData) roomEnergyOverviewVM {
	roomEnergy := sumRoomEnergy(today)
	roomPower := roomLivePowerTotal(live.Rooms)
	quality := reportQualityCode(today)
	summary := roomEnergyOverviewVM{
		WholeHousePower: floatPowerText(live.MainPowerW), RoomPower: floatPowerText(roomPower),
		WholeHouseToday: decimalText(reportWholeEnergy(today), 2, "kWh"), RoomToday: decimalText(roomEnergy, 2, "kWh"),
		UnmeteredToday: decimalText(today.Unmetered.EnergyKWh, 2, "kWh"), Coverage: percentageText(reportCoverage(today)),
		Status: reportReconciliationStatus(today), Quality: quality, WholeHouseCost: "—", RoomCost: "—", UnmeteredCost: "—",
	}
	if reportWholeEnergy(today) != nil {
		summary.WholeHouseCost = "€" + today.WholeHouse.Cost.TotalIncludingVAT.Round(2).StringFixed(2)
		summary.RoomCost = "€" + sumRoomCosts(today).Round(2).StringFixed(2)
		summary.UnmeteredCost = "€" + today.Unmetered.AllocatedCost.Round(2).StringFixed(2)
	}
	return summary
}

func reportWholeEnergy(report energyservice.RoomEnergyReport) *decimal.Decimal {
	for _, point := range report.WholeHouse.Points {
		if point.EnergyKWh != nil {
			value := report.WholeHouse.Consumption
			return &value
		}
	}
	return nil
}

func reportQualityCode(report energyservice.RoomEnergyReport) string {
	worst := roomcore.QualityComplete
	if len(report.Rooms) == 0 {
		worst = roomcore.QualityMissing
	}
	for _, room := range report.Rooms {
		worst = worseWebQuality(worst, room.Quality)
	}
	for _, point := range report.Reconciliation {
		worst = worseWebQuality(worst, point.Quality)
	}
	return string(worst)
}

func seriesPeakPower(series *energyservice.RoomEnergySeries) *decimal.Decimal {
	if series == nil {
		return nil
	}
	var peak *decimal.Decimal
	for _, point := range series.Points {
		if point.PeakPowerW == nil || (peak != nil && !point.PeakPowerW.GreaterThan(*peak)) {
			continue
		}
		value := *point.PeakPowerW
		peak = &value
	}
	return peak
}

func reportTimeDescription(start, end time.Time) string {
	return fmt.Sprintf("%s–%s", start.Format("2 Jan"), end.Format("2 Jan"))
}
