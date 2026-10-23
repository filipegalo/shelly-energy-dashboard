package energy

import (
	"context"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/filipegalo/shelly-energy-dashboard/internal/database"
	"github.com/filipegalo/shelly-energy-dashboard/internal/energy/pricing"
)

// Historical CSV exports for solar/battery sizing. Every energy value comes
// from persisted interval aggregates, which are derived from the meters'
// cumulative counters; power readings are never summed as energy. Missing
// intervals stay blank instead of becoming zero.
const (
	MaximumExportDays     = 366
	exportInterval        = 15 * time.Minute
	exportDaytimeStart    = 8
	exportDaytimeEnd      = 20
	exportCompleteDay     = 0.9
	evChargingThresholdW  = 1000.0
	evSessionGap          = 10 * time.Minute
	exportTimestampLayout = "2006-01-02T15:04:05-07:00"
)

var evChargerName = regexp.MustCompile(`(?i)\b(ev|car|vehicle)\b.*charg|wallbox`)

type ExportDevice struct {
	Source database.EnergySourceRef
	Name   string
	Model  string
	Room   string
	Role   string
	Column string
}

type ExportFile struct {
	Key   string
	Name  string
	Write func(context.Context, io.Writer) error
}

type Export struct {
	store       *database.Store
	config      database.EnergyConfig
	location    *time.Location
	Start       time.Time
	End         time.Time
	GeneratedAt time.Time
	Main        database.EnergySourceRef
	Devices     []ExportDevice
	devices     map[database.EnergySourceRef]int
	ev          int
}

// NewExport covers whole local days in [start, end). Devices are every room
// energy source assigned during the range except the main meter itself, which
// would otherwise be double-counted.
func NewExport(ctx context.Context, store *database.Store, config database.EnergyConfig, start, end time.Time) (Export, error) {
	if config.DeviceID < 1 || config.ComponentKey == "" {
		return Export{}, errors.New("main energy meter is not configured")
	}
	location, err := time.LoadLocation(config.Timezone)
	if err != nil {
		return Export{}, errors.New("configured energy timezone is invalid")
	}
	if !start.Before(end) || start.AddDate(0, 0, MaximumExportDays).Before(end) {
		return Export{}, fmt.Errorf("export range must contain between 1 and %d days", MaximumExportDays)
	}
	if config.PollIntervalSeconds < 1 {
		return Export{}, errors.New("main meter poll interval is invalid")
	}
	assignments, err := store.ListRoomEnergyAssignmentsOverlapping(ctx, start, end)
	if err != nil {
		return Export{}, err
	}
	export := Export{store: store, config: config, location: location, Start: start.In(location), End: end.In(location), GeneratedAt: time.Now().In(location), Main: database.EnergySourceRef{DeviceID: config.DeviceID, ComponentKey: config.ComponentKey}, devices: map[database.EnergySourceRef]int{}, ev: -1}
	// Assignments are ordered by effective_from, so the latest revision of a
	// source wins for its name, room and role.
	latest := map[database.EnergySourceRef]database.RoomEnergyAssignment{}
	for _, assignment := range assignments {
		ref := database.EnergySourceRef{DeviceID: assignment.DeviceID, ComponentKey: assignment.ComponentKey}
		if ref != export.Main {
			latest[ref] = assignment
		}
	}
	for ref, assignment := range latest {
		export.Devices = append(export.Devices, ExportDevice{Source: ref, Name: assignment.DeviceName, Model: assignment.DeviceModel, Room: assignment.RoomName, Role: assignment.AggregationRole})
	}
	sort.Slice(export.Devices, func(i, j int) bool {
		left, right := export.Devices[i], export.Devices[j]
		if left.Room != right.Room {
			return left.Room < right.Room
		}
		if left.Name != right.Name {
			return left.Name < right.Name
		}
		return left.Source.ComponentKey < right.Source.ComponentKey
	})
	used := map[string]bool{}
	for index := range export.Devices {
		device := &export.Devices[index]
		device.Column = "device_" + exportSlug(device.Name)
		if used[device.Column] {
			device.Column += fmt.Sprintf("_%d_%s", device.Source.DeviceID, exportSlug(device.Source.ComponentKey))
		}
		used[device.Column] = true
		export.devices[device.Source] = index
		if export.ev < 0 && evChargerName.MatchString(device.Name) {
			export.ev = index
		}
	}
	return export, nil
}

