package web

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/filipegalo/shelly-energy-dashboard/internal/database"
	energyservice "github.com/filipegalo/shelly-energy-dashboard/internal/energy"
	"github.com/filipegalo/shelly-energy-dashboard/internal/energy/billingcycle"
	"github.com/filipegalo/shelly-energy-dashboard/internal/energy/pricing"
	"github.com/filipegalo/shelly-energy-dashboard/internal/shelly"
	"github.com/shopspring/decimal"
)

type energyView struct {
	Configured           bool
	Onboarding           bool
	Config               database.EnergyConfig
	Device               database.Device
	Devices              []database.Device
	Components           []shelly.EnergyComponent
	TestReading          shelly.EnergyReading
	TestComponent        shelly.EnergyComponent
	SelectedComponentKey string
	TestSuccessful       bool
	Message              string
	Live                 energyservice.LiveState
	Today                energySummary
	Billing              energySummary
	Month                energySummary
	Selected             energySummary
	SelectedRange        string
	HistoryStart         time.Time
	Pricing              database.PricingVersion
	PricingHistory       []database.PricingVersion
	PricingPreviews      []energyPricingPreview
	VATPreview           string
	BillingStart         time.Time
	BillingEnd           time.Time
	BillingDays          int
	BillingElapsedDays   int
	ReducedQuotaUsed     string
	ReducedRemaining     string
}

type energySummary struct {
	Start          time.Time
	End            time.Time
	ConsumptionKWh string
	CostEUR        string
	ReducedKWh     string
	StandardKWh    string
	EnergyNetEUR   string
	ReducedVATEUR  string
	StandardVATEUR string
	ExciseNetEUR   string
	ExciseVATEUR   string
	TotalEUR       string
	HasData        bool
}

type energyPricingPreview struct {
	Consumption string
	Total       string
}

func (s *Server) energyPage(w http.ResponseWriter, r *http.Request) {
	view, err := s.buildEnergyDashboard(r)
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	s.render(w, r, viewData{Page: "energy", Title: "Energy", Energy: view})
}

func (s *Server) buildEnergyDashboard(r *http.Request) (energyView, error) {
	config, err := s.store.GetEnergyConfig(r.Context())
	if err != nil {
		return energyView{}, err
	}
	view := energyView{Config: config, Configured: config.DeviceID != 0 && config.ComponentKey != "", Onboarding: config.DeviceID == 0 || config.ComponentKey == "", SelectedRange: selectedEnergyRange(r)}
	if view.Onboarding {
		return view, nil
	}
	view.Device, err = s.store.GetDevice(r.Context(), config.DeviceID)
	if err != nil {
		return energyView{}, err
	}
	if s.energy != nil {
		view.Live = s.energy.Latest()
	}
	if view.Live.Reading.Timestamp.IsZero() {
		if sample, sampleErr := s.store.LatestEnergySample(r.Context(), config.DeviceID, config.ComponentKey); sampleErr == nil {
			view.Live = liveStateFromSample(config, sample)
		}
	}
	view.HistoryStart, _ = s.store.EnergyHistoryStart(r.Context(), config.DeviceID, config.ComponentKey)
	location, err := time.LoadLocation(config.Timezone)
	if err != nil {
		return energyView{}, err
	}
	now := time.Now().In(location)
	todayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, location)
	cycle, err := billingcycle.CycleContaining(now, config.BillingCycleStartDay, location)
	if err != nil {
		return energyView{}, err
	}
	monthStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, location)
	view.BillingStart, view.BillingEnd, view.BillingDays = cycle.Start, cycle.End, cycle.Days
	for cursor := cycle.Start; !cursor.After(todayStart) && cursor.Before(cycle.End); cursor = cursor.AddDate(0, 0, 1) {
		view.BillingElapsedDays++
	}
	view.Today, _ = s.energySummary(r, config, todayStart, now, "hour")
	view.Billing, _ = s.energySummary(r, config, cycle.Start, now, "day")
	view.Month, _ = s.energySummary(r, config, monthStart, now, "day")
	start, end, interval, err := energyRange(view.SelectedRange, r, config, now)
	if err != nil {
		return energyView{}, err
	}
	view.Selected, _ = s.energySummary(r, config, start, end, interval)
	view.ReducedQuotaUsed, view.ReducedRemaining = vatQuota(view.Billing, cycle, s.store, r, config)
	return view, nil
}

