package web

import (
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/filipegalo/shelly-energy-dashboard/internal/database"
	"github.com/filipegalo/shelly-energy-dashboard/internal/shelly"
	"github.com/go-chi/chi/v5"
)

func (s *Server) discoverRoomEnergyComponents(w http.ResponseWriter, r *http.Request) {
	deviceID, err := parsePositiveFormID(r.FormValue("device_id"), "device")
	if routeID := strings.TrimSpace(chi.URLParam(r, "deviceID")); routeID != "" {
		deviceID, err = parsePositiveFormID(routeID, "device")
	}
	discovery := roomEnergyDiscoveryVM{DeviceID: deviceID}
	if err != nil {
		discovery.Error = err.Error()
	} else if s.roomEnergy == nil {
		discovery.Error = "energy collector is unavailable"
	} else {
		device, components, discoverErr := s.roomEnergy.DiscoverMeter(r.Context(), deviceID)
		discovery.DeviceName = device.DisplayName()
		if discoverErr != nil {
			discovery.Error = "Component discovery failed: " + discoverErr.Error()
		} else {
			discovery.Components = make([]roomEnergyComponentVM, 0, len(components))
			for _, component := range components {
				if !component.Capabilities.Usable() {
					continue
				}
				discovery.Components = append(discovery.Components, roomEnergyComponentVM{Key: component.Key, Type: component.Type, Label: component.Label})
			}
		}
	}
	view, viewErr := s.buildRoomEnergySettings(r)
	if viewErr != nil {
		s.serverError(w, r, viewErr)
		return
	}
	view.Settings.Discovery = discoveryWithAssignments(discovery, view.Settings.Assignments)
	if r.Header.Get("HX-Request") == "true" {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if executeErr := s.views.ExecuteTemplate(w, "room-energy-discovery", viewData{CSRF: csrfToken(r), RoomEnergy: &view}); executeErr != nil {
			s.serverError(w, r, executeErr)
		}
		return
	}
	s.render(w, r, viewData{Page: "energy-room-settings", Title: "Room metering", RoomEnergy: &view})
}

func discoveryWithAssignments(discovery roomEnergyDiscoveryVM, assignments []roomEnergyAssignmentVM) roomEnergyDiscoveryVM {
	result := discovery
	result.Components = make([]roomEnergyComponentVM, 0, len(discovery.Components))
	for _, component := range discovery.Components {
		enriched := component
		enriched.Assignments = make([]roomEnergyAssignmentVM, 0)
		for _, assignment := range assignments {
			if assignment.DeviceID != discovery.DeviceID || assignment.ComponentKey != component.Key || assignment.State == "historical" {
				continue
			}
			enriched.Assignments = append(enriched.Assignments, assignment)
		}
		result.Components = append(result.Components, enriched)
	}
	return result
}

func (s *Server) testRoomEnergyComponent(w http.ResponseWriter, r *http.Request) {
	deviceID, deviceErr := parsePositiveFormID(r.FormValue("device_id"), "device")
	componentKey := strings.TrimSpace(r.FormValue("component_key"))
	result := s.testRoomEnergySource(r, deviceID, componentKey, deviceErr)
	s.renderRoomEnergyTestResult(w, r, result)
}

func (s *Server) testExistingRoomEnergyAssignment(w http.ResponseWriter, r *http.Request) {
	assignmentID, err := assignmentIDParam(r)
	if err != nil {
		s.formError(w, r, err)
		return
	}
	assignment, err := s.store.GetRoomEnergyAssignment(r.Context(), assignmentID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			s.notFound(w, r)
			return
		}
		s.serverError(w, r, err)
		return
	}
	result := s.testRoomEnergySource(r, assignment.DeviceID, assignment.ComponentKey, nil)
	s.renderRoomEnergyTestResult(w, r, result)
}