func (e Export) Files() []ExportFile {
	return []ExportFile{
		{Key: "readme", Name: "README.txt", Write: e.WriteReadme},
		{Key: "interval", Name: "interval-15min.csv", Write: e.WriteIntervalCSV},
		{Key: "hourly", Name: "hourly.csv", Write: e.WriteHourlyCSV},
		{Key: "daily", Name: "daily.csv", Write: e.WriteDailyCSV},
		{Key: "monthly", Name: "monthly.csv", Write: e.WriteMonthlyCSV},
		{Key: "cost", Name: "electricity-cost.csv", Write: e.WriteCostCSV},
	}
}

func (e Export) EVDevice() (ExportDevice, bool) {
	if e.ev < 0 {
		return ExportDevice{}, false
	}
	return e.Devices[e.ev], true
}

func (e Export) sources() []database.EnergySourceRef {
	refs := make([]database.EnergySourceRef, 0, len(e.Devices)+1)
	refs = append(refs, e.Main)
	for _, device := range e.Devices {
		refs = append(refs, device.Source)
	}
	return refs
}

func (e Export) nextDay(day time.Time) time.Time {
	return time.Date(day.Year(), day.Month(), day.Day()+1, 0, 0, 0, 0, e.location)
}

func (e Export) expectedSamples(start, end time.Time) float64 {
	return end.Sub(start).Seconds() / float64(e.config.PollIntervalSeconds)
}

// exportValue distinguishes "no data" from a measured zero.
type exportValue struct {
	value float64
	ok    bool
}

func (v *exportValue) add(x float64) { v.value, v.ok = v.value+x, true }

func (v *exportValue) max(x float64) {
	if !v.ok || x > v.value {
		v.value, v.ok = x, true
	}
}

func (v *exportValue) min(x float64) {
	if !v.ok || x < v.value {
		v.value, v.ok = x, true
	}
}

func (v exportValue) text(scale float64, places int) string {
	if !v.ok {
		return ""
	}
	return strconv.FormatFloat(v.value*scale, 'f', places, 64)
}

func (v exportValue) ratio(divisor float64, places int) string {
	if !v.ok || divisor <= 0 {
		return ""
	}
	return strconv.FormatFloat(v.value/divisor, 'f', places, 64)
}

// averagePower combines aggregate averages weighted by their sample counts.
type averagePower struct {
	sum     float64
	samples int
}

func (p *averagePower) add(aggregate database.EnergyAggregate) {
	if aggregate.AveragePowerW != nil && aggregate.SampleCount > 0 {
		p.sum += *aggregate.AveragePowerW * float64(aggregate.SampleCount)
		p.samples += aggregate.SampleCount
	}
}

func (p averagePower) text() string {
	if p.samples == 0 {
		return ""
	}
	return strconv.FormatFloat(p.sum/float64(p.samples), 'f', 1, 64)
}

func completenessText(samples int, expected float64) string {
	if expected <= 0 {
		return ""
	}
	return strconv.FormatFloat(min(1, float64(samples)/expected)*100, 'f', 1, 64)
}

var qualityRank = map[string]int{QualityMeasured: 0, QualityCalculated: 1, QualityStale: 2, QualityPartial: 2, QualityCounterReset: 3}

func worseQuality(current, next string) string {
	if current == "" || qualityRank[next] > qualityRank[current] {
		return next
	}
	return current
}