func (s *Server) energySettingsPage(w http.ResponseWriter, r *http.Request) {
	view, err := s.buildEnergySettings(r, 0, nil)
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	if r.URL.Query().Get("saved") == "1" {
		view.Message = "Energy settings saved. Background collection is paused."
		if view.Config.Enabled {
			view.Message = "Energy settings saved. Collection is running in the background."
		}
	}
	s.render(w, r, viewData{Page: "energy-settings", Title: "Energy settings", Energy: view})
}

func (s *Server) buildEnergySettings(r *http.Request, selectedDeviceID int64, components []shelly.EnergyComponent) (energyView, error) {
	config, err := s.store.GetEnergyConfig(r.Context())
	if err != nil {
		return energyView{}, err
	}
	devices, err := s.store.ListDevices(r.Context(), database.DeviceFilter{})
	if err != nil {
		return energyView{}, err
	}
	eligible := make([]database.Device, 0, len(devices))
	for _, device := range devices {
		for _, capability := range device.Capabilities() {
			if capability == "power meters" {
				eligible = append(eligible, device)
				break
			}
		}
	}
	if selectedDeviceID == 0 {
		selectedDeviceID = config.DeviceID
	}
	view := energyView{Config: config, Devices: eligible, Components: components, SelectedComponentKey: config.ComponentKey, Configured: config.DeviceID != 0 && config.ComponentKey != ""}
	for _, device := range eligible {
		if device.ID == selectedDeviceID {
			view.Device = device
		}
	}
	view.Pricing, err = s.store.ActivePricingVersionAt(r.Context(), time.Now())
	if err != nil {
		return energyView{}, err
	}
	view.PricingHistory, _ = s.store.ListPricingVersions(r.Context())
	view.PricingPreviews, view.VATPreview, err = pricingPreviews(view.Pricing)
	return view, err
}

func (s *Server) testEnergyMeter(w http.ResponseWriter, r *http.Request) {
	deviceID, _ := strconv.ParseInt(r.FormValue("device_id"), 10, 64)
	if deviceID < 1 || s.energy == nil {
		s.formError(w, r, errors.New("select a discovered Shelly energy meter"))
		return
	}
	_, components, err := s.energy.DiscoverMeter(r.Context(), deviceID)
	view, viewErr := s.buildEnergySettings(r, deviceID, components)
	if viewErr != nil {
		s.serverError(w, r, viewErr)
		return
	}
	if err != nil {
		view.Message = "Meter test failed: " + err.Error()
	} else if componentKey := selectEnergyComponentKey(r.FormValue("component_key"), components); componentKey != "" {
		view.SelectedComponentKey = componentKey
		_, component, reading, testErr := s.energy.TestMeter(r.Context(), deviceID, componentKey)
		if testErr != nil {
			view.Message = "Meter test failed: " + testErr.Error()
		} else {
			view.TestSuccessful, view.TestComponent, view.TestReading = true, component, reading
			view.Message = "Meter test succeeded. Monitoring is not configured yet; click Save tested meter to begin collecting."
		}
	} else {
		view.Message = "Choose one of the discovered meter components, then test it."
	}
	s.render(w, r, viewData{Page: "energy-settings", Title: "Energy settings", Energy: view})
}

func selectEnergyComponentKey(requested string, components []shelly.EnergyComponent) string {
	if selected := strings.TrimSpace(requested); selected != "" {
		return selected
	}
	if len(components) == 1 {
		return components[0].Key
	}
	for _, component := range components {
		if component.Key == "emeter:total" {
			return component.Key
		}
	}
	return ""
}

