package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"sync"
	"sync/atomic"
	"time"
)

const (
	pollIntervalSeconds    = 10
	readingsPerDay         = 24 * 60 * 60 / pollIntervalSeconds
	wholeHouseDailyKWh     = 30.0
	kitchenDailyKWh        = 10.0
	livingRoomDailyKWh     = 5.0
	officeDailyKWh         = 4.0
	garageDailyKWh         = 2.0
	unmeteredDailyKWh      = 9.0
	dishwasherDailyKWh     = 3.0
	refrigeratorDailyKWh   = 2.0
	defaultInitialEnergyWh = 100000.0
)

type componentFixture struct {
	Kind       string
	ID         int
	DailyKWh   float64
	InitialWh  float64
	ThreePhase bool
	ResetAfter uint64
}

type mockProfile struct {
	ID              string
	Model           string
	App             string
	MAC             string
	Name            string
	Components      []componentFixture
	DisconnectAfter uint64
	StaleAfter      uint64
}

type mockState struct {
	profile           string
	fixture           *mockProfile
	legacyEnergyReads atomic.Uint64
	fixtureReads      atomic.Uint64
	mu                sync.Mutex
	componentSteps    map[string]uint64
}

var deterministicRoomProfiles = buildRoomProfiles()

func buildRoomProfiles() map[string]mockProfile {
	profiles := map[string]mockProfile{
		"room-main3em": roomProfile("shellypro3em-room-main", "SPEM-003CEBEU", "Pro3EM", "AABBCCDDEE16", "Whole-house 30 kWh/day", []componentFixture{
			energyFixture("em", 0, wholeHouseDailyKWh, true),
		}),
		"room-pro4pm": roomProfile("shellypro4pm-room-circuits", "SPSW-104PE16EU", "Pro4PM", "AABBCCDDEE17", "Room circuits 21 kWh/day", baselineRoomComponents()),
		"room-em2": roomProfile("shellyproem50-room-two-channel", "SPEM-002CEBEU", "ProEM50", "AABBCCDDEE18", "Two-channel room meter", []componentFixture{
			energyFixture("em1", 0, kitchenDailyKWh, false),
			energyFixture("em1", 1, livingRoomDailyKWh, false),
		}),
		"room-plug-dishwasher":       roomProfile("shellyplug-room-dishwasher", "SNPL-00112EU", "PlusPlugS", "AABBCCDDEE19", "Dishwasher submeter 3 kWh/day", []componentFixture{energyFixture("switch", 0, dishwasherDailyKWh, false)}),
		"room-plug-refrigerator":     roomProfile("shellyplug-room-refrigerator", "SNPL-00112EU", "PlusPlugS", "AABBCCDDEE20", "Refrigerator submeter 2 kWh/day", []componentFixture{energyFixture("switch", 0, refrigeratorDailyKWh, false)}),
		"room-plug-living":           roomProfile("shellyplug-room-living", "SNPL-00112EU", "PlusPlugS", "AABBCCDDEE21", "Living Room plug 5 kWh/day", []componentFixture{energyFixture("switch", 0, livingRoomDailyKWh, false)}),
		"room-plug-office":           roomProfile("shellyplug-room-office", "SNPL-00112EU", "PlusPlugS", "AABBCCDDEE22", "Office plug 4 kWh/day", []componentFixture{energyFixture("switch", 0, officeDailyKWh, false)}),
		"room-plug-garage":           roomProfile("shellyplug-room-garage", "SNPL-00112EU", "PlusPlugS", "AABBCCDDEE23", "Garage plug 2 kWh/day", []componentFixture{energyFixture("switch", 0, garageDailyKWh, false)}),
		"room-source-offline":        withDisconnect(roomProfile("shellyplug-room-offline", "SNPL-00112EU", "PlusPlugS", "AABBCCDDEE24", "Room source goes offline", []componentFixture{energyFixture("switch", 0, 1, false)}), 2),
		"room-source-stale":          withStale(roomProfile("shellyplug-room-stale", "SNPL-00112EU", "PlusPlugS", "AABBCCDDEE25", "Room source leaves a stale reading", []componentFixture{energyFixture("switch", 0, 1, false)}), 2),
		"room-source-reset":          roomProfile("shellyplug-room-reset", "SNPL-00112EU", "PlusPlugS", "AABBCCDDEE26", "Room source counter reset", []componentFixture{resetFixture("switch", 0, 1, 3)}),
		"reconcile-below":            roomProfile("shellypro4pm-reconcile-below", "SPSW-104PE16EU", "Pro4PM", "AABBCCDDEE27", "Rooms below main meter: 21 vs 30 kWh/day", baselineRoomComponents()),
		"reconcile-slight-over":      roomProfile("shellypro4pm-reconcile-slight", "SPSW-104PE16EU", "Pro4PM", "AABBCCDDEE28", "Rooms slightly above main meter: 30.6 vs 30 kWh/day", scaledRoomComponents(30.6)),
		"reconcile-significant-over": roomProfile("shellypro4pm-reconcile-high", "SPSW-104PE16EU", "Pro4PM", "AABBCCDDEE29", "Rooms significantly above main meter: 36 vs 30 kWh/day", scaledRoomComponents(36)),
	}
	return profiles
}