func (s *Server) testRoomEnergySource(r *http.Request, deviceID int64, componentKey string, validationErr error) roomEnergyTestVM {
	result := roomEnergyTestVM{Attempted: true, DeviceID: deviceID, ComponentKey: componentKey}
	if validationErr != nil {
		result.Error = validationErr.Error()
		return result
	}
	if componentKey == "" || len(componentKey) > 64 {
		result.Error = "select a valid discovered energy component"
		return result
	}
	if s.roomEnergy == nil {
		result.Error = "energy collector is unavailable"
		return result
	}
	_, component, reading, err := s.roomEnergy.TestMeter(r.Context(), deviceID, componentKey)
	if err != nil {
		result.Error = "Component test failed: " + err.Error()
		return result
	}
	if component.Key != componentKey || !component.Capabilities.Usable() {
		result.Error = "the tested response did not match the selected component"
		return result
	}
	config, _ := s.store.GetEnergyConfig(r.Context())
	result.Available = true
	result.Label = component.Label
	result.CumulativeAvailable = component.Capabilities.ImportedEnergyWh && reading.ImportedEnergyWh != nil
	result.PowerOnly = component.Capabilities.PowerW && !result.CumulativeAvailable
	result.MainMeter = config.DeviceID == deviceID && config.ComponentKey == componentKey
	result.CurrentPower = floatPowerText(reading.PowerW)
	result.ImportedEnergy = floatMeasurementText(reading.ImportedEnergyWh, 1000, "kWh")
	result.Voltage = floatMeasurementText(reading.VoltageV, 1, "V")
	result.LastResponse = relativeReadingTime(reading.Timestamp)
	return result
}

func (s *Server) renderRoomEnergyTestResult(w http.ResponseWriter, r *http.Request, result roomEnergyTestVM) {
	if r.Header.Get("HX-Request") == "true" {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if err := s.views.ExecuteTemplate(w, "room-energy-test-result", viewData{CSRF: csrfToken(r), RoomEnergy: &roomEnergyViewModel{Settings: roomEnergySettingsVM{Test: result}}}); err != nil {
			s.serverError(w, r, err)
		}
		return
	}
	view, err := s.buildRoomEnergySettings(r)
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	view.Settings.Test = result
	s.render(w, r, viewData{Page: "energy-room-settings", Title: "Room metering", RoomEnergy: &view})
}

func floatMeasurementText(value *float64, divisor float64, unit string) string {
	if value == nil {
		return "—"
	}
	return fmt.Sprintf("%.2f %s", *value/divisor, unit)
}

func (s *Server) createRoomEnergyAssignment(w http.ResponseWriter, r *http.Request) {
	roomID, err := parsePositiveFormID(r.FormValue("room_id"), "room")
	if err != nil {
		s.formError(w, r, err)
		return
	}
	deviceID, err := parsePositiveFormID(r.FormValue("device_id"), "device")
	if err != nil {
		s.formError(w, r, err)
		return
	}
	componentKey := strings.TrimSpace(r.FormValue("component_key"))
	component, err := s.validatedRoomEnergyComponent(r, deviceID, componentKey)
	if err != nil {
		s.formError(w, r, err)
		return
	}
	config, err := s.store.GetEnergyConfig(r.Context())
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	effective, err := roomAssignmentEffectiveTime(r.FormValue("effective_preset"), r.FormValue("effective_at"), config, time.Now())
	if err != nil {
		s.formError(w, r, err)
		return
	}
	poll, err := formIntDefault(r, "poll_interval_seconds", max(10, config.PollIntervalSeconds))
	if err != nil {
		s.formError(w, r, err)
		return
	}
	parentID, err := optionalPositiveID(r.FormValue("parent_assignment_id"), "parent source")
	if err != nil {
		s.formError(w, r, err)
		return
	}
	displayName := strings.TrimSpace(r.FormValue("display_name"))
	if displayName == "" {
		displayName = strings.TrimSpace(component.Label)
	}
	if displayName == "" {
		displayName = component.Key
	}
	if len(displayName) > 100 {
		s.formError(w, r, errors.New("source display name is too long"))
		return
	}
	created, err := s.store.CreateRoomEnergyAssignment(r.Context(), database.RoomEnergyAssignment{
		RoomID: roomID, DeviceID: deviceID, ComponentKey: component.Key, ComponentType: component.Type,
		DisplayName: displayName, Enabled: formBool(r, "enabled"), AggregationRole: strings.TrimSpace(r.FormValue("aggregation_role")),
		ParentAssignmentID: parentID, PollIntervalSeconds: poll, MainMeterAcknowledged: formBool(r, "main_meter_acknowledged"), EffectiveFrom: effective,
	})
	if err != nil {
		s.formError(w, r, roomEnergyAssignmentError(err))
		return
	}
	if err := s.reconfigureRoomEnergy(r); err != nil {
		s.formError(w, r, err)
		return
	}
	s.logger.Info("room energy assignment created", "room_id", created.RoomID, "device_id", created.DeviceID, "component_key", created.ComponentKey, "assignment_id", created.ID, "operation", "create")
	s.redirect(w, r, "/energy/rooms/settings?saved=1")
}