func (s *Server) saveEnergyMeter(w http.ResponseWriter, r *http.Request) {
	deviceID, _ := strconv.ParseInt(r.FormValue("device_id"), 10, 64)
	componentKey := strings.TrimSpace(r.FormValue("component_key"))
	if deviceID < 1 || len(componentKey) == 0 || len(componentKey) > 64 {
		s.formError(w, r, errors.New("select a valid discovered energy component"))
		return
	}
	if s.energy == nil {
		s.formError(w, r, errors.New("energy collector is unavailable"))
		return
	}
	_, component, _, err := s.energy.TestMeter(r.Context(), deviceID, componentKey)
	if err != nil {
		s.formError(w, r, fmt.Errorf("meter validation failed: %w", err))
		return
	}
	config, err := s.store.GetEnergyConfig(r.Context())
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	previousDevice, previousComponent := config.DeviceID, config.ComponentKey
	config.DeviceID, config.ComponentKey, config.ComponentType = deviceID, component.Key, component.Type
	config.Enabled = r.FormValue("enabled") == "on"
	config.PollIntervalSeconds, _ = strconv.Atoi(r.FormValue("poll_interval_seconds"))
	config.MaximumExpectedPowerW, _ = strconv.ParseFloat(r.FormValue("maximum_expected_power_w"), 64)
	capabilities, _ := json.Marshal(component.Capabilities)
	config.CapabilitiesJSON = string(capabilities)
	if err := validateEnergyConfigForm(config); err != nil {
		s.formError(w, r, err)
		return
	}
	if _, err := s.store.SaveEnergyConfig(r.Context(), config); err != nil {
		s.formError(w, r, err)
		return
	}
	eventType := "meter_selected"
	if previousDevice != 0 && (previousDevice != deviceID || previousComponent != componentKey) {
		eventType = "meter_changed"
	}
	_, _ = s.store.InsertMeterEvent(r.Context(), database.MeterEvent{DeviceID: deviceID, ComponentKey: componentKey, EventType: eventType, DetailsJSON: `{}`})
	if err := s.energy.Reconfigure(r.Context()); err != nil {
		s.formError(w, r, err)
		return
	}
	s.redirect(w, r, "/energy/settings?saved=1")
}

func (s *Server) saveEnergyPricing(w http.ResponseWriter, r *http.Request) {
	active, err := s.store.ActivePricingVersionAt(r.Context(), time.Now())
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	version := database.PricingVersion{ReferencePricePerKWhExcludingVAT: active.ReferencePricePerKWhExcludingVAT, EnergyPricePerKWhExcludingVAT: strings.TrimSpace(r.FormValue("energy_price")), ContractedPowerKVA: strings.TrimSpace(r.FormValue("contracted_power_kva")), ReducedVATEnabled: r.FormValue("reduced_vat_enabled") == "on", LargeFamilyEnabled: r.FormValue("large_family_enabled") == "on", ReducedVATRate: decimalRate(r.FormValue("reduced_vat_rate")), StandardVATRate: decimalRate(r.FormValue("standard_vat_rate")), StandardReducedVATLimitKWhPer30Days: strings.TrimSpace(r.FormValue("standard_limit_kwh")), LargeFamilyReducedVATLimitKWhPer30Days: strings.TrimSpace(r.FormValue("large_family_limit_kwh")), ElectricityExciseTaxPerKWh: strings.TrimSpace(r.FormValue("excise_tax")), ElectricityExciseTaxVATRate: decimalRate(r.FormValue("excise_tax_vat_rate")), IncludeExciseTax: r.FormValue("include_excise_tax") == "on", BillingCycleStartDay: active.BillingCycleStartDay, Currency: "EUR", Region: "Portugal Continental"}
	config, _ := s.store.GetEnergyConfig(r.Context())
	version.BillingCycleStartDay = config.BillingCycleStartDay
	location, loadErr := time.LoadLocation(config.Timezone)
	if loadErr != nil {
		location = time.Local
	}
	version.PriceEffectiveFrom, err = parseEffectiveTime(r.FormValue("effective_from"), location)
	if err != nil {
		s.formError(w, r, err)
		return
	}
	if _, err := energyservice.PricingConfig(version); err != nil {
		s.formError(w, r, err)
		return
	}
	if _, err := s.store.CreatePricingVersion(r.Context(), version); err != nil {
		s.formError(w, r, errors.New("a pricing version already exists at that effective time"))
		return
	}
	s.redirect(w, r, "/energy/settings?saved=1")
}

