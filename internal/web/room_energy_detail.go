package web

import (
	"database/sql"
	"errors"
	"net/http"
	"sort"
	"time"

	"github.com/filipegalo/shelly-energy-dashboard/internal/database"
	energyservice "github.com/filipegalo/shelly-energy-dashboard/internal/energy"
	"github.com/filipegalo/shelly-energy-dashboard/internal/energy/billingcycle"
	roomcore "github.com/filipegalo/shelly-energy-dashboard/internal/energy/rooms"
	"github.com/shopspring/decimal"
)

func (s *Server) roomEnergyDetailPage(w http.ResponseWriter, r *http.Request) {
	roomID, err := roomIDParam(r)
	if err != nil {
		s.notFound(w, r)
		return
	}
	room, err := s.store.GetRoom(r.Context(), roomID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			s.notFound(w, r)
			return
		}
		s.serverError(w, r, err)
		return
	}
	view, err := s.buildRoomEnergyDetail(r, room)
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	s.render(w, r, viewData{Page: "energy-room", Title: room.Name + " energy", RoomEnergy: &view})
}

func (s *Server) buildRoomEnergyDetail(r *http.Request, room database.Room) (roomEnergyViewModel, error) {
	config, err := s.store.GetEnergyConfig(r.Context())
	if err != nil {
		return roomEnergyViewModel{}, err
	}
	view := roomEnergyViewModel{Configured: roomEnergyConfigured(config), SelectedRange: selectedEnergyRange(r)}
	view.Room = roomEnergyRoomDetailVM{roomEnergyRoomCardVM: roomEnergyRoomCardVM{ID: room.ID, Name: room.Name, Icon: room.Icon, Archived: room.Archived, CurrentPower: "—", TodayEnergy: "—", TodayCost: "—", BillingEnergy: "—", BillingCost: "—", HouseShare: "—", PeakPower: "—", Quality: "missing", LastUpdate: "No reading"}, WeekEnergy: "—", MonthEnergy: "—", Coverage: "—", ReconciliationStatus: "Missing main-meter data"}
	if !view.Configured {
		return view, nil
	}
	now, location, err := roomEnergyNow(config)
	if err != nil {
		return roomEnergyViewModel{}, err
	}
	todayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, location)
	weekStart := todayStart.AddDate(0, 0, -((int(todayStart.Weekday()) + 6) % 7))
	monthStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, location)
	cycle, err := billingcycle.CycleContaining(now, config.BillingCycleStartDay, location)
	if err != nil {
		return roomEnergyViewModel{}, err
	}
	today, err := s.roomEnergyReport(r.Context(), config, todayStart, now, "hour")
	if err != nil {
		return roomEnergyViewModel{}, err
	}
	week, err := s.roomEnergyReport(r.Context(), config, weekStart, now, "day")
	if err != nil {
		return roomEnergyViewModel{}, err
	}
	month, err := s.roomEnergyReport(r.Context(), config, monthStart, now, "day")
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
	card := roomEnergyCard(room, roomEnergySeries(today, room.ID), roomEnergySeries(billing, room.ID), reportWholeEnergy(billing), live.Rooms[room.ID])
	view.Room.roomEnergyRoomCardVM = card
	view.Room.WeekEnergy = decimalText(seriesEnergy(week, room.ID), 2, "kWh")
	view.Room.MonthEnergy = decimalText(seriesEnergy(month, room.ID), 2, "kWh")
	view.Room.PeakPower = decimalPowerText(seriesPeakPower(roomEnergySeries(billing, room.ID)))
	view.Room.Coverage = seriesCoverageText(roomEnergySeries(billing, room.ID))
	view.Room.ReconciliationStatus = reportReconciliationStatus(billing)
	view.Room.Sources, err = s.buildRoomEnergySources(r, room.ID, cycle.Start, now, roomEnergySeries(today, room.ID), roomEnergySeries(billing, room.ID), live)
	if err != nil {
		return roomEnergyViewModel{}, err
	}
	billingSeries := roomEnergySeries(billing, room.ID)
	view.Room.Cost = roomEnergyCostView(billing, billingSeries)
	return view, nil
}

func seriesEnergy(report energyservice.RoomEnergyReport, roomID int64) *decimal.Decimal {
	series := roomEnergySeries(report, roomID)
	if series == nil {
		return nil
	}
	return series.EnergyKWh
}

func seriesCoverageText(series *energyservice.RoomEnergySeries) string {
	if series == nil {
		return "—"
	}
	expected, reporting := 0, 0
	for _, point := range series.Points {
		expected += point.ExpectedSources
		reporting += point.ReportingSources
	}
	if expected == 0 {
		return "—"
	}
	return decimal.NewFromInt(int64(reporting)).Div(decimal.NewFromInt(int64(expected))).Mul(decimal.NewFromInt(100)).Round(1).StringFixed(1) + "%"
}

