# Solar PV Feasibility Report: Alhandra / São João dos Montes (August–September 2026 data)

**Date:** 23/10/2026 (updated with the exact roof coordinates)
**Data:** Shelly energy export, 1 August – 30 September 2026 (61 days, two summer months)
**Location:** roof at 38.950780 N, −9.037919 E (São João dos Montes, Vila Franca de Xira), altitude about 125 m
**Replaces:** the 23/10/2026 report based on September only

> **How to read this report.** August and September figures are **measured**. Annual figures are **modelled scenarios** extrapolated from two summer months of consumption data. Costs and export prices are **market indications, not quotes**.

---

## 1. Recommendation

**Install about 4 kWp, split between the south and west roof sections, without a battery for now, and set the EV to charge from solar.**

- **Payback:** about **12 years** at mid-range 2026 installed prices, saving about **€545/yr**. If at least half of each day's EV charging moves into daylight, payback drops to about **9.5 years** and savings rise to about €685/yr.
- **If you buy the equipment and a licensed electrician connects and registers it** (about €2,400–3,400 instead of €6,500), payback would be about **4.5–6 years**.
- **Why not 3 kWp:** its payback is slightly shorter (11.4 years) but it saves less in total. On its own, the 4th kWp pays back in about 14.5 years, within a 25-year panel life.
- **Why not 5 kWp or more:** each extra kWp mostly adds low-paid export. On its own, the 5th kWp pays back in about 17 years, the 6th in about 19 and the 7th–8th in about 22. Consider 5 kWp only if the car is reliably at home and charging during the day.
- **Keep the inverter at 4 kW or below.** Above 4 kW, Portuguese rules require metering of all production at the owner's cost. With panels split south/west, a 4 kW inverter loses almost nothing even with 5 kWp of panels.
- **Batteries are not justified at current prices.** Payback on the extra cost is about 17 years for 5 kWh and 19 years for 10 kWh, longer than a typical 10-year battery warranty.
- **If roof space is limited, put panels on the south section first.** In December it produces about 65% more than the west section (see section 3).

**What changed with August and the exact location:**
- With August added, estimated annual consumption fell from about 9,230 to about **8,725 kWh/yr**, which lowers savings by about €30/yr.
- The exact coordinates confirm that **the surrounding terrain does not shade the roof** (it costs less than 0.2% of output) and slightly raise the yield estimate.
- **The recommendation is unchanged.** Using August only or September only as the demand base gives a narrow payback range for 4 kWp (11.2–12.5 years).

---

## 2. What the data shows (measured)

### The export

- Covers 1 August – 30 September 2026, Europe/Lisbon time (UTC+1 throughout, so no clock change).
- The main meter is read every 10 seconds; the files give 15-minute, hourly, daily and monthly totals.
- Household consumption equals grid import, because there is no generation and no export was recorded.
- The September rows are identical to the earlier September-only export.

### Consumption

| Measure | August | September | Both months |
|---|---|---|---|
| Total consumption | **701.5 kWh** | **756.5 kWh** | **1,458.0 kWh** |
| Variable cost (app calculation) | €112.74 | €124.10 | €236.84 |
| Daily average (all days) | 22.6 kWh | 25.2 kWh | 23.9 kWh |
| Daily range (standard deviation) | 6.5–47.2 kWh (9.0) | 17.6–37.0 kWh (4.9) | 6.5–47.2 kWh (7.4) |
| Weekday vs weekend, complete days only | 23.3 vs 22.4 kWh | 25.2 vs 23.3 kWh | No meaningful difference |
| Daytime (08:00–20:00) share | 50% | 49% | 50% |
| Night-time base load, excluding EV (03:00–07:00) | about 295 W | about 375 W | about 335 W |
| Peak demand (10-second reading) | 8.81 kW (5 Aug) | 8.87 kW (25 Sep) | 8.87 kW |
| Highest 15-minute average | 5.76 kW | 5.42 kW | 5.76 kW |
| EV charging | 296.7 kWh (42%), 43 sessions | 275.1 kWh (36%), 48 sessions | **571.8 kWh (39%)** |
| EV charging in daytime | 40% | 34% | 37% |

