package web

import (
	"net/http"
	"sort"
	"time"

	"github.com/filipegalo/shelly-energy-dashboard/internal/database"
	"github.com/shopspring/decimal"
)

func (s *Server) roomEnergySettingsPage(w http.ResponseWriter, r *http.Request) {
	view, err := s.buildRoomEnergySettings(r)
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	if r.URL.Query().Get("saved") == "1" {
		view.Message = "Room metering settings saved. Collection was reconfigured without changing historical attribution."
	}
	s.render(w, r, viewData{Page: "energy-room-settings", Title: "Room metering", RoomEnergy: &view})
}

func (s *Server) buildRoomEnergySettings(r *http.Request) (roomEnergyViewModel, error) {
	config, err := s.store.GetEnergyConfig(r.Context())
	if err != nil {
		return roomEnergyViewModel{}, err
	}
	roomConfig, err := s.store.GetRoomEnergyConfig(r.Context())
	if err != nil {
		return roomEnergyViewModel{}, err
	}
	rooms, err := s.store.ListRooms(r.Context())
	if err != nil {
		return roomEnergyViewModel{}, err
	}
	devices, err := s.store.ListDevices(r.Context(), database.DeviceFilter{})
	if err != nil {
		return roomEnergyViewModel{}, err
	}
	assignments, err := s.store.ListRoomEnergyAssignments(r.Context())
	if err != nil {
		return roomEnergyViewModel{}, err
	}
	now := time.Now()
	view := roomEnergyViewModel{Configured: roomEnergyConfigured(config), SelectedRange: "24h"}
	view.Settings.Config = roomEnergyConfigVM{
		RelativeTolerancePercent: decimal.NewFromFloat(roomConfig.RelativeTolerance).Mul(decimal.NewFromInt(100)).Round(2).String(),
		AbsoluteToleranceKWh:     decimal.NewFromFloat(roomConfig.AbsoluteToleranceWh).Div(decimal.NewFromInt(1000)).Round(3).String(),
		DiagnosticCollection:     roomConfig.DiagnosticCollection,
	}
	view.Settings.Rooms = make([]roomEnergyOptionVM, 0, len(rooms))
	for _, room := range rooms {
		if room.IsSystem || room.Archived {
			continue
		}
		view.Settings.Rooms = append(view.Settings.Rooms, roomEnergyOptionVM{ID: room.ID, Name: room.Name})
	}
	view.Settings.Devices = make([]roomEnergyDeviceOptionVM, 0, len(devices))
	for _, device := range devices {
		if !deviceHasEnergyCapability(device) {
			continue
		}
		view.Settings.Devices = append(view.Settings.Devices, roomEnergyDeviceOptionVM{ID: device.ID, Name: device.DisplayName(), Model: device.Model, IPAddress: device.IPAddress, OnlineStatus: device.OnlineStatus})
	}
	sort.SliceStable(view.Settings.Devices, func(left, right int) bool {
		return view.Settings.Devices[left].Name < view.Settings.Devices[right].Name
	})

	parentsByRoom := map[int64][]roomEnergyOptionVM{}
	for _, assignment := range assignments {
		if assignment.AggregationRole != database.RoomEnergyRolePrimary || !assignment.Enabled || assignment.EffectiveFrom.After(now) || (assignment.EffectiveTo != nil && !assignment.EffectiveTo.After(now)) {
			continue
		}
		name := assignment.DisplayName
		if name == "" {
			name = assignment.DeviceName + " · " + assignment.ComponentKey
		}
		parentsByRoom[assignment.RoomID] = append(parentsByRoom[assignment.RoomID], roomEnergyOptionVM{ID: assignment.ID, Name: name})
	}
	view.Settings.Assignments = make([]roomEnergyAssignmentVM, 0, len(assignments))
	for _, assignment := range assignments {
		state := assignmentState(assignment, now)
		parents := append([]roomEnergyOptionVM(nil), parentsByRoom[assignment.RoomID]...)
		filtered := parents[:0]
		for _, parent := range parents {
			if parent.ID != assignment.ID {
				filtered = append(filtered, parent)
			}
		}
		view.Settings.Assignments = append(view.Settings.Assignments, roomEnergyAssignmentVM{
			ID: assignment.ID, RoomID: assignment.RoomID, DeviceID: assignment.DeviceID, ParentAssignmentID: assignment.ParentAssignmentID,
			RoomName: assignment.RoomName, DeviceName: assignment.DeviceName, DeviceModel: assignment.DeviceModel, IPAddress: assignment.IPAddress,
			ComponentKey: assignment.ComponentKey, ComponentType: assignment.ComponentType, DisplayName: assignment.DisplayName,
			Role: assignment.AggregationRole, ParentName: assignment.ParentDisplayName, State: state, Enabled: assignment.Enabled,
			MainMeter: config.DeviceID == assignment.DeviceID && config.ComponentKey == assignment.ComponentKey,
			CanEdit:   state == "active", CanDisable: state == "active", CanDeleteFuture: state == "future",
			PollIntervalSeconds: assignment.PollIntervalSeconds, EffectiveFrom: assignment.EffectiveFrom, EffectiveTo: assignment.EffectiveTo, Parents: filtered,
		})
	}
	return view, nil
}

func deviceHasEnergyCapability(device database.Device) bool {
	for _, capability := range device.Capabilities() {
		if capability == "power meters" {
			return true
		}
	}
	return false
}
