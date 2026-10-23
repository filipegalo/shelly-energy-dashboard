package energy

import (
	"context"
	"fmt"
	"io"
	"sort"
	"strings"
	"text/tabwriter"
)

func (e Export) WriteReadme(ctx context.Context, w io.Writer) error {
	versions, err := e.store.ListPricingVersions(ctx)
	if err != nil {
		return err
	}
	sort.Slice(versions, func(i, j int) bool { return versions[i].PriceEffectiveFrom.Before(versions[j].PriceEffectiveFrom) })
	applicable := make([]string, 0, len(versions))
	for index, version := range versions {
		if !version.PriceEffectiveFrom.Before(e.End) || (index+1 < len(versions) && !versions[index+1].PriceEffectiveFrom.After(e.Start)) {
			continue
		}
		applicable = append(applicable, fmt.Sprintf("  - from %s: energy %s %s/kWh excl. VAT; reduced VAT %s (enabled=%t, %s kWh per 30 days, large family=%t); standard VAT %s; excise tax %s/kWh (included=%t, VAT %s); region %s",
			version.PriceEffectiveFrom.In(e.location).Format(exportTimestampLayout), version.EnergyPricePerKWhExcludingVAT, version.Currency, version.ReducedVATRate, version.ReducedVATEnabled, version.StandardReducedVATLimitKWhPer30Days, version.LargeFamilyEnabled, version.StandardVATRate, version.ElectricityExciseTaxPerKWh, version.IncludeExciseTax, version.ElectricityExciseTaxVATRate, version.Region))
	}

	var b strings.Builder
	line := func(format string, args ...any) { fmt.Fprintf(&b, format+"\n", args...) }
	line("SHELLY ENERGY EXPORT — historical data for solar PV / battery sizing")
	line("")
	line("Generated:      %s", e.GeneratedAt.Format(exportTimestampLayout))
	line("Period:         %s to %s inclusive (whole local days)", e.Start.Format("2006-01-02"), e.End.AddDate(0, 0, -1).Format("2006-01-02"))
	line("Timezone:       %s. Timestamps are ISO 8601 with UTC offset, so DST changes are unambiguous", e.config.Timezone)
	line("                (spring-forward days have 92 15-minute rows, fall-back days 100).")
	line("Main meter:     device %d, component %s (%s), polled every %d s (the configured whole-house meter).", e.Main.DeviceID, e.Main.ComponentKey, e.config.ComponentType, e.config.PollIntervalSeconds)
	line("Retention:      minute data %d days, hourly data %d days, daily data indefinitely (Energy settings).", e.config.MinuteRetentionDays, e.config.HourlyRetentionDays)
	line("")
	line("FILES")
	line("  interval-15min.csv    15-minute grid: household/grid import energy, grid power, per-device energy and power")
	line("  hourly.csv            same columns on an hourly grid, from hourly data (kept much longer than minute data);")
	line("                        the natural resolution to compare against hourly solar production estimates")
	line("  daily.csv             one row per local day: totals, day/night split, base load, peak, EV, per-device energy")
	line("  monthly.csv           one row per calendar month (clipped to the period): totals, averages, peak, EV, per-device")
	line("  electricity-cost.csv  one row per calendar month plus TOTAL: grid import energy and variable cost")
	line("")
	line("UNITS")
	line("  *_wh = watt-hours, *_kwh = kilowatt-hours, *_w = watts, *_eur = euros, *_pct = percent (0-100).")
	line("  Decimal separator is '.', field separator is ','. An empty field means NO DATA — never zero.")
	line("")
	line("DATA SOURCES AND MEASURED vs DERIVED VALUES")
	line("  Measured")
	line("    - Energy columns (*_wh, *_kwh) are differences of each meter's cumulative import counter, summed from")
	line("      stored minute/hour aggregates. Power readings are never summed to obtain energy. When a meter has no")
	line("      counter the collector integrates power between consecutive polls; those intervals are marked")
	line("      household_quality=calculated in interval-15min.csv.")
	line("    - grid_avg_power_w / *_avg_power_w: sample-weighted mean of polled active power over the interval.")
	line("    - grid_max_power_w / peak_power_w: highest polled power sample (every %d s for the main meter). Short", e.config.PollIntervalSeconds)
	line("      spikes between polls are not captured.")
	line("  Derived")
	line("    - household_consumption_*: equal to grid import. This assumes no on-site generation: the collector")
	line("      rejects negative (exporting) power readings today, so it cannot record generation. Revisit once PV")
	line("      is installed.")
	line("    - daytime_* = local hours 08:00-20:00, nighttime_* = 00:00-08:00 + 20:00-24:00 of the same date.")
	line("      daytime + nighttime = household total. ev_charging_daytime_kwh and device_*_daytime_kwh use the same")
	line("      08:00-20:00 window: the part of that load already running in solar hours. The rest would need")
	line("      shifting or a battery to be covered by solar.")
	line("    - night_min_hourly_avg_power_w: lowest hourly average power in the nighttime hours (base-load proxy).")
	line("    - data_completeness_pct: main-meter samples received / samples expected at the configured poll interval.")
	line("    - complete_days: days with >= %.0f%% completeness. Monthly averages use only these days; monthly totals", exportCompleteDay*100)
	line("      use all data. avg_weekday/avg_weekend are per complete weekday/weekend day.")
	if device, ok := e.EVDevice(); ok {
		line("    - ev_*: the EV charger was identified by device name: %q (device %d, %s).", device.Name, device.Source.DeviceID, device.Source.ComponentKey)
		line("      ev_charging_kwh is that device's measured energy (also present in its device column).")
		line("      ev_charging_hours counts minutes whose average power >= %.0f W. A session starts at the first such", evChargingThresholdW)
		line("      minute after a gap of more than %.0f minutes; sessions are counted on the day they start. Both need", evSessionGap.Minutes())
		line("      minute data and are empty for days outside minute retention.")
	} else {
		line("    - No EV charger could be identified by device name, so EV columns are omitted.")
	}
	line("")
	line("DEVICES (per-device columns; rooms are the only category data that exists)")
	if len(e.Devices) == 0 {
		line("  No room energy sources were assigned during this period.")
	} else {
		table := tabwriter.NewWriter(&b, 2, 4, 2, ' ', 0)
		fmt.Fprintln(table, "  column\tdevice name\troom\trole\tmodel\tsource")
		for _, device := range e.Devices {
			fmt.Fprintf(table, "  %s\t%s\t%s\t%s\t%s\tdevice %d %s\n", device.Column, device.Name, device.Room, device.Role, device.Model, device.Source.DeviceID, device.Source.ComponentKey)
		}
		table.Flush()
		line("  Device columns are a breakdown measured by separate Shelly devices. They are NOT added into household")
		line("  totals (those come only from the main meter), and the main meter itself is excluded from this list.")
		line("  A 'submeter' is nested inside its parent device; do not add a submeter to its parent. The rest of the")
		line("  household (household minus primary devices) is unmetered.")
	}
	line("")
	line("ELECTRICITY COST (electricity-cost.csv)")
	line("  Uses the tariff versions configured in the app, applied at each interval's time:")
	if len(applicable) == 0 {
		line("  - none configured")
	}
	for _, text := range applicable {
		line("%s", text)
	}
	line("  - Single flat energy price. The app has no time-of-use periods, so no time-of-use pricing is applied.")
	line("  - Portuguese reduced VAT applies to the first N kWh of each billing cycle (prorated per 30 days, cycle")
	line("    starts on day %d), the rest at standard VAT. This is why average price per kWh varies by month.", e.config.BillingCycleStartDay)
	line("  - EXCLUDED: fixed charges (contracted power / daily standing charge), fees and taxes other than VAT and")
	line("    excise tax (e.g. audiovisual contribution, DGEG fee), discounts and invoice adjustments. Those are not")
	line("    configured in the app. Use the Reports page invoice reconciliation to compare against real bills.")
	line("  - EXCLUDED: export compensation. No feed-in tariff is configured, and export is not recorded (see below).")
	line("")
	line("LIMITATIONS")
	line("  - Grid EXPORT is not available historically. The main meter reports an export counter, but it is only")
	line("    kept in raw samples (%d-day retention) and is not aggregated, so no export columns are included.", e.config.SampleRetentionDays)
	line("  - 15-minute data comes from minute aggregates, kept for %d days. Older 15-minute rows are blank", e.config.MinuteRetentionDays)
	line("    (household_quality=missing). To keep a full year at 15-minute resolution, raise 'minute retention'")
	line("    in Energy settings to >= 400 days before data ages out. Hourly, daily and monthly files use hourly")
	line("    data (kept %d days).", e.config.HourlyRetentionDays)
	line("  - Collection gaps: missing samples are not interpolated. Because energy comes from cumulative counters,")
	line("    energy consumed during a short outage is booked to the first interval after collection resumes.")
	line("    Totals stay correct, but that interval is overstated and the gap intervals are blank or low. Check")
	line("    data_completeness_pct and household_quality.")
	line("  - Counter resets (meter reboot or firmware update): the energy between the last reading before the")
	line("    reset and the first one after it cannot be recovered. Those intervals are marked counter_reset.")
	line("  - Duplicate timestamps are rejected by unique constraints on samples and aggregates.")
	line("  - Per-device data is polled less often than the main meter and has its own gaps. Device sums will not")
	line("    exactly match household totals.")
	_, err = io.WriteString(w, b.String())
	return err
}