### Low-consumption days (probably holidays or absences)

- 7–8 August and 13–15 August used only 6.5–15 kWh/day, with almost no EV charging. 15 August is a public holiday.
- These are real behaviour and are kept in the analysis, which roughly matches holiday time in a typical year.

### Daily load pattern (average watts by hour)

| Hour | 0 | 1 | 2 | 3–8 | 9 | 10 | 11 | 12–16 | 17–20 | 21 | 22 | 23 |
|---|---|---|---|---|---|---|---|---|---|---|---|---|
| August | 1,754 | 1,226 | 673 | 285–434 | 380 | 467 | 474 | 955–1,235 | 1,154–1,374 | 1,099 | 1,927 | 1,897 |
| September | 1,749 | 1,013 | 560 | 356–393 | 947 | 1,168 | 973 | 960–1,266 | 1,101–1,293 | 1,592 | 2,431 | 2,263 |

- Overnight (03:00–08:00): about 290–390 W.
- **August mornings (09:00–11:00) are much lower than September's** (about 400 W vs about 1 kW). Office AC also used far less in August (13.5 vs 55 kWh), which suggests less home-office use.
- Evening peak (22:00–01:00): about 1.7–2.4 kW, mostly EV charging.
- Excluding the EV, 57–58% of consumption already falls in daylight hours in both months.

### Consumption by device

Device readings are a breakdown of the household total and are not added to it, so nothing is double-counted.

| Device | August kWh | September kWh | Daytime share (Aug / Sep) |
|---|---|---|---|
| EV charger | 296.7 | 275.1 | 40% / 36% |
| Office server/network (constant ~90–100 W) | 65.1 | 72.2 | 50% / 50% |
| Kitchen sockets (probably the fridge) | 61.8 | 68.8 | 55% / 54% |
| Garage "Tests" (constant ~59 W, unidentified) | 43.8 | 42.4 | 50% / 49% |
| Oven/stove | 32.2 | 28.2 | 68% / 59% |
| Kitchen washing machine | 24.7 | 25.8 | 48% / 39% |
| Laundry machines | 17.1 | 23.6 | 68% / 83% |
| Office air conditioning | 13.5 | 55.0 | 79% / 79% |
| Lights, towel rail, fireplace | 3.0 | 1.0 | n/a |
| Not covered by any device | about 143.7 (20%) | about 164.5 (22%) | n/a |

### Data quality

- No duplicate records.
- **August is much cleaner:** 27 of 31 days are at least 90% complete, against 16 of 30 in September.
- 14 collection outages totalling about 52 hours (3.6% of the period). Only 3 hours were in August; the longest were 15 h on 9–10 Sep and about 9 h each on 6 and 30 Sep.
- After each outage, the meter booked the missed energy into the next interval, up to 19.6 kWh in a single 15-minute slot. Totals are correct, but timing inside the gaps is unknown.
  - About 69 kWh (4.7% of the period) was spread back across the gaps using the typical daily pattern. **That timing is modelled, not measured.**
- 4 intervals in September had a meter counter reset, so a small amount of energy is unrecoverable.
- No export history exists.
- **Retention warning:** 15-minute data is only kept for 90 days, so the early-August 15-minute data will start to disappear around the end of October 2026. This export already preserves it.

---

## 3. The site: sun path and terrain (modelled from the exact coordinates)

### Terrain around the house

- The PVGIS terrain model gives a horizon of only **0–7°** in every direction.
  - Lowest to the southeast (about 0–1.5° between 120° and 150°), which is where the sun rises in winter.
  - Highest to the south-southwest and southwest (about 6–7° between 195° and 225°) and to the north-northeast.
- Comparing PVGIS with and without this horizon, **the terrain costs about 0.04% of annual output on the south section and 0.2% on the west section.** Terrain shading is not a concern.
- **The terrain model does not include buildings, trees or chimneys.** Nearby obstacles still need checking on site.

### The sun through the year