func roomEnergyCostView(report energyservice.RoomEnergyReport, series *energyservice.RoomEnergySeries) roomEnergyCostVM {
	view := roomEnergyCostVM{MeasuredEnergy: "—", HouseShare: "—", WholeHouseCost: "—", AllocatedCost: "—", AllocationMethod: "Proportional whole-house allocation", Quality: "missing", ReconciliationStatus: reportReconciliationStatus(report)}
	main := reportWholeEnergy(report)
	if main != nil {
		view.WholeHouseCost = "€" + report.WholeHouse.Cost.TotalIncludingVAT.Round(2).StringFixed(2)
	}
	if series == nil || series.EnergyKWh == nil {
		return view
	}
	view.MeasuredEnergy = decimalText(series.EnergyKWh, 2, "kWh")
	view.AllocatedCost = "€" + series.AllocatedCost.Round(2).StringFixed(2)
	view.Quality = string(series.Quality)
	if main != nil && main.IsPositive() {
		share := series.EnergyKWh.Div(*main)
		view.HouseShare = percentageText(&share)
	}
	for _, point := range series.Points {
		if point.AllocationNormalized {
			view.Normalized = true
			view.AllocationMethod = "Normalized across measured rooms"
			break
		}
	}
	return view
}

func (s *Server) buildRoomEnergySources(r *http.Request, roomID int64, start, end time.Time, today, billing *energyservice.RoomEnergySeries, live roomEnergyLiveData) ([]roomEnergySourceVM, error) {
	assignments, err := s.store.ListRoomEnergyAssignmentsOverlapping(r.Context(), start, end)
	if err != nil {
		return nil, err
	}
	filtered := make([]database.RoomEnergyAssignment, 0)
	for _, assignment := range assignments {
		if assignment.RoomID == roomID {
			filtered = append(filtered, assignment)
		}
	}
	sort.SliceStable(filtered, func(left, right int) bool {
		if filtered[left].ParentAssignmentID != filtered[right].ParentAssignmentID {
			return filtered[left].ParentAssignmentID < filtered[right].ParentAssignmentID
		}
		return filtered[left].DisplayName < filtered[right].DisplayName
	})
	todayEnergy := assignmentEnergyMap(today)
	billingEnergy := assignmentEnergyMap(billing)
	roomTotal := decimal.Zero
	if billing != nil && billing.EnergyKWh != nil {
		roomTotal = *billing.EnergyKWh
	}
	nodes := make(map[int64]roomEnergySourceVM, len(filtered))
	childrenByParent := make(map[int64][]int64)
	for _, assignment := range filtered {
		ref := database.EnergySourceRef{DeviceID: assignment.DeviceID, ComponentKey: assignment.ComponentKey}
		active := assignment.Enabled && !assignment.EffectiveFrom.After(end) && (assignment.EffectiveTo == nil || assignment.EffectiveTo.After(end))
		state, found := live.Sources[ref]
		online := "No reading"
		quality := string(roomcore.QualityMissing)
		switch {
		case !active:
			state = roomEnergyLiveSource{}
			found = false
			online = "Historical · not collecting"
		case found && state.Stale:
			online, quality = "Stale", string(roomcore.QualityStale)
		case found && state.Online:
			online, quality = "Online", string(roomcore.QualityComplete)
		case found:
			online = "Offline"
		}
		billingValue := billingEnergy[assignment.ID]
		node := roomEnergySourceVM{
			AssignmentID: assignment.ID, DeviceID: assignment.DeviceID, DisplayName: assignment.DisplayName,
			DeviceName: assignment.DeviceName, DeviceModel: assignment.DeviceModel, ComponentKey: assignment.ComponentKey,
			Role: assignment.AggregationRole, ParentName: assignment.ParentDisplayName, CurrentPower: floatPowerText(state.PowerW),
			TodayEnergy: decimalText(todayEnergy[assignment.ID], 2, "kWh"), BillingEnergy: decimalText(billingValue, 2, "kWh"),
			OnlineState: online, Freshness: relativeReadingTime(state.UpdatedAt), Quality: quality,
		}
		if billingValue != nil && roomTotal.IsPositive() {
			share := billingValue.Div(roomTotal)
			node.RoomShare = percentageText(&share)
		}
		nodes[assignment.ID] = node
		if assignment.ParentAssignmentID != 0 {
			childrenByParent[assignment.ParentAssignmentID] = append(childrenByParent[assignment.ParentAssignmentID], assignment.ID)
		}
	}
	for parentID, childIDs := range childrenByParent {
		parent, exists := nodes[parentID]
		if !exists {
			continue
		}
		parentEnergy := billingEnergy[parentID]
		childrenEnergy := decimal.Zero
		for _, childID := range childIDs {
			child := nodes[childID]
			childValue := billingEnergy[childID]
			if childValue != nil {
				childrenEnergy = childrenEnergy.Add(*childValue)
				if parentEnergy != nil && parentEnergy.IsPositive() {
					share := childValue.Div(*parentEnergy)
					child.ParentShare = percentageText(&share)
				}
			}
			parent.Children = append(parent.Children, child)
		}
		if parentEnergy != nil {
			remainder := parentEnergy.Sub(childrenEnergy)
			if remainder.IsNegative() {
				parent.Overlap = true
				remainder = decimal.Zero
			}
			parent.OtherUsage = decimalValueText(remainder, 2, "kWh")
		}
		nodes[parentID] = parent
	}
	result := make([]roomEnergySourceVM, 0, len(filtered))
	for _, assignment := range filtered {
		if assignment.ParentAssignmentID != 0 {
			continue
		}
		result = append(result, nodes[assignment.ID])
	}
	return result, nil
}

