package web

import (
	"archive/zip"
	"context"
	"errors"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/filipegalo/shelly-energy-dashboard/internal/database"
	energyservice "github.com/filipegalo/shelly-energy-dashboard/internal/energy"
	"github.com/shopspring/decimal"
)

type reportsView struct {
	Configured                                                 bool
	Range                                                      string
	Start, End                                                 time.Time
	Current, Previous, Billing                                 energySummary
	ConsumptionChange, CostChange                              string
	Profile                                                    []reportProfileHour
	Anomalies                                                  []reportAnomaly
	Rooms                                                      []reportRoom
	Coverage, Unmetered, Quality                               string
	Events                                                     []database.MeterEvent
	MissingSamples, Samples                                    int
	Invoice                                                    database.InvoiceReconciliation
	InvoiceSaved                                               bool
	EstimateVariable, EstimateTotal, VariableDelta, TotalDelta string
	ExportStart, ExportEnd                                     string
}

type reportProfileHour struct{ Hour, Energy, Peak string }
type reportAnomaly struct {
	When, Actual, Typical, Increase string
	delta                           decimal.Decimal
}
type reportRoom struct {
	Name, Icon, Energy, Cost, Share, Quality string
	Virtual                                  bool
	consumption                              decimal.Decimal
}

func (s *Server) reportsPage(w http.ResponseWriter, r *http.Request) {
	view, err := s.buildReports(r)
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	s.render(w, r, viewData{Page: "reports", Title: "Reports", Reports: &view})
}

func (s *Server) buildReports(r *http.Request) (reportsView, error) {
	config, err := s.store.GetEnergyConfig(r.Context())
	if err != nil {
		return reportsView{}, err
	}
	view := reportsView{Configured: roomEnergyConfigured(config), Range: r.URL.Query().Get("range")}
	if view.Range == "" {
		view.Range = "billing"
	}
	if !view.Configured {
		return view, nil
	}
	now, location, err := roomEnergyNow(config)
	if err != nil {
		return reportsView{}, err
	}
	start, end, interval, err := energyRange(view.Range, r, config, now)
	if err != nil {
		return reportsView{}, err
	}
	view.Start, view.End = start, end
	if exportStart, exportEnd, exportErr := s.exportRange(r.Context(), config, now, "", ""); exportErr == nil {
		view.ExportStart, view.ExportEnd = exportStart.Format(time.DateOnly), exportEnd.AddDate(0, 0, -1).Format(time.DateOnly)
	}
	view.Current, err = s.energySummary(r, config, start, end, interval)
	if err != nil {
		return reportsView{}, err
	}
	duration := end.Sub(start)
	previousStart, previousEnd := start.Add(-duration), start
	view.Previous, err = s.energySummary(r, config, previousStart, previousEnd, interval)
	if err != nil {
		return reportsView{}, err
	}
	view.ConsumptionChange = reportChange(view.Current.ConsumptionKWh, view.Previous.ConsumptionKWh, "kWh")
	view.CostChange = reportChange(view.Current.CostEUR, view.Previous.CostEUR, "€")
	billingStart, billingEnd, _, billingErr := energyRange("billing", r, config, now)
	if billingErr != nil {
		return reportsView{}, billingErr
	}
	view.Billing, err = s.energySummary(r, config, billingStart, billingEnd, "day")
	if err != nil {
		return reportsView{}, err
	}
	profileStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, location).AddDate(0, 0, -30)
	view.Profile, err = s.reportProfile(r, config, profileStart, now)
	if err != nil {
		return reportsView{}, err
	}
	view.Anomalies, err = s.reportAnomalies(r, config, now)
	if err != nil {
		return reportsView{}, err
	}
	roomReport, err := s.roomEnergyReport(r.Context(), config, start, end, interval)
	if err != nil {
		return reportsView{}, err
	}
	view.Coverage, view.Unmetered, view.Quality = percentageText(reportCoverage(roomReport)), decimalText(roomReport.Unmetered.EnergyKWh, 2, "kWh"), reportQualityCode(roomReport)
	rooms, err := s.store.ListRoomsIncludingArchived(r.Context())
	if err != nil {
		return reportsView{}, err
	}
	byID := make(map[int64]database.Room, len(rooms))
	for _, room := range rooms {
		byID[room.ID] = room
	}
	whole := reportWholeEnergy(roomReport)
	for _, series := range roomReport.Rooms {
		room := byID[series.RoomID]
		if room.ID == 0 || series.EnergyKWh == nil {
			continue
		}
		share := "—"
		if whole != nil && whole.IsPositive() {
			value := series.EnergyKWh.Div(*whole)
			share = percentageText(&value)
		}
		view.Rooms = append(view.Rooms, reportRoom{Name: room.Name, Icon: room.Icon, Energy: decimalText(series.EnergyKWh, 2, "kWh"), Cost: "€" + series.AllocatedCost.Round(2).StringFixed(2), Share: share, Quality: string(series.Quality), consumption: *series.EnergyKWh})
	}
	if roomReport.Unmetered.EnergyKWh != nil {
		view.Rooms = append(view.Rooms, reportRoom{Name: "Unmetered / Other", Icon: "∑", Energy: decimalText(roomReport.Unmetered.EnergyKWh, 2, "kWh"), Cost: "€" + roomReport.Unmetered.AllocatedCost.Round(2).StringFixed(2), Share: "—", Quality: string(roomReport.Unmetered.Quality), Virtual: true, consumption: *roomReport.Unmetered.EnergyKWh})
	}
	sort.SliceStable(view.Rooms, func(i, j int) bool { return view.Rooms[i].consumption.GreaterThan(view.Rooms[j].consumption) })
	aggregates, err := s.store.ListEnergyAggregates(r.Context(), config.DeviceID, config.ComponentKey, start, end, interval)
	if err != nil {
		return reportsView{}, err
	}
	for _, item := range aggregates {
		view.Samples += item.SampleCount
		view.MissingSamples += item.MissingSampleCount
	}
	view.Events, _ = s.store.ListMeterEvents(r.Context(), config.DeviceID, config.ComponentKey, 30)
	if invoice, invoiceErr := s.store.InvoiceReconciliationForCycle(r.Context(), billingStart, billingEnd); invoiceErr == nil {
		view.Invoice = invoice
	}
	view.InvoiceSaved = r.URL.Query().Get("saved") == "1"
	view.EstimateVariable, view.EstimateTotal = "€"+view.Billing.CostEUR, "€"+view.Billing.CostEUR
	if view.Invoice.ID > 0 {
		view.VariableDelta = currencyDelta(view.Invoice.VariableCostEUR, view.Billing.CostEUR)
		view.TotalDelta = currencyDelta(view.Invoice.TotalCostEUR, view.Billing.CostEUR)
	}
	return view, nil
}