func (s *Server) saveEnergyBillingCycle(w http.ResponseWriter, r *http.Request) {
	config, err := s.store.GetEnergyConfig(r.Context())
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	config.BillingCycleStartDay, _ = strconv.Atoi(r.FormValue("billing_cycle_start_day"))
	config.Timezone = strings.TrimSpace(r.FormValue("timezone"))
	config.SampleRetentionDays, _ = strconv.Atoi(r.FormValue("sample_retention_days"))
	config.MinuteRetentionDays, _ = strconv.Atoi(r.FormValue("minute_retention_days"))
	config.HourlyRetentionDays, _ = strconv.Atoi(r.FormValue("hourly_retention_days"))
	if err := validateEnergyConfigForm(config); err != nil {
		s.formError(w, r, err)
		return
	}
	active, priceErr := s.store.ActivePricingVersionAt(r.Context(), time.Now())
	var billingPricing *database.PricingVersion
	if priceErr == nil && active.BillingCycleStartDay != config.BillingCycleStartDay {
		active.ID = 0
		active.BillingCycleStartDay = config.BillingCycleStartDay
		active.PriceEffectiveFrom = time.Now()
		billingPricing = &active
	}
	if priceErr != nil {
		s.serverError(w, r, priceErr)
		return
	}
	if err := s.store.SaveEnergyBillingSettings(r.Context(), config, billingPricing); err != nil {
		s.serverError(w, r, err)
		return
	}
	if s.energy != nil {
		_ = s.energy.Reconfigure(r.Context())
	}
	s.redirect(w, r, "/energy/settings?saved=1")
}

func (s *Server) disableEnergyMonitoring(w http.ResponseWriter, r *http.Request) {
	if err := s.store.DisableEnergyMonitoring(r.Context()); err != nil {
		s.formError(w, r, err)
		return
	}
	if s.energy != nil {
		_ = s.energy.Reconfigure(r.Context())
	}
	s.redirect(w, r, "/energy/settings?saved=1")
}

func (s *Server) enableEnergyMonitoring(w http.ResponseWriter, r *http.Request) {
	config, err := s.store.GetEnergyConfig(r.Context())
	if err != nil || config.DeviceID == 0 || config.ComponentKey == "" {
		s.formError(w, r, errors.New("configure and test a main meter first"))
		return
	}
	config.Enabled = true
	if _, err := s.store.SaveEnergyConfig(r.Context(), config); err != nil {
		s.formError(w, r, err)
		return
	}
	if s.energy != nil {
		_ = s.energy.Reconfigure(r.Context())
	}
	s.redirect(w, r, "/energy/settings?saved=1")
}

func (s *Server) energyHistory(w http.ResponseWriter, r *http.Request) {
	config, err := s.store.GetEnergyConfig(r.Context())
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	location, err := time.LoadLocation(config.Timezone)
	if err != nil {
		s.formError(w, r, err)
		return
	}
	now := time.Now().In(location)
	start, end, interval, err := energyRange(selectedEnergyRange(r), r, config, now)
	if err != nil {
		s.formError(w, r, err)
		return
	}
	report, err := energyservice.BuildReport(r.Context(), s.store, config, start, end, interval)
	if err != nil {
		s.formError(w, r, err)
		return
	}
	jsonResponse(w, http.StatusOK, map[string]any{"range": map[string]any{"start": start, "end": end, "timezone": config.Timezone}, "interval": interval, "currency": config.Currency, "points": report.Points})
}