func roomProfile(id, model, app, mac, name string, components []componentFixture) mockProfile {
	return mockProfile{ID: id, Model: model, App: app, MAC: mac, Name: name, Components: append([]componentFixture(nil), components...)}
}

func withDisconnect(profile mockProfile, after uint64) mockProfile {
	profile.DisconnectAfter = after
	return profile
}

func withStale(profile mockProfile, after uint64) mockProfile {
	profile.StaleAfter = after
	return profile
}

func energyFixture(kind string, id int, dailyKWh float64, threePhase bool) componentFixture {
	return componentFixture{Kind: kind, ID: id, DailyKWh: dailyKWh, InitialWh: defaultInitialEnergyWh + float64(id)*10000, ThreePhase: threePhase}
}

func resetFixture(kind string, id int, dailyKWh float64, after uint64) componentFixture {
	fixture := energyFixture(kind, id, dailyKWh, false)
	fixture.ResetAfter = after
	return fixture
}

func baselineRoomComponents() []componentFixture {
	return []componentFixture{
		energyFixture("switch", 0, kitchenDailyKWh, false),
		energyFixture("switch", 1, livingRoomDailyKWh, false),
		energyFixture("switch", 2, officeDailyKWh, false),
		energyFixture("switch", 3, garageDailyKWh, false),
	}
}

func scaledRoomComponents(totalDailyKWh float64) []componentFixture {
	scale := totalDailyKWh / (kitchenDailyKWh + livingRoomDailyKWh + officeDailyKWh + garageDailyKWh)
	return []componentFixture{
		energyFixture("switch", 0, kitchenDailyKWh*scale, false),
		energyFixture("switch", 1, livingRoomDailyKWh*scale, false),
		energyFixture("switch", 2, officeDailyKWh*scale, false),
		energyFixture("switch", 3, garageDailyKWh*scale, false),
	}
}

func newMockHandler(profile string) http.Handler {
	state := &mockState{profile: profile, componentSteps: map[string]uint64{}}
	if fixture, ok := deterministicRoomProfiles[profile]; ok {
		copy := fixture
		state.fixture = &copy
	}
	mux := http.NewServeMux()
	state.register(mux)
	return mux
}

func (s *mockState) register(mux *http.ServeMux) {
	mux.HandleFunc("/shelly", s.shelly)
	mux.HandleFunc("/settings", s.settings)
	mux.HandleFunc("/status", s.status)
	mux.HandleFunc("/rpc/Sys.GetConfig", s.sysGetConfig)
	mux.HandleFunc("/rpc/Shelly.GetDeviceInfo", s.shellyGetDeviceInfo)
	mux.HandleFunc("/rpc/Shelly.GetStatus", s.shellyGetStatus)
	mux.HandleFunc("/rpc/Switch.GetStatus", s.switchGetStatus)
	mux.HandleFunc("/rpc/EM.GetStatus", s.emGetStatus)
	mux.HandleFunc("/rpc/EMData.GetStatus", s.emDataGetStatus)
	mux.HandleFunc("/rpc/EM1.GetStatus", s.em1GetStatus)
	mux.HandleFunc("/rpc/EM1Data.GetStatus", s.em1DataGetStatus)
	mux.HandleFunc("/rpc/PM.GetStatus", s.pmGetStatus)
	mux.HandleFunc("/rpc/PMData.GetStatus", s.pmDataGetStatus)
	mux.HandleFunc("/rpc/Sys.GetStatus", s.sysGetStatus)
}