| | 21 Dec (winter) | 20 Mar / 22 Sep (equinoxes) | 21 Jun (summer) |
|---|---|---|---|
| Sunrise / sunset | 07:52 / 17:17 | 06:40 / 18:48 (Mar); 07:24 / 19:33 (Sep) | 06:11 / 21:05 |
| Sunrise / sunset direction | 120° (ESE) / 240° (WSW) | 90° (E) / 271° (W) | 58° (ENE) / 302° (WNW) |
| Daylight | 9.4 h | 12.1 h | 14.9 h |
| Highest sun | **27.6°** at 12:34 | 51° at 12:44 (Mar) / 13:29 (Sep) | **74.5°** at 13:38 |
| Sun above the terrain horizon | 08:05–16:35 (8.5 h) | 07:02–18:25 / 07:46–19:11 (11.4 h) | 06:52–20:29 (13.6 h) |
| Direct sun on the **south** section | 8.5 h (08:05–16:35) | 11.4 h | 12.8 h (07:14–20:02) |
| Direct sun on the **west** section | 6.8 h (from 09:48) | 9.6–9.7 h (from 08:48 / 09:33) | 12.0 h (from 08:32) |

### Typical day of production, 4 kWp (2 kWp south + 2 kWp west, average of 2014–2023)

| Month | South section | West section | Total per day | Cloudy day (10th percentile) | Sunny day (90th percentile) | Best hour |
|---|---|---|---|---|---|---|
| December | 5.1 kWh | 3.1 kWh | **8.2 kWh** | 3.9 kWh | 11.7 kWh | 13:00, about 1.4 kWh |
| March | 8.3 kWh | 6.7 kWh | **14.9 kWh** | 7.2 kWh | 19.8 kWh | 13:00, about 2.1 kWh |
| June | 10.3 kWh | 10.4 kWh | **20.7 kWh** | 13.9 kWh | 24.6 kWh | 14:00, about 2.4 kWh |
| September | 9.3 kWh | 7.8 kWh | **17.1 kWh** | 12.5 kWh | 20.2 kWh | 14:00, about 2.3 kWh |

### What this means for the design

- **In winter the south section does most of the work.** In December the sun peaks at only 27.6°, and the west section gets no direct sun before about 09:50, so it produces about 60% of what the south section does. In June both produce about the same.
- **The west section shifts production into the late afternoon.** In June it still produces about 0.7 kWh between 19:00 and 20:00, against about 0.2 kWh from the south. That overlaps with your 17:00–20:00 consumption, so splitting the panels still makes sense.
- **In winter almost all solar output would be used at home.** A typical December day produces about 8 kWh for a household using about 24 kWh/day. The surplus problem (and the case for moving EV charging or adding a battery) is mainly a spring and summer issue.
- **Rule of thumb for nearby obstacles:** at midday in December (sun at 27.6°), an obstacle to the south casts a shadow about **2× its height**; at 10:00 (sun at about 18°) about **3× its height**. For example, a 5 m tree or building within 10–15 m to the south or southeast of the panels would shade them on winter mornings.
- **Roof tilt matters little.** The best tilt for a south-facing roof here is 33°, which produces only about 1% more than the 25° assumed.

---

## 4. Solar systems compared (modelled)

### Generation estimate (PVGIS)

- Source: PVGIS at the exact roof coordinates, roof-mounted, 2014–2023 hourly weather data, with PVGIS's standard 14% system loss (inverter, cabling, dirt on the panels) plus temperature losses and the terrain horizon.
- Assumed tilt is 25° and the sections are assumed to face due south (180°) and due west (270°). **These are assumptions; measure the actual roof pitch and orientation.**
- Yield per kWp per year: south 1,507 kWh, west 1,283 kWh. Year-to-year variation is about ±3–4%.
- Base case is a 50/50 south/west split.

### Method

- Each year (2014–2023 weather) is simulated chronologically in 15-minute steps.
- Household demand repeats the 61 cleaned August and September days, matching weekdays and weekends.
- Export is valued at €0.03/kWh.