func (s *Server) reviseRoomEnergyAssignment(w http.ResponseWriter, r *http.Request) {
	assignmentID, err := assignmentIDParam(r)
	if err != nil {
		s.formError(w, r, err)
		return
	}
	original, err := s.store.GetRoomEnergyAssignment(r.Context(), assignmentID)
	if err != nil {
		s.assignmentLookupError(w, r, err)
		return
	}
	config, err := s.store.GetEnergyConfig(r.Context())
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	revision, err := roomEnergyRevisionFromForm(r, original, config)
	if err != nil {
		s.formError(w, r, err)
		return
	}
	created, err := s.store.ReviseRoomEnergyAssignment(r.Context(), assignmentID, revision)
	if err != nil {
		s.formError(w, r, roomEnergyAssignmentError(err))
		return
	}
	if err := s.reconfigureRoomEnergy(r); err != nil {
		s.formError(w, r, err)
		return
	}
	s.logger.Info("room energy assignment revised", "room_id", created.RoomID, "device_id", created.DeviceID, "component_key", created.ComponentKey, "assignment_id", created.ID, "operation", "revise")
	s.redirect(w, r, "/energy/rooms/settings?saved=1")
}

func roomEnergyRevisionFromForm(r *http.Request, original database.RoomEnergyAssignment, config database.EnergyConfig) (database.RoomEnergyAssignmentRevision, error) {
	if err := r.ParseForm(); err != nil {
		return database.RoomEnergyAssignmentRevision{}, errors.New("invalid assignment form")
	}
	roomID := original.RoomID
	if r.Form.Has("room_id") {
		parsed, err := parsePositiveFormID(r.FormValue("room_id"), "room")
		if err != nil {
			return database.RoomEnergyAssignmentRevision{}, err
		}
		roomID = parsed
	}
	displayName := original.DisplayName
	if r.Form.Has("display_name") {
		displayName = strings.TrimSpace(r.FormValue("display_name"))
		if len(displayName) > 100 {
			return database.RoomEnergyAssignmentRevision{}, errors.New("source display name is too long")
		}
	}
	role := original.AggregationRole
	if r.Form.Has("aggregation_role") {
		role = strings.TrimSpace(r.FormValue("aggregation_role"))
	}
	parentID := original.ParentAssignmentID
	if r.Form.Has("parent_assignment_id") {
		parsed, err := optionalPositiveID(r.FormValue("parent_assignment_id"), "parent source")
		if err != nil {
			return database.RoomEnergyAssignmentRevision{}, err
		}
		parentID = parsed
	}
	poll := original.PollIntervalSeconds
	if r.Form.Has("poll_interval_seconds") {
		parsed, err := formIntDefault(r, "poll_interval_seconds", poll)
		if err != nil {
			return database.RoomEnergyAssignmentRevision{}, err
		}
		poll = parsed
	}
	enabled := original.Enabled
	if r.Form.Has("enabled_present") {
		enabled = formBool(r, "enabled")
	}
	effective, err := roomAssignmentEffectiveTime(r.FormValue("effective_preset"), r.FormValue("effective_at"), config, time.Now())
	if err != nil {
		return database.RoomEnergyAssignmentRevision{}, err
	}
	return database.RoomEnergyAssignmentRevision{RoomID: roomID, DisplayName: displayName, Enabled: enabled, AggregationRole: role, ParentAssignmentID: parentID, PollIntervalSeconds: poll, MainMeterAcknowledged: original.MainMeterAcknowledged || formBool(r, "main_meter_acknowledged"), EffectiveFrom: effective}, nil
}