func (s *mockState) shelly(w http.ResponseWriter, r *http.Request) {
	if !allowGET(w, r) {
		return
	}
	if s.profile == "offline" || (s.profile == "energy-flaky" && s.legacyEnergyReads.Load() >= 2) || (s.fixture != nil && s.fixture.DisconnectAfter > 0 && s.fixtureReads.Load() >= s.fixture.DisconnectAfter) {
		disconnect(w)
		return
	}
	if s.profile == "slow" {
		time.Sleep(5 * time.Second)
	}
	if s.profile == "malformed" {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":`))
		return
	}
	if s.fixture != nil {
		writeJSON(w, map[string]any{"id": s.fixture.ID, "model": s.fixture.Model, "gen": 2, "mac": s.fixture.MAC, "ver": "1.4.4", "app": s.fixture.App})
		return
	}
	switch s.profile {
	case "gen1":
		writeJSON(w, map[string]any{"type": "SHSW-1", "mac": "AABBCCDDEE01", "auth": false, "fw": "20230913-112003/v1.14.0"})
	case "gen3":
		writeJSON(w, map[string]any{"id": "shelly1g3-mock03", "model": "S3SW-001X16EU", "gen": 3, "mac": "AABBCCDDEE03", "ver": "1.5.0", "app": "Switch"})
	case "auth":
		writeJSON(w, map[string]any{"id": "shellyplus1-auth", "model": "SNSW-001X16EU", "gen": 2, "mac": "AABBCCDDEE04", "ver": "1.4.4", "auth_en": true})
	case "ipchange":
		writeJSON(w, map[string]any{"id": "shellyplus1-moving", "model": "SNSW-001X16EU", "gen": 2, "mac": "AABBCCDDEE09", "ver": "1.4.4"})
	case "energy", "energy-reset", "energy-invalid", "energy-slow", "energy-flaky":
		id, mac := energyIdentity(s.profile)
		writeJSON(w, map[string]any{"id": id, "model": "SPEM-003CEBEU", "gen": 2, "mac": mac, "ver": "1.4.4", "app": "Pro3EM"})
	default:
		writeJSON(w, map[string]any{"id": "shellyplus1-mock02", "model": "SNSW-001X16EU", "gen": 2, "mac": "AABBCCDDEE02", "ver": "1.4.4", "app": "Plus1"})
	}
}

func (s *mockState) settings(w http.ResponseWriter, r *http.Request) {
	if !allowGET(w, r) {
		return
	}
	writeJSON(w, map[string]any{"name": "Mock Gen1", "device": map[string]any{"hostname": "shelly1-mock01", "type": "SHSW-1", "mac": "AABBCCDDEE01"}})
}

func (s *mockState) status(w http.ResponseWriter, r *http.Request) {
	if !allowGET(w, r) {
		return
	}
	writeJSON(w, map[string]any{"relays": []any{map[string]any{"ison": true}}, "meters": []any{map[string]any{"power": 12.3}}})
}

func (s *mockState) sysGetConfig(w http.ResponseWriter, r *http.Request) {
	if !allowGET(w, r) || s.requireDigest(w, r) {
		return
	}
	if s.profile == "unsupported" {
		http.Error(w, "method not found", http.StatusNotFound)
		return
	}
	name := "Mock " + s.profile
	if s.fixture != nil {
		name = s.fixture.Name
	}
	writeJSON(w, map[string]any{"device": map[string]any{"name": name}})
}

func (s *mockState) shellyGetDeviceInfo(w http.ResponseWriter, r *http.Request) {
	if !allowGET(w, r) || s.requireDigest(w, r) {
		return
	}
	if s.profile == "unsupported" {
		http.Error(w, "method not found", http.StatusNotFound)
		return
	}
	name := "Mock " + s.profile
	if s.fixture != nil {
		name = s.fixture.Name
	}
	writeJSON(w, map[string]any{"name": name})
}

func (s *mockState) shellyGetStatus(w http.ResponseWriter, r *http.Request) {
	if !allowGET(w, r) || s.requireDigest(w, r) {
		return
	}
	if s.fixture != nil {
		payload := map[string]any{"sys": map[string]any{"uptime": 1234}}
		for _, fixture := range s.fixture.Components {
			step := s.componentStep(fixture, false)
			payload[componentKey(fixture.Kind, fixture.ID)] = livePayload(fixture, step)
			if dataKey := companionKey(fixture.Kind, fixture.ID); dataKey != "" {
				payload[dataKey] = dataPayload(fixture, step)
			}
		}
		writeJSON(w, payload)
		return
	}
	if isEnergyProfile(s.profile) {
		step := s.legacyEnergyReads.Load()
		writeJSON(w, map[string]any{"sys": map[string]any{"uptime": 1234}, "em:0": energyLiveStatus(s.profile, step), "emdata:0": energyDataStatus(s.profile, step)})
		return
	}
	writeJSON(w, map[string]any{"sys": map[string]any{"uptime": 1234}, "switch:0": map[string]any{"output": true, "apower": 8.2, "voltage": 230.1}, "input:0": map[string]any{"state": false}})
}