### Results

| | 3 kWp | **4 kWp** | 5 kWp | 6 kWp | 8 kWp |
|---|---|---|---|---|---|
| Annual generation, kWh (range across years) | 4,161 (3,971–4,353) | **5,547** (5,294–5,803) | 6,934 | 8,321 | 11,095 |
| January / July generation, kWh | 211 / 503 | 281 / 671 | 351 / 839 | 422 / 1,007 | 562 / 1,342 |
| Solar used directly, kWh | 2,387 | **2,705** | 2,942 | 3,117 | 3,342 |
| Share of solar used at home | 57% | 49% | 42% | 37% | 30% |
| Share of household need met by solar | 27% | 31% | 34% | 36% | 38% |
| Grid import, kWh (from about 8,725) | 6,338 | 6,020 | 5,783 | 5,608 | 5,384 |
| Export, kWh | 1,774 | 2,843 | 3,992 | 5,204 | 7,753 |
| **With 50% of EV charging moved to daylight:** share of need met by solar | 36% | 42% | 47% | 50% | 53% |

### Monthly generation, kWh (average of 2014–2023, 50/50 south/west)

| kWp | Jan | Feb | Mar | Apr | May | Jun | Jul | Aug | Sep | Oct | Nov | Dec |
|---|---|---|---|---|---|---|---|---|---|---|---|---|
| 3 | 211 | 238 | 347 | 388 | 468 | 465 | 503 | 477 | 385 | 285 | 203 | 190 |
| 4 | 281 | 317 | 463 | 517 | 624 | 620 | 671 | 636 | 513 | 380 | 271 | 253 |
| 5 | 351 | 396 | 579 | 647 | 780 | 776 | 839 | 795 | 642 | 475 | 338 | 317 |
| 6 | 422 | 475 | 695 | 776 | 936 | 931 | 1,007 | 954 | 770 | 570 | 406 | 380 |
| 8 | 562 | 634 | 926 | 1,035 | 1,248 | 1,241 | 1,342 | 1,273 | 1,027 | 760 | 541 | 507 |

### Sensitivity checks

- **Which month the demand profile is based on (4 kWp):**
  - August-only: €518/yr, 12.5-year payback.
  - September-only: €578/yr, 11.2-year payback.
  - Both months pooled: €546/yr, 11.9-year payback.
- **Orientation:** all-south panels add about €10–15/yr; all-west panels cost about €25–30/yr.
- **Higher winter use:** in an illustrative scenario where non-EV winter use is up to 35% higher, savings rise slightly (by €15–30/yr). Not measured.

### Against the actual measured two months (4 kWp)

| | August | September | Both |
|---|---|---|---|
| Generation (PVGIS, same dates) | about 636 kWh | about 513 kWh | about 1,150 kWh (1,101–1,223) |
| Used directly | 273 kWh | 265 kWh | 538 kWh (47%) |
| Exported | 363 kWh | 248 kWh | 612 kWh |
| Share of consumption covered | 39% | 35% | 37% |

- For these two months, the modelled variable bill would have been about €147 instead of about €239 (about €92 less), plus about €18 of export income at €0.03/kWh. The app measured €237 before solar; the small difference is because the app applies the reduced-VAT allowance by billing cycle rather than by calendar month.

---

## 5. Batteries (modelled)

### Assumptions

- 90% of nominal capacity is usable; round-trip efficiency is 90%.
- Power limits: 2.5 kW for the 5 kWh battery, 5 kW for the 10 kWh battery.
- The battery charges only from solar surplus.
- **Extra cost (illustrative, from 4gnews):** 5 kWh about €3,200 (range €2,000–4,200); 10 kWh about €5,500 (range €4,000–6,400).

### Results

| Added to 4 kWp | Extra solar used at home, kWh/yr | Export reduction, kWh | Extra savings | Payback on extra cost: base (range) | Maximum price for 10-year payback |
|---|---|---|---|---|---|
| 5 kWh | 1,403 | 1,559 | €192/yr | **17 yrs** (10–22) | about €1,900 |
| 10 kWh | 2,091 | 2,324 | €286/yr | **19 yrs** (14–22) | about €2,900 |