func (s *Server) energyPricingPreview(w http.ResponseWriter, r *http.Request) {
	active, err := s.store.ActivePricingVersionAt(r.Context(), time.Now())
	if err != nil {
		s.formError(w, r, err)
		return
	}
	query := r.URL.Query()
	active.EnergyPricePerKWhExcludingVAT = query.Get("energy_price")
	active.ContractedPowerKVA = query.Get("contracted_power_kva")
	active.ReducedVATRate = decimalRate(query.Get("reduced_vat_rate"))
	active.StandardVATRate = decimalRate(query.Get("standard_vat_rate"))
	active.StandardReducedVATLimitKWhPer30Days = query.Get("standard_limit_kwh")
	active.LargeFamilyReducedVATLimitKWhPer30Days = query.Get("large_family_limit_kwh")
	active.ElectricityExciseTaxPerKWh = query.Get("excise_tax")
	active.ElectricityExciseTaxVATRate = decimalRate(query.Get("excise_tax_vat_rate"))
	active.ReducedVATEnabled = query.Get("reduced_vat_enabled") == "on"
	active.LargeFamilyEnabled = query.Get("large_family_enabled") == "on"
	active.IncludeExciseTax = query.Get("include_excise_tax") == "on"
	previews, allowance, err := pricingPreviews(active)
	if err != nil {
		jsonResponse(w, http.StatusUnprocessableEntity, map[string]string{"error": err.Error()})
		return
	}
	items := make([]map[string]string, 0, len(previews))
	for _, preview := range previews {
		items = append(items, map[string]string{"consumption_kwh": preview.Consumption, "total_eur": preview.Total})
	}
	jsonResponse(w, http.StatusOK, map[string]any{"previews": items, "allowance_kwh": allowance})
}

func (s *Server) energyCostBreakdown(w http.ResponseWriter, r *http.Request) {
	view, err := s.buildEnergyDashboard(r)
	if err != nil {
		s.formError(w, r, err)
		return
	}
	if err := s.views.ExecuteTemplate(w, "energy-cost-breakdown", viewData{Energy: view}); err != nil {
		s.serverError(w, r, err)
	}
}

func (s *Server) energySummaryPartial(w http.ResponseWriter, r *http.Request) {
	view, err := s.buildEnergyDashboard(r)
	if err != nil {
		s.formError(w, r, err)
		return
	}
	if err := s.views.ExecuteTemplate(w, "energy-summary", viewData{Energy: view}); err != nil {
		s.serverError(w, r, err)
	}
}

func (s *Server) energyEvents(w http.ResponseWriter, r *http.Request) {
	if s.energy == nil {
		http.Error(w, "energy collector unavailable", http.StatusServiceUnavailable)
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Accel-Buffering", "no")
	stream, unsubscribe, err := s.energy.Subscribe()
	if err != nil {
		http.Error(w, "too many energy event streams", http.StatusServiceUnavailable)
		return
	}
	defer unsubscribe()
	keepalive := time.NewTicker(15 * time.Second)
	defer keepalive.Stop()
	for {
		select {
		case state := <-stream:
			event := "energy-reading"
			if !state.Online {
				event = "meter-offline"
			} else if state.Stale {
				event = "meter-stale"
			}
			payload, _ := json.Marshal(state)
			fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, payload)
			flusher.Flush()
		case <-keepalive.C:
			fmt.Fprint(w, ": keepalive\n\n")
			flusher.Flush()
		case <-r.Context().Done():
			return
		}
	}
}

func (s *Server) energySummary(r *http.Request, config database.EnergyConfig, start, end time.Time, interval string) (energySummary, error) {
	report, err := energyservice.BuildReport(r.Context(), s.store, config, start, end, interval)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return energySummary{Start: start, End: end}, nil
		}
		return energySummary{}, err
	}
	return summaryFromReport(report), nil
}