func (s *mockState) switchGetStatus(w http.ResponseWriter, r *http.Request) {
	if s.fixture != nil {
		s.fixtureComponent(w, r, "switch", false, true)
		return
	}
	http.NotFound(w, r)
}

func (s *mockState) emGetStatus(w http.ResponseWriter, r *http.Request) {
	if s.fixture != nil {
		s.fixtureComponent(w, r, "em", false, false)
		return
	}
	if !allowGET(w, r) || s.requireDigest(w, r) {
		return
	}
	if s.profile == "energy-slow" {
		time.Sleep(5 * time.Second)
	}
	writeJSON(w, energyLiveStatus(s.profile, s.legacyEnergyReads.Load()))
}

func (s *mockState) emDataGetStatus(w http.ResponseWriter, r *http.Request) {
	if s.fixture != nil {
		s.fixtureComponent(w, r, "em", true, true)
		return
	}
	if !allowGET(w, r) || s.requireDigest(w, r) {
		return
	}
	step := s.legacyEnergyReads.Add(1)
	writeJSON(w, energyDataStatus(s.profile, step))
}

func (s *mockState) em1GetStatus(w http.ResponseWriter, r *http.Request) {
	s.fixtureComponent(w, r, "em1", false, false)
}

func (s *mockState) em1DataGetStatus(w http.ResponseWriter, r *http.Request) {
	s.fixtureComponent(w, r, "em1", true, true)
}

func (s *mockState) pmGetStatus(w http.ResponseWriter, r *http.Request) {
	s.fixtureComponent(w, r, "pm", false, false)
}

func (s *mockState) pmDataGetStatus(w http.ResponseWriter, r *http.Request) {
	s.fixtureComponent(w, r, "pm", true, true)
}

func (s *mockState) sysGetStatus(w http.ResponseWriter, r *http.Request) {
	if !allowGET(w, r) || s.requireDigest(w, r) {
		return
	}
	writeJSON(w, map[string]any{"uptime": 1234})
}