- On larger systems, battery paybacks are slightly better (15–16 years at 5–8 kWp). Only the lowest cost estimates get close to 10 years.
- **Each stored kWh only saves about €0.14 net:** it avoids roughly €0.17 of grid electricity but gives up €0.03 of export income.
- **Moving EV charging to daytime does part of the battery's job for free.** Once 50% of EV charging is shifted, a 5 kWh battery adds only about €148/yr on a 4 kWp system.
- **In winter there is little surplus to store** (see section 3), so a battery would cycle much less from November to February.

---

## 6. Loads that can move into solar hours

- **EV charging (the only big one).** It averages 9.4 kWh/day and 39% of consumption.
  - Moving 50% of daily EV charging to the same day's sunny hours adds about €140/yr at 4 kWp. Moving 100% (an upper limit) adds about €215/yr.
  - This only works if the car is at home during the day. The data can't confirm that.
  - The charger relay switches a roughly 2.3 kW load on and off. That works for "charge when there's a solar surplus" automation, but it can't follow the surplus smoothly. With 4 kWp, average midday output in summer is about 2–2.4 kW and the house already uses about 1 kW, so the surplus is usually below the charger's 2.3 kW. Part of each solar-charging session would still come from the grid; the model accounts for this.
- **Washing machines (kitchen and laundry).** Together they use about 18–20 kWh/month at night, worth at most about €40/yr if moved to daytime.
- **Office AC.** Already 79% daytime, and summer cooling naturally coincides with solar.
- **Not shiftable:** the server/network, the fridge and the oven.
- **Hot water and heating: not identifiable.** No device measures them. An electric water heater, if there is one, is probably in the 140–165 kWh/month not covered by any device and could be a good candidate for moving to daytime.

---

## 7. Financial comparison

### Tariff assumptions

- Tariff as configured in the Shelly app: €0.137378/kWh before VAT, plus €0.001/kWh excise tax, with 6% VAT on the first 200 kWh per 30 days and 23% on the rest.
- Each kWh no longer imported is charged at 23% VAT, so it is worth about **€0.170**.
- Fixed charges (contracted power, audiovisual contribution, DGEG fee) are excluded from the bill figures because they stay the same after installation.

### Bill and payback, no battery

- Annual variable bill before solar: **about €1,428** (modelled). The measured €236.84 over 61 days annualises to about €1,417.
- Installed prices include 23% VAT. The 6% rate on solar equipment ended on 30 June 2025, and no source was found showing the 2026 budget restored it.

| | 3 kWp | **4 kWp** | 5 kWp | 6 kWp* | 8 kWp* |
|---|---|---|---|---|---|
| Installed cost: base (low–high) | €5,250 (4.5–6.0k) | **€6,500** (5.5–7.5k) | €7,750 (6.5–9.0k) | €9,000 (7.5–10.5k) | €11,500 (9.5–13.5k) |
| Bill after installation | €1,022 | €967 | €927 | €897 | €859 |
| Export income at €0.03/kWh | €53 | €85 | €120 | €156 | €233 |
| **Annual net savings** | €460 | **€546** | €621 | €687 | €801 |
| **Simple payback** | 11.4 yrs | **11.9** | 12.5 | 13.1 | 14.4 |
| Payback with 50% of EV charging moved to daylight | 9.4 | **9.5** | 9.9 | 10.5 | 11.8 |
| Payback at low / high installed cost | 9.8 / 13.1 | 10.1 / 13.7 | 10.5 / 14.5 | 10.9 / 15.3 | 11.9 / 16.8 |
| Payback with no export income | 12.9 | 14.1 | 15.5 | 17.0 | 20.2 |
| Payback with export at €0.05/kWh | 10.6 | 10.8 | 11.1 | 11.4 | 12.0 |
| Payback with grid prices −20% / +20% | 13.9 / 9.7 | 14.3 / 10.2 | 14.9 / 10.8 | 15.5 / 11.4 | 16.7 / 12.6 |