func assignmentEnergyMap(series *energyservice.RoomEnergySeries) map[int64]*decimal.Decimal {
	result := map[int64]*decimal.Decimal{}
	if series == nil {
		return result
	}
	totals := map[int64]decimal.Decimal{}
	found := map[int64]bool{}
	for _, point := range series.Points {
		for _, source := range point.Sources {
			if !source.HasData {
				continue
			}
			totals[source.AssignmentID] = totals[source.AssignmentID].Add(source.EnergyKWh)
			found[source.AssignmentID] = true
		}
	}
	for assignmentID, value := range totals {
		if found[assignmentID] {
			copy := value
			result[assignmentID] = &copy
		}
	}
	return result
}

func (s *Server) buildDeviceEnergyView(r *http.Request, device database.Device) (deviceEnergyVM, error) {
	view := deviceEnergyVM{Supported: deviceHasEnergyCapability(device), InventoryRoomID: device.RoomID, InventoryRoomName: device.RoomName}
	if !view.Supported {
		return view, nil
	}
	config, err := s.store.GetEnergyConfig(r.Context())
	if err != nil {
		return deviceEnergyVM{}, err
	}
	now, location, err := roomEnergyNow(config)
	if err != nil {
		return deviceEnergyVM{}, err
	}
	assignments, err := s.store.ListRoomEnergyAssignments(r.Context())
	if err != nil {
		return deviceEnergyVM{}, err
	}
	var todayReport, billingReport energyservice.RoomEnergyReport
	if roomEnergyConfigured(config) {
		todayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, location)
		cycle, cycleErr := billingcycle.CycleContaining(now, config.BillingCycleStartDay, location)
		if cycleErr != nil {
			return deviceEnergyVM{}, cycleErr
		}
		todayReport, err = s.roomEnergyReport(r.Context(), config, todayStart, now, "hour")
		if err != nil {
			return deviceEnergyVM{}, err
		}
		billingReport, err = s.roomEnergyReport(r.Context(), config, cycle.Start, now, "day")
		if err != nil {
			return deviceEnergyVM{}, err
		}
	}
	live, err := s.roomEnergyLive(r.Context(), now, config)
	if err != nil {
		return deviceEnergyVM{}, err
	}
	room, roomErr := s.store.GetRoom(r.Context(), device.RoomID)
	ordinaryInventoryRoom := roomErr == nil && !room.IsSystem && !room.Archived
	for _, assignment := range assignments {
		if assignment.DeviceID != device.ID || assignment.EffectiveFrom.After(now) || (assignment.EffectiveTo != nil && !assignment.EffectiveTo.After(now)) {
			continue
		}
		ref := database.EnergySourceRef{DeviceID: assignment.DeviceID, ComponentKey: assignment.ComponentKey}
		view.Assignments = append(view.Assignments, roomEnergyDeviceAssignmentVM{
			ID: assignment.ID, RoomID: assignment.RoomID, DisplayName: assignment.DisplayName, RoomName: assignment.RoomName,
			Role: assignment.AggregationRole, ParentName: assignment.ParentDisplayName, CurrentPower: floatPowerText(live.Sources[ref].PowerW),
			TodayEnergy:         decimalText(assignmentEnergyMap(roomEnergySeries(todayReport, assignment.RoomID))[assignment.ID], 2, "kWh"),
			BillingEnergy:       decimalText(assignmentEnergyMap(roomEnergySeries(billingReport, assignment.RoomID))[assignment.ID], 2, "kWh"),
			CanUseInventoryRoom: ordinaryInventoryRoom && assignment.RoomID != device.RoomID,
		})
	}
	return view, nil
}