// reportAnomalies only compares complete hourly buckets. A candidate needs at
// least three equivalent historical hours and must exceed both a 50% relative
// increase and 0.50 kWh absolute increase, keeping normal small fluctuations
// out of the report.
func (s *Server) reportAnomalies(r *http.Request, config database.EnergyConfig, now time.Time) ([]reportAnomaly, error) {
	end := time.Date(now.Year(), now.Month(), now.Day(), now.Hour(), 0, 0, 0, now.Location())
	recentStart := end.AddDate(0, 0, -7)
	baselineStart := recentStart.AddDate(0, 0, -28)
	aggregates, err := s.store.ListEnergyAggregates(r.Context(), config.DeviceID, config.ComponentKey, baselineStart, end, "hour")
	if err != nil {
		return nil, err
	}
	return detectHourlyAnomalies(aggregates, recentStart, end, now.Location()), nil
}

func detectHourlyAnomalies(aggregates []database.EnergyAggregate, recentStart, recentEnd time.Time, location *time.Location) []reportAnomaly {
	byStart := make(map[string]database.EnergyAggregate, len(aggregates))
	for _, item := range aggregates {
		if completeHour(item) {
			byStart[item.IntervalStart.UTC().Format(time.RFC3339)] = item
		}
	}
	result := make([]reportAnomaly, 0)
	for _, item := range aggregates {
		if !completeHour(item) || item.IntervalStart.Before(recentStart) || !item.IntervalStart.Before(recentEnd) {
			continue
		}
		baselineWh, count := 0.0, 0
		for week := 1; week <= 4; week++ {
			key := item.IntervalStart.In(location).AddDate(0, 0, -7*week).UTC().Format(time.RFC3339)
			if previous, found := byStart[key]; found {
				baselineWh += previous.EnergyConsumedWh
				count++
			}
		}
		if count < 3 {
			continue
		}
		typicalWh := baselineWh / float64(count)
		deltaWh := item.EnergyConsumedWh - typicalWh
		if deltaWh < 500 || item.EnergyConsumedWh < typicalWh*1.5 {
			continue
		}
		result = append(result, reportAnomaly{When: item.IntervalStart.In(location).Format("Mon 2 Jan, 15:00"), Actual: decimal.NewFromFloat(item.EnergyConsumedWh/1000).Round(2).StringFixed(2) + " kWh", Typical: decimal.NewFromFloat(typicalWh/1000).Round(2).StringFixed(2) + " kWh", Increase: "+" + decimal.NewFromFloat(deltaWh/1000).Round(2).StringFixed(2) + " kWh", delta: decimal.NewFromFloat(deltaWh)})
	}
	sort.SliceStable(result, func(i, j int) bool { return result[i].delta.GreaterThan(result[j].delta) })
	if len(result) > 5 {
		result = result[:5]
	}
	return result
}

