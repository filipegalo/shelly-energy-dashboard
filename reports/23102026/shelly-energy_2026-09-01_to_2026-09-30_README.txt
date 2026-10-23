SHELLY ENERGY EXPORT — historical data for solar PV / battery sizing

Generated:      2026-10-23T11:12:24+01:00
Period:         2026-09-01 to 2026-09-30 inclusive (whole local days)
Timezone:       Europe/Lisbon. Timestamps are ISO 8601 with UTC offset, so DST changes are unambiguous
                (spring-forward days have 92 15-minute rows, fall-back days 100).
Main meter:     device 5, component em1:0 (em1), polled every 10 s (the configured whole-house meter).
Retention:      minute data 90 days, hourly data 730 days, daily data indefinitely (Energy settings).

FILES
  interval-15min.csv    15-minute grid: household/grid import energy, grid power, per-device energy and power
  hourly.csv            same columns on an hourly grid, from hourly data (kept much longer than minute data);
                        the natural resolution to compare against hourly solar production estimates
  daily.csv             one row per local day: totals, day/night split, base load, peak, EV, per-device energy
  monthly.csv           one row per calendar month (clipped to the period): totals, averages, peak, EV, per-device
  electricity-cost.csv  one row per calendar month plus TOTAL: grid import energy and variable cost

UNITS
  *_wh = watt-hours, *_kwh = kilowatt-hours, *_w = watts, *_eur = euros, *_pct = percent (0-100).
  Decimal separator is '.', field separator is ','. An empty field means NO DATA — never zero.

DATA SOURCES AND MEASURED vs DERIVED VALUES
  Measured
    - Energy columns (*_wh, *_kwh) are differences of each meter's cumulative import counter, summed from
      stored minute/hour aggregates. Power readings are never summed to obtain energy. When a meter has no
      counter the collector integrates power between consecutive polls; those intervals are marked
      household_quality=calculated in interval-15min.csv.
    - grid_avg_power_w / *_avg_power_w: sample-weighted mean of polled active power over the interval.
    - grid_max_power_w / peak_power_w: highest polled power sample (every 10 s for the main meter). Short
      spikes between polls are not captured.
  Derived
    - household_consumption_*: equal to grid import. This assumes no on-site generation: the collector
      rejects negative (exporting) power readings today, so it cannot record generation. Revisit once PV
      is installed.
    - daytime_* = local hours 08:00-20:00, nighttime_* = 00:00-08:00 + 20:00-24:00 of the same date.
      daytime + nighttime = household total. ev_charging_daytime_kwh and device_*_daytime_kwh use the same
      08:00-20:00 window: the part of that load already running in solar hours. The rest would need
      shifting or a battery to be covered by solar.
    - night_min_hourly_avg_power_w: lowest hourly average power in the nighttime hours (base-load proxy).
    - data_completeness_pct: main-meter samples received / samples expected at the configured poll interval.
    - complete_days: days with >= 90% completeness. Monthly averages use only these days; monthly totals
      use all data. avg_weekday/avg_weekend are per complete weekday/weekend day.
    - ev_*: the EV charger was identified by device name: "Shelly - Garage Car Charger" (device 4, switch:0).
      ev_charging_kwh is that device's measured energy (also present in its device column).
      ev_charging_hours counts minutes whose average power >= 1000 W. A session starts at the first such
      minute after a gap of more than 10 minutes; sessions are counted on the day they start. Both need
      minute data and are empty for days outside minute retention.