func summaryFromReport(report energyservice.Report) energySummary {
	cost := report.Cost
	hasData := false
	for _, point := range report.Points {
		hasData = hasData || point.EnergyKWh != nil
	}
	return energySummary{Start: report.Start, End: report.End, ConsumptionKWh: report.Consumption.Round(2).StringFixed(2), CostEUR: cost.TotalIncludingVAT.Round(2).StringFixed(2), ReducedKWh: cost.ReducedRateKWh.Round(2).StringFixed(2), StandardKWh: cost.StandardRateKWh.Round(2).StringFixed(2), EnergyNetEUR: cost.EnergyNetTotal.Round(2).StringFixed(2), ReducedVATEUR: cost.ReducedVAT.Round(2).StringFixed(2), StandardVATEUR: cost.StandardVAT.Round(2).StringFixed(2), ExciseNetEUR: cost.ExciseTaxNet.Round(2).StringFixed(2), ExciseVATEUR: cost.ExciseTaxVAT.Round(2).StringFixed(2), TotalEUR: cost.TotalIncludingVAT.Round(2).StringFixed(2), HasData: hasData}
}

func energyRange(name string, r *http.Request, config database.EnergyConfig, now time.Time) (time.Time, time.Time, string, error) {
	location := now.Location()
	switch name {
	case "24h":
		return now.Add(-24 * time.Hour), now, "hour", nil
	case "7d":
		return now.AddDate(0, 0, -7), now, "hour", nil
	case "30d":
		return now.AddDate(0, 0, -30), now, "day", nil
	case "billing":
		cycle, err := billingcycle.CycleContaining(now, config.BillingCycleStartDay, location)
		return cycle.Start, now, "day", err
	case "previous_billing":
		current, err := billingcycle.CycleContaining(now, config.BillingCycleStartDay, location)
		if err != nil {
			return time.Time{}, time.Time{}, "", err
		}
		previous, err := billingcycle.CycleContaining(current.Start.Add(-time.Nanosecond), config.BillingCycleStartDay, location)
		return previous.Start, previous.End, "day", err
	case "month":
		return time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, location), now, "day", nil
	case "custom":
		start, startErr := time.ParseInLocation("2006-01-02", r.URL.Query().Get("start"), location)
		endDate, endErr := time.ParseInLocation("2006-01-02", r.URL.Query().Get("end"), location)
		end := endDate.AddDate(0, 0, 1)
		if startErr != nil || endErr != nil || !start.Before(end) || end.Sub(start) > 366*24*time.Hour {
			return time.Time{}, time.Time{}, "", errors.New("custom energy range must contain valid dates spanning at most 366 days")
		}
		interval := "day"
		if end.Sub(start) <= 7*24*time.Hour {
			interval = "hour"
		}
		return start, end, interval, nil
	default:
		return now.Add(-24 * time.Hour), now, "hour", nil
	}
}

func selectedEnergyRange(r *http.Request) string {
	value := r.URL.Query().Get("range")
	if value == "" {
		return "24h"
	}
	return value
}

func pricingPreviews(version database.PricingVersion) ([]energyPricingPreview, string, error) {
	rules, err := energyservice.PricingConfig(version)
	if err != nil {
		return nil, "", err
	}
	previews := make([]energyPricingPreview, 0, 4)
	for _, consumption := range []int64{100, 200, 300, 500} {
		cost, err := pricing.Calculate(rules, decimal.NewFromInt(consumption), 30)
		if err != nil {
			return nil, "", err
		}
		previews = append(previews, energyPricingPreview{Consumption: strconv.FormatInt(consumption, 10), Total: cost.TotalIncludingVAT.Round(2).StringFixed(2)})
	}
	preview, err := pricing.Calculate(rules, decimal.Zero, 31)
	if err != nil {
		return nil, "", err
	}
	return previews, preview.ProratedReducedLimitKWh.Round(2).StringFixed(2), nil
}