type intervalSource struct {
	energy exportValue
	power  averagePower
}

type intervalBucket struct {
	main    intervalSource
	maximum exportValue
	samples int
	quality string
	devices []intervalSource
}

// WriteIntervalCSV uses minute aggregates, so it only reaches back as far as
// minute retention. DST days produce 92 or 100 rows.
func (e Export) WriteIntervalCSV(ctx context.Context, w io.Writer) error {
	return e.writeGridCSV(ctx, w, "minute", exportInterval)
}

// WriteHourlyCSV uses hourly aggregates, which are retained far longer than
// minute data and match the resolution of solar production estimates.
func (e Export) WriteHourlyCSV(ctx context.Context, w io.Writer) error {
	return e.writeGridCSV(ctx, w, "hour", time.Hour)
}

// writeGridCSV streams one day of aggregates at a time into a regular grid of
// the given length, which must be a whole multiple of the aggregate length.
func (e Export) writeGridCSV(ctx context.Context, w io.Writer, aggregateType string, length time.Duration) error {
	out := csv.NewWriter(w)
	header := []string{"interval_start", "interval_end", "data_completeness_pct", "household_quality", "household_consumption_wh", "grid_import_wh", "grid_avg_power_w", "grid_max_power_w"}
	for _, device := range e.Devices {
		header = append(header, device.Column+"_wh", device.Column+"_avg_power_w")
	}
	if err := out.Write(header); err != nil {
		return err
	}
	for day := e.Start; day.Before(e.End); day = e.nextDay(day) {
		dayEnd := e.nextDay(day)
		aggregates, err := e.store.ListEnergyAggregatesForSources(ctx, e.sources(), day, dayEnd, aggregateType)
		if err != nil {
			return err
		}
		buckets := make([]intervalBucket, int((dayEnd.Sub(day)+length-1)/length))
		for index := range buckets {
			buckets[index].devices = make([]intervalSource, len(e.Devices))
		}
		for _, aggregate := range aggregates {
			index := int(aggregate.IntervalStart.Sub(day) / length)
			if aggregate.IntervalStart.Before(day) || index >= len(buckets) {
				continue
			}
			bucket := &buckets[index]
			ref := database.EnergySourceRef{DeviceID: aggregate.DeviceID, ComponentKey: aggregate.ComponentKey}
			if ref == e.Main {
				bucket.main.energy.add(aggregate.EnergyConsumedWh)
				bucket.main.power.add(aggregate)
				bucket.samples += aggregate.SampleCount
				bucket.quality = worseQuality(bucket.quality, aggregate.CalculationQuality)
				if aggregate.MaximumPowerW != nil {
					bucket.maximum.max(*aggregate.MaximumPowerW)
				}
			} else if device, found := e.devices[ref]; found {
				bucket.devices[device].energy.add(aggregate.EnergyConsumedWh)
				bucket.devices[device].power.add(aggregate)
			}
		}
		for index, bucket := range buckets {
			start := day.Add(time.Duration(index) * length)
			end := start.Add(length)
			if end.After(dayEnd) {
				end = dayEnd
			}
			quality := bucket.quality
			if !bucket.main.energy.ok {
				quality = "missing"
			}
			row := []string{start.Format(exportTimestampLayout), end.Format(exportTimestampLayout), completenessText(bucket.samples, e.expectedSamples(start, end)), quality, bucket.main.energy.text(1, 1), bucket.main.energy.text(1, 1), bucket.main.power.text(), bucket.maximum.text(1, 1)}
			for _, device := range bucket.devices {
				row = append(row, device.energy.text(1, 1), device.power.text())
			}
			if err := out.Write(row); err != nil {
				return err
			}
		}
		if out.Flush(); out.Error() != nil {
			return out.Error()
		}
	}
	out.Flush()
	return out.Error()
}