\*The sources only cover 3–5 kWp, so the 6 and 8 kWp costs are extrapolated. Cost is modelled as about €1,500 fixed plus €1,250 per kWp. None of these are quotes.

### Self-supplied equipment (4 kWp)

| Item | Estimate |
|---|---|
| 8 × 500 W panels (Leroy Merlin, Hanersun TOPCon) | about €616 |
| 4 kW inverter with 2 MPPT inputs, hybrid (e.g. Huawei SUN2000-4KTL-L1) | about €700–900 |
| 2 × 4-panel tile-roof mounting kits | about €307 |
| Tile hooks, cables, protection, meter (my estimate) | about €300–500 |
| Licensed electrician: connection, sign-off, DGEG registration (my estimate) | about €400–1,000 |
| Delivery | about €50–100 |
| **Total** | **about €2,400–3,400**, so payback about **4.5–6 years** at €546/yr |

- Above 800 W, the installation must be carried out by a certified installer or licensed electrician and registered with DGEG. **Find that electrician before buying anything.**
- Doing it yourself carries risks: roof work, high-voltage DC wiring, panel and inverter warranties, and home insurance.

### What the payback figures leave out

- Panel degradation (about 0.5%/yr).
- Electricity price inflation.
- Replacing the inverter around year 10–15.
- Any extra metering cost above 4 kW.

### Getting paid for export

- Export is only paid with a buyer contract. That means opening activity with the tax authority (Finanças); sales income up to €1,000/yr is IRS-exempt.
- Published prices are roughly €0.035–0.039/kWh fixed, or market-indexed. Without a contract, exported energy earns nothing.
- At 4 kWp that is about **€85–110/yr**, roughly 15% of savings. Even with zero export income, a 4 kWp system pays back in about 14 years.

### Check VAT eligibility

The 6% VAT on the first 200 kWh requires contracted power of 6.9 kVA or less, and the 8.9 kW peak suggests it may be higher. This changes the bill before solar, but not the savings.

---

## 8. Uncertainty

- **Annual consumption (about 8,725 kWh/yr) is extrapolated from two summer months.** There is still **no winter data**.
  - Winter heating (electric towel rail, fireplace, AC used as a heat pump) and seasonal changes in EV mileage are unknown.
- **Seasonal timing is a bigger risk than total kWh.** If winter use is mostly evening heating, self-consumption falls just when solar output is lowest. That favours staying at 4 kWp or less.
- **August includes holiday days.** It shows how sensitive the result is to occupancy: home-office use (September) raises daytime consumption and improves solar value.
- **About 4.7% of the period's timing is reconstructed**, mostly in September (6, 9–10, 12 and 30 Sep).
- **The 15-minute steps overstate self-consumption by a few percent.** Short spikes can't be matched exactly, and solar output is spread evenly within each hour.
- **Shading:** the terrain has been checked and is negligible. **Nearby obstacles** (trees, buildings, chimneys) **are still unknown** and matter most in winter.
- **Roof orientation, tilt and usable area are still assumed** (due south and due west, 25°).
- **Costs and export prices are market indications, not quotes.**

---

## 9. Next steps

### Data

1. **Raise Shelly minute-data retention to at least 400 days now.** 15-minute data older than 90 days is being deleted, and early August starts disappearing around 30 October 2026.
2. **Get 12+ months of E-REDES smart-meter readings** (15-minute data, from the E-REDES Balcão Digital website). This is the single most valuable addition because it adds winter.
3. **Rerun this analysis in February–March 2027** with October–January Shelly data.
4. **Electricity bill:** contracted power, tariff type, and whether a two-period day/night tariff would be cheaper for night EV charging.

### The roof (the exact coordinates are done)

5. **Exact orientation of each section, in degrees.** Measure the direction of each roof edge on Google Maps, or with a phone compass held against the edge, facing outwards.
6. **Roof pitch.** A phone level app on the roof or the underside of the roof in the attic is enough.
7. **Dimensions of each section** (length × width), to check how many panels fit on each.
8. **Nearby obstacles:** direction, height and distance of trees, buildings and chimneys, especially to the southeast, south and southwest. Photos taken from the roof towards those directions are ideal.