func (s *mockState) fixtureComponent(w http.ResponseWriter, r *http.Request, kind string, data, advance bool) {
	if !allowGET(w, r) || s.requireDigest(w, r) {
		return
	}
	if s.fixture == nil {
		http.NotFound(w, r)
		return
	}
	id, err := requestedComponentID(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	fixture, ok := s.findFixture(kind, id)
	if !ok {
		http.NotFound(w, r)
		return
	}
	if s.fixture.StaleAfter > 0 && s.fixtureReads.Load() >= s.fixture.StaleAfter {
		http.Error(w, "mock source stopped updating", http.StatusServiceUnavailable)
		return
	}
	if s.fixture.DisconnectAfter > 0 && s.fixtureReads.Load() >= s.fixture.DisconnectAfter {
		disconnect(w)
		return
	}
	step := s.componentStep(fixture, advance)
	if advance {
		s.fixtureReads.Add(1)
	}
	if data {
		writeJSON(w, dataPayload(fixture, step))
		return
	}
	writeJSON(w, livePayload(fixture, step))
}

func (s *mockState) findFixture(kind string, id int) (componentFixture, bool) {
	for _, fixture := range s.fixture.Components {
		if fixture.Kind == kind && fixture.ID == id {
			return fixture, true
		}
	}
	return componentFixture{}, false
}

func (s *mockState) componentStep(fixture componentFixture, advance bool) uint64 {
	key := componentKey(fixture.Kind, fixture.ID)
	s.mu.Lock()
	defer s.mu.Unlock()
	step := s.componentSteps[key]
	if advance {
		step++
		s.componentSteps[key] = step
	}
	return step
}

func (s *mockState) requireDigest(w http.ResponseWriter, r *http.Request) bool {
	if s.profile != "auth" || r.Header.Get("Authorization") != "" {
		return false
	}
	w.Header().Set("WWW-Authenticate", `Digest realm="shelly", nonce="mock-nonce", algorithm=SHA-256, qop="auth"`)
	w.WriteHeader(http.StatusUnauthorized)
	return true
}

func requestedComponentID(r *http.Request) (int, error) {
	query := r.URL.Query()
	values, present := query["id"]
	if !present || len(values) != 1 || len(query) != 1 {
		return 0, fmt.Errorf("invalid component id")
	}
	raw := values[0]
	id, err := strconv.Atoi(raw)
	if err != nil || id < 0 || id > 128 || strconv.Itoa(id) != raw {
		return 0, fmt.Errorf("invalid component id")
	}
	return id, nil
}

func livePayload(fixture componentFixture, step uint64) map[string]any {
	power := fixture.DailyKWh * 1000 / 24
	switch fixture.Kind {
	case "switch":
		return map[string]any{"id": fixture.ID, "output": true, "apower": power, "voltage": 230.0, "current": power / 230, "pf": .98, "freq": 50.0, "aenergy": map[string]any{"total": counterEnergy(fixture, step)}}
	case "em":
		if fixture.ThreePhase {
			return map[string]any{"id": fixture.ID, "a_act_power": power * .32, "b_act_power": power * .33, "c_act_power": power * .35, "total_act_power": power, "a_voltage": 230.1, "b_voltage": 229.8, "c_voltage": 230.4, "a_current": power * .32 / 230.1, "b_current": power * .33 / 229.8, "c_current": power * .35 / 230.4, "a_pf": .98, "b_pf": .97, "c_pf": .99, "frequency": 50.0}
		}
		return map[string]any{"id": fixture.ID, "total_act_power": power, "voltage": 230.0, "current": power / 230, "pf": .98, "frequency": 50.0}
	default:
		return map[string]any{"id": fixture.ID, "act_power": power, "apower": power, "voltage": 230.0, "current": power / 230, "pf": .98, "freq": 50.0}
	}
}

func dataPayload(fixture componentFixture, step uint64) map[string]any {
	total := counterEnergy(fixture, step)
	payload := map[string]any{"id": fixture.ID, "total_act": total, "total_act_ret": 0.0}
	if fixture.ThreePhase {
		payload["a_total_act_energy"] = total * .32
		payload["b_total_act_energy"] = total * .33
		payload["c_total_act_energy"] = total * .35
	}
	return payload
}

func counterEnergy(fixture componentFixture, step uint64) float64 {
	increment := fixture.DailyKWh * 1000 / readingsPerDay
	if fixture.ResetAfter > 0 && step >= fixture.ResetAfter {
		return float64(step-fixture.ResetAfter) * increment
	}
	return fixture.InitialWh + float64(step)*increment
}

func componentKey(kind string, id int) string {
	return kind + ":" + strconv.Itoa(id)
}

func companionKey(kind string, id int) string {
	switch kind {
	case "em":
		return componentKey("emdata", id)
	case "em1":
		return componentKey("em1data", id)
	case "pm":
		return componentKey("pmdata", id)
	default:
		return ""
	}
}

func allowGET(w http.ResponseWriter, r *http.Request) bool {
	if r.Method == http.MethodGet {
		return true
	}
	w.Header().Set("Allow", http.MethodGet)
	http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	return false
}

func disconnect(w http.ResponseWriter) {
	if hijacker, ok := w.(http.Hijacker); ok {
		connection, _, err := hijacker.Hijack()
		if err == nil && connection != nil {
			_ = connection.Close()
			return
		}
	}
	http.Error(w, "mock device offline", http.StatusServiceUnavailable)
}

func writeJSON(w http.ResponseWriter, value any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(value)
}

func isEnergyProfile(profile string) bool {
	switch profile {
	case "energy", "energy-reset", "energy-invalid", "energy-slow", "energy-flaky":
		return true
	default:
		return false
	}
}

func energyIdentity(profile string) (string, string) {
	identities := map[string]string{"energy": "11", "energy-reset": "12", "energy-invalid": "13", "energy-slow": "14", "energy-flaky": "15"}
	suffix := identities[profile]
	return "shellypro3em-" + suffix, "AABBCCDDEE" + suffix
}

func energyLiveStatus(profile string, step uint64) map[string]any {
	if profile == "energy-invalid" {
		return map[string]any{"id": 0, "errors": []string{"ct_type_not_set"}}
	}
	power := 1200.0 + float64(step%20)*10
	return map[string]any{"id": 0, "a_act_power": power * .32, "b_act_power": power * .33, "c_act_power": power * .35, "total_act_power": power, "a_voltage": 230.1, "b_voltage": 229.8, "c_voltage": 230.4, "a_current": 1.8, "b_current": 1.9, "c_current": 2.0, "a_pf": .98, "b_pf": .97, "c_pf": .99, "frequency": 50.0}
}

func energyDataStatus(profile string, step uint64) map[string]any {
	total := 100000.0 + float64(step)*10
	if profile == "energy-reset" && step >= 3 {
		total = float64(step-3) * 10
	}
	return map[string]any{"id": 0, "total_act": total, "total_act_ret": float64(step), "a_total_act_energy": total * .32, "b_total_act_energy": total * .33, "c_total_act_energy": total * .35}
}