type exportDay struct {
	Start, End      time.Time
	Samples         int
	ExpectedSamples float64
	Household       exportValue
	Daytime         exportValue
	Nighttime       exportValue
	NightMinPower   exportValue
	PeakPower       exportValue
	Devices         []exportValue
	DevicesDaytime  []exportValue
	EVChargingMins  exportValue
	EVSessions      exportValue
}

func (d exportDay) completeness() float64 {
	if d.ExpectedSamples <= 0 {
		return 0
	}
	return min(1, float64(d.Samples)/d.ExpectedSamples)
}

func (d exportDay) weekend() bool {
	return d.Start.Weekday() == time.Saturday || d.Start.Weekday() == time.Sunday
}

// eachDay builds day statistics from hourly aggregates (retained longer than
// minute data). EV sessions need minute data and stay blank without it.
func (e Export) eachDay(ctx context.Context, visit func(exportDay) error) error {
	var lastCharging time.Time
	for day := e.Start; day.Before(e.End); day = e.nextDay(day) {
		dayEnd := e.nextDay(day)
		hours, err := e.store.ListEnergyAggregatesForSources(ctx, e.sources(), day, dayEnd, "hour")
		if err != nil {
			return err
		}
		stats := exportDay{Start: day, End: dayEnd, ExpectedSamples: e.expectedSamples(day, dayEnd), Devices: make([]exportValue, len(e.Devices)), DevicesDaytime: make([]exportValue, len(e.Devices))}
		for _, hour := range hours {
			if hour.IntervalStart.Before(day) || !hour.IntervalStart.Before(dayEnd) {
				continue
			}
			ref := database.EnergySourceRef{DeviceID: hour.DeviceID, ComponentKey: hour.ComponentKey}
			localHour := hour.IntervalStart.In(e.location).Hour()
			daytime := localHour >= exportDaytimeStart && localHour < exportDaytimeEnd
			if device, found := e.devices[ref]; found {
				stats.Devices[device].add(hour.EnergyConsumedWh)
				if daytime {
					stats.DevicesDaytime[device].add(hour.EnergyConsumedWh)
				}
				continue
			}
			if ref != e.Main {
				continue
			}
			stats.Household.add(hour.EnergyConsumedWh)
			stats.Samples += hour.SampleCount
			if daytime {
				stats.Daytime.add(hour.EnergyConsumedWh)
			} else {
				stats.Nighttime.add(hour.EnergyConsumedWh)
				if hour.AveragePowerW != nil {
					stats.NightMinPower.min(*hour.AveragePowerW)
				}
			}
			if hour.MaximumPowerW != nil {
				stats.PeakPower.max(*hour.MaximumPowerW)
			}
		}
		if e.ev >= 0 {
			ev := e.Devices[e.ev].Source
			minutes, err := e.store.ListEnergyAggregates(ctx, ev.DeviceID, ev.ComponentKey, day, dayEnd, "minute")
			if err != nil {
				return err
			}
			if len(minutes) > 0 {
				stats.EVChargingMins.add(0)
				stats.EVSessions.add(0)
			}
			for _, minute := range minutes {
				if minute.AveragePowerW == nil || *minute.AveragePowerW < evChargingThresholdW {
					continue
				}
				if lastCharging.IsZero() || minute.IntervalStart.Sub(lastCharging) > evSessionGap {
					stats.EVSessions.add(1)
				}
				stats.EVChargingMins.add(1)
				lastCharging = minute.IntervalStart
			}
		}
		if err := visit(stats); err != nil {
			return err
		}
	}
	return nil
}