func (s *Server) moveRoomEnergyAssignment(w http.ResponseWriter, r *http.Request) {
	assignmentID, err := assignmentIDParam(r)
	if err != nil {
		s.formError(w, r, err)
		return
	}
	_, err = s.store.GetRoomEnergyAssignment(r.Context(), assignmentID)
	if err != nil {
		s.assignmentLookupError(w, r, err)
		return
	}
	roomID, err := parsePositiveFormID(r.FormValue("room_id"), "room")
	if err != nil {
		s.formError(w, r, err)
		return
	}
	config, err := s.store.GetEnergyConfig(r.Context())
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	effective, err := roomAssignmentEffectiveTime(r.FormValue("effective_preset"), r.FormValue("effective_at"), config, time.Now())
	if err != nil {
		s.formError(w, r, err)
		return
	}
	moved, err := s.store.MoveRoomEnergyAssignment(r.Context(), assignmentID, roomID, effective)
	if err != nil {
		s.formError(w, r, roomEnergyAssignmentError(err))
		return
	}
	if err := s.reconfigureRoomEnergy(r); err != nil {
		s.formError(w, r, err)
		return
	}
	s.logger.Info("room energy assignment moved", "room_id", moved.RoomID, "device_id", moved.DeviceID, "component_key", moved.ComponentKey, "assignment_id", moved.ID, "operation", "move")
	s.redirect(w, r, "/energy/rooms/settings?saved=1")
}

func (s *Server) disableRoomEnergyAssignment(w http.ResponseWriter, r *http.Request) {
	assignmentID, err := assignmentIDParam(r)
	if err != nil {
		s.formError(w, r, err)
		return
	}
	assignment, err := s.store.GetRoomEnergyAssignment(r.Context(), assignmentID)
	if err != nil {
		s.assignmentLookupError(w, r, err)
		return
	}
	config, err := s.store.GetEnergyConfig(r.Context())
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	effective, err := roomAssignmentEffectiveTime(r.FormValue("effective_preset"), r.FormValue("effective_at"), config, time.Now())
	if err != nil {
		s.formError(w, r, err)
		return
	}
	if err := s.store.CloseRoomEnergyAssignment(r.Context(), assignmentID, effective); err != nil {
		s.formError(w, r, roomEnergyAssignmentError(err))
		return
	}
	if err := s.reconfigureRoomEnergy(r); err != nil {
		s.formError(w, r, err)
		return
	}
	s.logger.Info("room energy assignment disabled", "room_id", assignment.RoomID, "device_id", assignment.DeviceID, "component_key", assignment.ComponentKey, "assignment_id", assignment.ID, "operation", "disable")
	s.redirect(w, r, "/energy/rooms/settings?saved=1")
}

func (s *Server) deleteFutureRoomEnergyAssignment(w http.ResponseWriter, r *http.Request) {
	assignmentID, err := assignmentIDParam(r)
	if err != nil {
		s.formError(w, r, err)
		return
	}
	if err := s.store.DeleteFutureRoomEnergyAssignment(r.Context(), assignmentID, time.Now()); err != nil {
		s.formError(w, r, err)
		return
	}
	if err := s.reconfigureRoomEnergy(r); err != nil {
		s.formError(w, r, err)
		return
	}
	s.logger.Info("future room energy assignment removed", "assignment_id", assignmentID, "operation", "delete_future")
	s.redirect(w, r, "/energy/rooms/settings?saved=1")
}

func (s *Server) saveRoomEnergyReconciliationSettings(w http.ResponseWriter, r *http.Request) {
	relative, err := strconv.ParseFloat(strings.TrimSpace(r.FormValue("relative_tolerance_percent")), 64)
	if err != nil {
		s.formError(w, r, errors.New("relative tolerance must be a percentage"))
		return
	}
	absolute, err := strconv.ParseFloat(strings.TrimSpace(r.FormValue("absolute_tolerance_kwh")), 64)
	if err != nil {
		s.formError(w, r, errors.New("absolute tolerance must be measured in kWh"))
		return
	}
	saved, err := s.store.SaveRoomEnergyConfig(r.Context(), database.RoomEnergyConfig{RelativeTolerance: relative / 100, AbsoluteToleranceWh: absolute * 1000, DiagnosticCollection: formBool(r, "diagnostic_collection")})
	if err != nil {
		s.formError(w, r, err)
		return
	}
	if err := s.reconfigureRoomEnergy(r); err != nil {
		s.formError(w, r, err)
		return
	}
	s.logger.Info("room energy reconciliation settings saved", "operation", "save_tolerance", "relative_tolerance", saved.RelativeTolerance, "absolute_tolerance_wh", saved.AbsoluteToleranceWh)
	s.redirect(w, r, "/energy/rooms/reconciliation?saved=1")
}