DEVICES (per-device columns; rooms are the only category data that exists)
  column                                      device name                              room         role     model          source
  device_shelly_entrance_lights               Shelly - Entrance Lights                 Entrance     primary  S3SW-001P8EU   device 1 switch:0
  device_shelly_garage_all_lights             Shelly - Garage All Lights               Garage       primary  S3SW-001P8EU   device 7 switch:0
  device_shelly_garage_car_charger            Shelly - Garage Car Charger              Garage       primary  S3SW-001P16EU  device 4 switch:0
  device_shelly_garage_tests                  Shelly - Garage Tests                    Garage       primary  S3SW-001P8EU   device 16 switch:0
  device_shelly_kitchen_oven_stove            Shelly - Kitchen Oven/Stove              Kitchen      primary  S3SW-001P16EU  device 8 switch:0
  device_shelly_kitchen_sockets               Shelly - Kitchen Sockets                 Kitchen      primary  S3SW-001P16EU  device 3 switch:0
  device_shelly_kitchen_washing_machine       Shelly - Kitchen Washing Machine         Kitchen      primary  S3SW-001P16EU  device 10 switch:0
  device_shelly_laundry_machines              Shelly - Laundry Machines                Laundry      primary  S3SW-001P16EU  device 6 switch:0
  device_shelly_living_room_fireplace         Shelly - Living Room Fireplace           Living Room  primary  S3SW-001P8EU   device 15 switch:0
  device_shelly_wc_towel_rack                 Shelly - WC Towel Rack                   Main WC      primary  S3SW-001P8EU   device 2 switch:0
  device_shelly_office_outlet_ac              Shelly - Office Outlet AC                Office       primary  S3SW-001P8EU   device 12 switch:0
  device_shelly_office_outlet_server_network  Shelly - Office Outlet Server / Network  Office       primary  S3SW-001P8EU   device 13 switch:0
  device_shelly_exterior_garden_lights        Shelly - Exterior Garden Lights          Outside      primary  S3SW-001P8EU   device 17 switch:0
  device_shelly_outside_entrance_lights       Shelly - Outside Entrance Lights         Outside      primary  S3SW-001P8EU   device 11 switch:0
  Device columns are a breakdown measured by separate Shelly devices. They are NOT added into household
  totals (those come only from the main meter), and the main meter itself is excluded from this list.
  A 'submeter' is nested inside its parent device; do not add a submeter to its parent. The rest of the
  household (household minus primary devices) is unmetered.

ELECTRICITY COST (electricity-cost.csv)
  Uses the tariff versions configured in the app, applied at each interval's time:
  - from 2026-07-21T19:24:16+01:00: energy 0.137378 EUR/kWh excl. VAT; reduced VAT 0.06 (enabled=true, 200 kWh per 30 days, large family=false); standard VAT 0.23; excise tax 0.001000/kWh (included=true, VAT 0.23); region Portugal Continental
  - Single flat energy price. The app has no time-of-use periods, so no time-of-use pricing is applied.
  - Portuguese reduced VAT applies to the first N kWh of each billing cycle (prorated per 30 days, cycle
    starts on day 11), the rest at standard VAT. This is why average price per kWh varies by month.
  - EXCLUDED: fixed charges (contracted power / daily standing charge), fees and taxes other than VAT and
    excise tax (e.g. audiovisual contribution, DGEG fee), discounts and invoice adjustments. Those are not
    configured in the app. Use the Reports page invoice reconciliation to compare against real bills.
  - EXCLUDED: export compensation. No feed-in tariff is configured, and export is not recorded (see below).

LIMITATIONS
  - Grid EXPORT is not available historically. The main meter reports an export counter, but it is only
    kept in raw samples (7-day retention) and is not aggregated, so no export columns are included.
  - 15-minute data comes from minute aggregates, kept for 90 days. Older 15-minute rows are blank
    (household_quality=missing). To keep a full year at 15-minute resolution, raise 'minute retention'
    in Energy settings to >= 400 days before data ages out. Hourly, daily and monthly files use hourly
    data (kept 730 days).
  - Collection gaps: missing samples are not interpolated. Because energy comes from cumulative counters,
    energy consumed during a short outage is booked to the first interval after collection resumes.
    Totals stay correct, but that interval is overstated and the gap intervals are blank or low. Check
    data_completeness_pct and household_quality.
  - Counter resets (meter reboot or firmware update): the energy between the last reading before the
    reset and the first one after it cannot be recovered. Those intervals are marked counter_reset.
  - Duplicate timestamps are rejected by unique constraints on samples and aggregates.
  - Per-device data is polled less often than the main meter and has its own gaps. Device sums will not
    exactly match household totals.