func (e Export) WriteDailyCSV(ctx context.Context, w io.Writer) error {
	out := csv.NewWriter(w)
	header := []string{"date", "weekday", "data_completeness_pct", "household_consumption_kwh", "daytime_consumption_kwh", "nighttime_consumption_kwh", "night_min_hourly_avg_power_w", "peak_power_w"}
	if e.ev >= 0 {
		header = append(header, "ev_charging_kwh", "ev_charging_daytime_kwh", "ev_charging_hours", "ev_sessions_started")
	}
	for _, device := range e.Devices {
		header = append(header, device.Column+"_kwh")
	}
	if err := out.Write(header); err != nil {
		return err
	}
	err := e.eachDay(ctx, func(day exportDay) error {
		row := []string{day.Start.Format("2006-01-02"), day.Start.Weekday().String(), completenessText(day.Samples, day.ExpectedSamples), day.Household.text(0.001, 3), day.Daytime.text(0.001, 3), day.Nighttime.text(0.001, 3), day.NightMinPower.text(1, 1), day.PeakPower.text(1, 1)}
		if e.ev >= 0 {
			row = append(row, day.Devices[e.ev].text(0.001, 3), day.DevicesDaytime[e.ev].text(0.001, 3), day.EVChargingMins.text(1.0/60, 2), day.EVSessions.text(1, 0))
		}
		for _, device := range day.Devices {
			row = append(row, device.text(0.001, 3))
		}
		if err := out.Write(row); err != nil {
			return err
		}
		out.Flush()
		return out.Error()
	})
	if err != nil {
		return err
	}
	out.Flush()
	return out.Error()
}

type exportMonth struct {
	start                                     time.Time
	days, completeDays, weekdays, weekends    int
	samples                                   int
	expected                                  float64
	household, peak, evSessions               exportValue
	completeTotal, completeDay, completeNight exportValue
	weekdayTotal, weekendTotal                exportValue
	devices, devicesDaytime                   []exportValue
}