### The home

9. **Car availability and charger:** how many weekdays the car is at home in daylight, and whether a charger that follows the solar surplus is worth adding.
10. **Hot water and heating:** what the water heater and heating run on, and their consumption.
11. **Identify the "Garage Tests" load** (about 59 W constant, about 520 kWh/yr). If it isn't needed, switching it off saves about €90/yr with no investment.

### Installers and buyers

12. **Installer site survey** to confirm points 5–8.
13. **Quotes for about 4 kWp,** for both turnkey and "supply your own equipment" options. Ask whether the inverter stays at 4 kW or below, the price per kWp for 5 kWp, and the battery price as a separate line.
14. **Compare surplus-buyer offers** using the ERSE comparison tool, before deciding whether exporting is worth registering for.

---

## Sources

- Solar generation, terrain horizon and optimal tilt: [PVGIS v5.3 (EC JRC)](https://re.jrc.ec.europa.eu/pvg_tools/en/), via its API: SARAH3/ERA5 data, 38.950780 N, −9.037919 E, roof-mounted, 14% loss
- Sun positions: NOAA solar position equations, calculated for the exact coordinates
- Installation and battery costs: [4gnews – 2026 costs](https://4gnews.pt/vais-instalar-paineis-solares-fizemos-as-contas-antes-de-gastares-dinheiro/), [4gnews – batteries](https://4gnews.pt/isto-pode-baixar-a-tua-conta-da-luz-mas-sera-que-compensa/), [E-konomista](https://www.e-konomista.pt/vale-sempre-a-pena-instalar-paineis-solares-em-casas-particulares/)
- Equipment prices: [Leroy Merlin – Hanersun 500 W](https://www.leroymerlin.pt/produtos/painel-solar-hanersun-topcon-500-w-95653745.html), [Tienda Solar – Huawei SUN2000-4KTL-L1](https://tienda-solar.es/en/inverters/976-solar-inverter-huawei-sun2000-4ktl-l1), [FV Componentes](https://fvcomponentes.com/en/?p=3175), [Sun Suprimentos – mounting kits](https://suministrosdelsol.com/pt/tampa-inclinada/1393-11133-suporte-coplanar-para-4-paineis-tampa-metalica-ou-azulejo-vertical-ate-2400x1150.html)
- VAT on solar back to 23%: [The Portugal News](https://www.theportugalnews.com/pt/noticias/2026-07-13/a-taxa-de-iva-de-6-para-a-energia-solar-em-portugal-vai-expirar-e-voltara-a-23-em-julho-de-2025/133871), [Jornal de Negócios](https://www.jornaldenegocios.pt/empresas/energia/detalhe/associacoes-apelam-para-descida-do-iva-nos-paineis-solares-e-ar-condicionado)
- Self-consumption rules, certified installer requirement and selling surplus: [Doutor Finanças](https://www.doutorfinancas.pt/?p=213476), [ADENE presentation](https://www.cm-maia.pt/cmmaia/uploads/writer_file/document/13010/apresentacao_maia_empoweringus_17072025_adene.pdf), [Universidade de Évora](https://www.catedraer.uevora.pt/content/download/392/2196/version/1/file/Autoconsumo+-+DecLei162-2019+v1.2.pdf)
- Export prices: [DECO Proteste](https://www.deco.proteste.pt/comunidades/energias-renovaveis/produzir-eletricidade/conversation/5185/preco-da-compensacao-por-excedente), [Jornal Económico](https://jornaleconomico.sapo.pt/?p=23546), [EC Clean Energy Islands](https://clean-energy-islands.ec.europa.eu/countries/portugal/legal/res-electricity/net-metering-net-billing)
- Reduced VAT on electricity: [Repsol](https://www.repsol.pt/particulares/assessoramento/iva-eletricidade-tudo-que-precisa-saber/)
- Tariff: as configured in the Shelly app (export README)