func completeHour(item database.EnergyAggregate) bool {
	return item.SampleCount > 0 && item.MissingSampleCount == 0 && item.CalculationQuality != "partial" && item.CalculationQuality != "missing"
}

func (s *Server) reportProfile(r *http.Request, config database.EnergyConfig, start, end time.Time) ([]reportProfileHour, error) {
	aggregates, err := s.store.ListEnergyAggregates(r.Context(), config.DeviceID, config.ComponentKey, start, end, "hour")
	if err != nil {
		return nil, err
	}
	totals, peaks := [24]float64{}, [24]float64{}
	counts := [24]int{}
	for _, item := range aggregates {
		hour := item.IntervalStart.In(start.Location()).Hour()
		totals[hour] += item.EnergyConsumedWh
		counts[hour]++
		if item.MaximumPowerW != nil && *item.MaximumPowerW > peaks[hour] {
			peaks[hour] = *item.MaximumPowerW
		}
	}
	result := make([]reportProfileHour, 0, 24)
	for hour := 0; hour < 24; hour++ {
		average := 0.0
		if counts[hour] > 0 {
			average = totals[hour] / float64(counts[hour])
		}
		result = append(result, reportProfileHour{Hour: time.Date(2000, 1, 1, hour, 0, 0, 0, time.UTC).Format("15:04"), Energy: decimal.NewFromFloat(average/1000).Round(2).StringFixed(2) + " kWh", Peak: floatPowerText(&peaks[hour])})
	}
	return result, nil
}

func reportChange(current, previous, suffix string) string {
	c, cErr := decimal.NewFromString(current)
	p, pErr := decimal.NewFromString(previous)
	if cErr != nil || pErr != nil || !p.IsPositive() {
		return "No comparable prior period"
	}
	delta := c.Sub(p)
	sign := "+"
	if delta.IsNegative() {
		sign = ""
	}
	return sign + delta.Round(2).StringFixed(2) + " " + suffix + " (" + sign + delta.Div(p).Mul(decimal.NewFromInt(100)).Round(1).StringFixed(1) + "%)"
}

func currencyDelta(actual, estimate string) string {
	a, aErr := decimal.NewFromString(actual)
	e, eErr := decimal.NewFromString(estimate)
	if aErr != nil || eErr != nil {
		return "—"
	}
	delta := a.Sub(e)
	sign := "+"
	if delta.IsNegative() {
		sign = ""
	}
	return sign + "€" + delta.Round(2).StringFixed(2)
}

func (s *Server) saveInvoiceReconciliation(w http.ResponseWriter, r *http.Request) {
	config, err := s.store.GetEnergyConfig(r.Context())
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	now, _, err := roomEnergyNow(config)
	if err != nil {
		s.formError(w, r, err)
		return
	}
	start, end, _, err := energyRange("billing", r, config, now)
	if err != nil {
		s.formError(w, r, err)
		return
	}
	values := []string{strings.TrimSpace(r.FormValue("variable_cost_eur")), strings.TrimSpace(r.FormValue("fixed_cost_eur")), strings.TrimSpace(r.FormValue("total_cost_eur"))}
	for _, value := range values {
		if _, parseErr := decimal.NewFromString(value); parseErr != nil || strings.HasPrefix(value, "-") {
			s.formError(w, r, errors.New("invoice amounts must be non-negative decimal values"))
			return
		}
	}
	if _, err = s.store.SaveInvoiceReconciliation(r.Context(), database.InvoiceReconciliation{CycleStart: start, CycleEnd: end, VariableCostEUR: values[0], FixedCostEUR: values[1], TotalCostEUR: values[2]}); err != nil {
		s.formError(w, r, err)
		return
	}
	http.Redirect(w, r, "/reports?range=billing&saved=1", http.StatusSeeOther)
}