func (e Export) WriteMonthlyCSV(ctx context.Context, w io.Writer) error {
	out := csv.NewWriter(w)
	header := []string{"month", "days_in_period", "complete_days", "data_completeness_pct", "household_consumption_kwh", "grid_import_kwh", "avg_daily_consumption_kwh", "avg_daily_daytime_consumption_kwh", "avg_daily_nighttime_consumption_kwh", "avg_weekday_consumption_kwh_per_day", "avg_weekend_consumption_kwh_per_day", "peak_power_w"}
	if e.ev >= 0 {
		header = append(header, "ev_charging_kwh", "ev_charging_daytime_kwh", "ev_sessions_started")
	}
	for _, device := range e.Devices {
		header = append(header, device.Column+"_kwh", device.Column+"_daytime_kwh")
	}
	if err := out.Write(header); err != nil {
		return err
	}
	var month *exportMonth
	flush := func() error {
		if month == nil {
			return nil
		}
		row := []string{month.start.Format("2006-01"), strconv.Itoa(month.days), strconv.Itoa(month.completeDays), completenessText(month.samples, month.expected), month.household.text(0.001, 3), month.household.text(0.001, 3), month.completeTotal.ratio(1000*float64(month.completeDays), 3), month.completeDay.ratio(1000*float64(month.completeDays), 3), month.completeNight.ratio(1000*float64(month.completeDays), 3), month.weekdayTotal.ratio(1000*float64(month.weekdays), 3), month.weekendTotal.ratio(1000*float64(month.weekends), 3), month.peak.text(1, 1)}
		if e.ev >= 0 {
			row = append(row, month.devices[e.ev].text(0.001, 3), month.devicesDaytime[e.ev].text(0.001, 3), month.evSessions.text(1, 0))
		}
		for index, device := range month.devices {
			row = append(row, device.text(0.001, 3), month.devicesDaytime[index].text(0.001, 3))
		}
		if err := out.Write(row); err != nil {
			return err
		}
		out.Flush()
		return out.Error()
	}
	err := e.eachDay(ctx, func(day exportDay) error {
		if month == nil || day.Start.Month() != month.start.Month() || day.Start.Year() != month.start.Year() {
			if err := flush(); err != nil {
				return err
			}
			month = &exportMonth{start: day.Start, devices: make([]exportValue, len(e.Devices)), devicesDaytime: make([]exportValue, len(e.Devices))}
		}
		month.days++
		month.samples += day.Samples
		month.expected += day.ExpectedSamples
		if day.Household.ok {
			month.household.add(day.Household.value)
		}
		if day.PeakPower.ok {
			month.peak.max(day.PeakPower.value)
		}
		if day.EVSessions.ok {
			month.evSessions.add(day.EVSessions.value)
		}
		for index, device := range day.Devices {
			if device.ok {
				month.devices[index].add(device.value)
			}
			if daytime := day.DevicesDaytime[index]; daytime.ok {
				month.devicesDaytime[index].add(daytime.value)
			}
		}
		// Averages use complete days only so partial first/last days and
		// collector outages do not drag the typical day down.
		if day.completeness() >= exportCompleteDay {
			month.completeDays++
			month.completeTotal.add(day.Household.value)
			month.completeDay.add(day.Daytime.value)
			month.completeNight.add(day.Nighttime.value)
			if day.weekend() {
				month.weekends++
				month.weekendTotal.add(day.Household.value)
			} else {
				month.weekdays++
				month.weekdayTotal.add(day.Household.value)
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	if err := flush(); err != nil {
		return err
	}
	out.Flush()
	return out.Error()
}

// WriteCostCSV prices the main meter's imports with BuildReport, one calendar
// month at a time. BuildReport seeds each billing cycle's VAT allowance from
// stored history, so month chunks match a single long report.
func (e Export) WriteCostCSV(ctx context.Context, w io.Writer) error {
	out := csv.NewWriter(w)
	if err := out.Write([]string{"month", "grid_import_kwh", "reduced_vat_kwh", "standard_vat_kwh", "energy_net_eur", "energy_vat_eur", "excise_tax_net_eur", "excise_tax_vat_eur", "variable_cost_incl_vat_eur", "avg_import_price_eur_per_kwh"}); err != nil {
		return err
	}
	row := func(label string, cost pricing.EnergyCostBreakdown) []string {
		price := ""
		if cost.ConsumptionKWh.IsPositive() {
			price = cost.TotalIncludingVAT.Div(cost.ConsumptionKWh).Round(6).StringFixed(6)
		}
		return []string{label, cost.ConsumptionKWh.Round(3).StringFixed(3), cost.ReducedRateKWh.Round(3).StringFixed(3), cost.StandardRateKWh.Round(3).StringFixed(3), cost.EnergyNetTotal.Round(4).StringFixed(4), cost.EnergyVATTotal.Round(4).StringFixed(4), cost.ExciseTaxNet.Round(4).StringFixed(4), cost.ExciseTaxVAT.Round(4).StringFixed(4), cost.TotalIncludingVAT.Round(4).StringFixed(4), price}
	}
	total := pricing.EnergyCostBreakdown{}
	for start := e.Start; start.Before(e.End); {
		end := time.Date(start.Year(), start.Month()+1, 1, 0, 0, 0, 0, e.location)
		if end.After(e.End) {
			end = e.End
		}
		report, err := BuildReport(ctx, e.store, e.config, start, end, "day")
		if err != nil {
			return err
		}
		total = addCost(total, report.Cost)
		if err := out.Write(row(start.Format("2006-01"), report.Cost)); err != nil {
			return err
		}
		start = end
	}
	if err := out.Write(row("TOTAL", total)); err != nil {
		return err
	}
	out.Flush()
	return out.Error()
}

func exportSlug(value string) string {
	var builder strings.Builder
	underscore := false
	for _, character := range strings.ToLower(value) {
		if (character >= 'a' && character <= 'z') || (character >= '0' && character <= '9') {
			builder.WriteRune(character)
			underscore = false
		} else if !underscore && builder.Len() > 0 {
			builder.WriteByte('_')
			underscore = true
		}
	}
	slug := strings.TrimRight(builder.String(), "_")
	if slug == "" {
		return "unnamed"
	}
	return slug
}