func vatQuota(summary energySummary, cycle billingcycle.Period, store *database.Store, r *http.Request, config database.EnergyConfig) (string, string) {
	version, err := store.ActivePricingVersionAt(r.Context(), time.Now())
	if err != nil {
		return "0.00", "0.00"
	}
	rules, err := energyservice.PricingConfig(version)
	if err != nil {
		return "0.00", "0.00"
	}
	consumption, _ := decimal.NewFromString(summary.ConsumptionKWh)
	cost, err := pricing.Calculate(rules, consumption, cycle.Days)
	if err != nil {
		return "0.00", "0.00"
	}
	used := decimal.Min(consumption, cost.ProratedReducedLimitKWh)
	return used.Round(2).StringFixed(2), decimal.Max(cost.ProratedReducedLimitKWh.Sub(used), decimal.Zero).Round(2).StringFixed(2)
}

func validateEnergyConfigForm(config database.EnergyConfig) error {
	if config.PollIntervalSeconds < 5 || config.PollIntervalSeconds > 300 {
		return errors.New("polling interval must be between 5 and 300 seconds")
	}
	if config.BillingCycleStartDay < 1 || config.BillingCycleStartDay > 31 {
		return errors.New("billing-cycle start day must be between 1 and 31")
	}
	if len(config.ComponentKey) > 64 || len(config.Timezone) == 0 || len(config.Timezone) > 100 {
		return errors.New("energy component or timezone is too long")
	}
	if config.MaximumExpectedPowerW <= 0 || config.MaximumExpectedPowerW > 1_000_000 {
		return errors.New("maximum expected power must be between 1 and 1,000,000 watts")
	}
	if config.SampleRetentionDays < 1 || config.SampleRetentionDays > 365 || config.MinuteRetentionDays < config.SampleRetentionDays || config.MinuteRetentionDays > 730 || config.HourlyRetentionDays < config.MinuteRetentionDays || config.HourlyRetentionDays > 3650 {
		return errors.New("retention must increase from raw to minute to hourly data and stay within supported limits")
	}
	if _, err := time.LoadLocation(config.Timezone); err != nil {
		return errors.New("timezone must be a valid IANA timezone")
	}
	return nil
}

func decimalRate(value string) string {
	parsed, err := pricing.ParseFixedDecimal("VAT rate", value)
	if err != nil {
		return strings.TrimSpace(value)
	}
	return parsed.Div(decimal.NewFromInt(100)).String()
}

func parseEffectiveTime(value string, location *time.Location) (time.Time, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Now().In(location), nil
	}
	if len(value) > 32 {
		return time.Time{}, errors.New("pricing effective time is too long")
	}
	parsed, err := time.ParseInLocation("2006-01-02T15:04", value, location)
	if err != nil {
		return time.Time{}, errors.New("pricing effective time must use a valid local date and time")
	}
	return parsed, nil
}

func liveStateFromSample(config database.EnergyConfig, sample database.EnergySample) energyservice.LiveState {
	reading := shelly.EnergyReading{Timestamp: sample.RecordedAt, PowerW: sample.PowerW, ImportedEnergyWh: sample.ImportedEnergyTotalWh, ExportedEnergyWh: sample.ExportedEnergyTotalWh, VoltageV: sample.VoltageV, CurrentA: sample.CurrentA, PowerFactor: sample.PowerFactor, FrequencyHz: sample.FrequencyHz}
	stale := time.Since(sample.RecordedAt) > 2*time.Duration(config.PollIntervalSeconds)*time.Second
	return energyservice.LiveState{Configured: true, Enabled: config.Enabled, Online: !stale, Stale: stale, DeviceID: config.DeviceID, ComponentKey: config.ComponentKey, PollIntervalSeconds: config.PollIntervalSeconds, Reading: reading}
}