// exportReports streams CSV exports straight to the response so a year of
// 15-minute rows never has to be held in memory. Once streaming starts a
// failure can only truncate the download, so it is logged.
func (s *Server) exportReports(w http.ResponseWriter, r *http.Request) {
	config, err := s.store.GetEnergyConfig(r.Context())
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	if !roomEnergyConfigured(config) {
		s.formError(w, r, errors.New("configure a whole-house meter before exporting reports"))
		return
	}
	now, _, err := roomEnergyNow(config)
	if err != nil {
		s.formError(w, r, err)
		return
	}
	start, end, err := s.exportRange(r.Context(), config, now, r.URL.Query().Get("start"), r.URL.Query().Get("end"))
	if err != nil {
		s.formError(w, r, err)
		return
	}
	export, err := energyservice.NewExport(r.Context(), s.store, config, start, end)
	if err != nil {
		s.formError(w, r, err)
		return
	}
	prefix := "shelly-energy_" + start.Format(time.DateOnly) + "_to_" + end.AddDate(0, 0, -1).Format(time.DateOnly) + "_"
	report := r.URL.Query().Get("report")
	w.Header().Set("Cache-Control", "no-store")
	if report == "all" {
		w.Header().Set("Content-Type", "application/zip")
		w.Header().Set("Content-Disposition", `attachment; filename="`+prefix+`solar-analysis.zip"`)
		archive := zip.NewWriter(w)
		for _, file := range export.Files() {
			part, createErr := archive.CreateHeader(&zip.FileHeader{Name: prefix + file.Name, Method: zip.Deflate, Modified: export.GeneratedAt})
			if createErr == nil {
				createErr = file.Write(r.Context(), part)
			}
			if createErr != nil {
				s.logger.Error("report export failed", "operation", "report_export", "report", file.Key, "error", createErr)
				return
			}
		}
		if err := archive.Close(); err != nil {
			s.logger.Error("report export failed", "operation", "report_export", "report", report, "error", err)
		}
		return
	}
	for _, file := range export.Files() {
		if file.Key != report {
			continue
		}
		contentType := "text/csv; charset=utf-8"
		if strings.HasSuffix(file.Name, ".txt") {
			contentType = "text/plain; charset=utf-8"
		}
		w.Header().Set("Content-Type", contentType)
		w.Header().Set("Content-Disposition", `attachment; filename="`+prefix+file.Name+`"`)
		if err := file.Write(r.Context(), w); err != nil {
			s.logger.Error("report export failed", "operation", "report_export", "report", report, "error", err)
		}
		return
	}
	s.formError(w, r, errors.New("unknown report export"))
}

// exportRange returns whole local days [start, end) that end no later than
// today, so an export never contains a partially elapsed day. Without dates it
// covers the last 12 months, or all history when that is shorter.
func (s *Server) exportRange(ctx context.Context, config database.EnergyConfig, now time.Time, startText, endText string) (time.Time, time.Time, error) {
	location := now.Location()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, location)
	start, end := today.AddDate(-1, 0, 0), today
	if first, err := s.store.EnergyHistoryStart(ctx, config.DeviceID, config.ComponentKey); err == nil {
		first = first.In(location)
		if day := time.Date(first.Year(), first.Month(), first.Day(), 0, 0, 0, 0, location); day.After(start) {
			start = day
		}
	}
	if startText != "" {
		parsed, err := time.ParseInLocation(time.DateOnly, startText, location)
		if err != nil {
			return time.Time{}, time.Time{}, errors.New("export start must be a valid date")
		}
		start = parsed
	}
	if endText != "" {
		parsed, err := time.ParseInLocation(time.DateOnly, endText, location)
		if err != nil {
			return time.Time{}, time.Time{}, errors.New("export end must be a valid date")
		}
		end = parsed.AddDate(0, 0, 1)
	}
	if end.After(today) {
		end = today
	}
	return start, end, nil
}