func (s *Server) useInventoryRoomForEnergy(w http.ResponseWriter, r *http.Request) {
	deviceID := idParam(r)
	assignmentID, err := parsePositiveFormID(r.FormValue("assignment_id"), "assignment")
	if err != nil {
		s.formError(w, r, err)
		return
	}
	device, err := s.store.GetDevice(r.Context(), deviceID)
	if err != nil {
		s.assignmentLookupError(w, r, err)
		return
	}
	room, err := s.store.GetRoom(r.Context(), device.RoomID)
	if err != nil || room.IsSystem || room.Archived {
		s.formError(w, r, errors.New("assign the device to an ordinary inventory room first"))
		return
	}
	assignment, err := s.store.GetRoomEnergyAssignment(r.Context(), assignmentID)
	if err != nil || assignment.DeviceID != deviceID {
		s.formError(w, r, errors.New("select an energy assignment belonging to this device"))
		return
	}
	config, err := s.store.GetEnergyConfig(r.Context())
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	effective, err := roomAssignmentEffectiveTime(r.FormValue("effective_preset"), r.FormValue("effective_at"), config, time.Now())
	if err != nil {
		s.formError(w, r, err)
		return
	}
	if _, err := s.store.MoveRoomEnergyAssignment(r.Context(), assignmentID, room.ID, effective); err != nil {
		s.formError(w, r, roomEnergyAssignmentError(err))
		return
	}
	if err := s.reconfigureRoomEnergy(r); err != nil {
		s.formError(w, r, err)
		return
	}
	s.redirect(w, r, fmt.Sprintf("/devices/%d", deviceID))
}

func (s *Server) validatedRoomEnergyComponent(r *http.Request, deviceID int64, componentKey string) (shelly.EnergyComponent, error) {
	componentKey = strings.TrimSpace(componentKey)
	if deviceID < 1 || componentKey == "" || len(componentKey) > 64 {
		return shelly.EnergyComponent{}, errors.New("select a valid discovered energy component")
	}
	if s.roomEnergy == nil {
		return shelly.EnergyComponent{}, errors.New("energy collector is unavailable")
	}
	_, component, _, err := s.roomEnergy.TestMeter(r.Context(), deviceID, componentKey)
	if err != nil {
		return shelly.EnergyComponent{}, fmt.Errorf("component validation failed: %w", err)
	}
	if component.Key != componentKey || component.Type == "" || !component.Capabilities.Usable() {
		return shelly.EnergyComponent{}, errors.New("the tested response did not match a usable discovered component")
	}
	if kind, _, _, parseErr := shelly.ParseEnergyComponentKey(component.Key); parseErr != nil || kind != component.Type {
		return shelly.EnergyComponent{}, errors.New("the tested component identity is invalid")
	}
	return component, nil
}

func optionalPositiveID(raw, label string) (int64, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" || raw == "0" {
		return 0, nil
	}
	return parsePositiveFormID(raw, label)
}

func (s *Server) reconfigureRoomEnergy(r *http.Request) error {
	if s.roomEnergy == nil {
		return nil
	}
	if err := s.roomEnergy.Reconfigure(r.Context()); err != nil {
		return fmt.Errorf("assignment saved but collector reconfiguration failed: %w", err)
	}
	return nil
}

func (s *Server) assignmentLookupError(w http.ResponseWriter, r *http.Request, err error) {
	if errors.Is(err, sql.ErrNoRows) {
		s.notFound(w, r)
		return
	}
	s.serverError(w, r, err)
}

func roomEnergyAssignmentError(err error) error {
	switch {
	case errors.Is(err, database.ErrRoomEnergyAssignmentOverlap):
		return errors.New("this device component already has an overlapping room assignment")
	case errors.Is(err, database.ErrRoomEnergyParentRequired):
		return errors.New("select a primary source in the same room as the submeter parent")
	case errors.Is(err, database.ErrRoomEnergyInvalidHierarchy):
		return errors.New("the selected parent or room would create an invalid meter hierarchy")
	case errors.Is(err, database.ErrRoomEnergyMainMeterConfirmation):
		return errors.New("confirm that assigning the whole-house main meter to a room can invalidate reconciliation")
	case errors.Is(err, database.ErrRoomEnergyInvalidAssignment):
		return errors.New("the room energy assignment or effective date is invalid")
	default:
		return err
	}
}
