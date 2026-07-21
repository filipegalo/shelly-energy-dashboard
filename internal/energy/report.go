package energy

import (
	"context"
	"errors"
	"sort"
	"time"

	"github.com/filipegalo/shelly-energy-dashboard/internal/database"
	"github.com/filipegalo/shelly-energy-dashboard/internal/energy/billingcycle"
	"github.com/filipegalo/shelly-energy-dashboard/internal/energy/pricing"
	"github.com/shopspring/decimal"
)

type ChartPoint struct {
	Start       time.Time `json:"start"`
	End         time.Time `json:"end"`
	EnergyKWh   *string   `json:"energy_kwh"`
	CostEUR     *string   `json:"cost_eur"`
	Quality     string    `json:"quality"`
	SampleCount int       `json:"sample_count"`
}

type Report struct {
	Start       time.Time
	End         time.Time
	Interval    string
	Points      []ChartPoint
	Consumption decimal.Decimal
	Cost        pricing.EnergyCostBreakdown
}

func BuildReport(ctx context.Context, store *database.Store, config database.EnergyConfig, start, end time.Time, intervalType string) (Report, error) {
	if config.DeviceID < 1 || config.ComponentKey == "" {
		return Report{}, errors.New("main energy meter is not configured")
	}
	if start.IsZero() || end.IsZero() || !start.Before(end) || end.Sub(start) > 366*24*time.Hour {
		return Report{}, errors.New("energy report range is invalid or exceeds 366 days")
	}
	if intervalType != "hour" && intervalType != "day" && intervalType != "minute" {
		return Report{}, errors.New("energy report interval must be minute, hour, or day")
	}
	if intervalType == "minute" && end.Sub(start) > 7*24*time.Hour {
		return Report{}, errors.New("minute energy reports are limited to 7 days")
	}
	location, err := time.LoadLocation(config.Timezone)
	if err != nil {
		return Report{}, errors.New("configured energy timezone is invalid")
	}
	aggregates, err := store.ListEnergyAggregates(ctx, config.DeviceID, config.ComponentKey, start, end, intervalType)
	if err != nil {
		return Report{}, err
	}
	report := Report{Start: start, End: end, Interval: intervalType, Points: make([]ChartPoint, 0, len(aggregates))}
	versions, err := store.ListPricingVersions(ctx)
	if err != nil {
		return Report{}, err
	}
	aggregatesByStart := make(map[string]database.EnergyAggregate, len(aggregates))
	for _, aggregate := range aggregates {
		aggregatesByStart[aggregate.IntervalStart.UTC().Format(time.RFC3339Nano)] = aggregate
	}
	bucketStart, bucketEnd, err := IntervalBounds(start, intervalType, location)
	if err != nil {
		return Report{}, err
	}
	if bucketStart.Before(start) {
		bucketStart = bucketEnd
	}
	for bucketStart.Before(end) {
		_, bucketEnd, err = IntervalBounds(bucketStart, intervalType, location)
		if err != nil {
			return Report{}, err
		}
		aggregate, found := aggregatesByStart[bucketStart.UTC().Format(time.RFC3339Nano)]
		if !found {
			report.Points = append(report.Points, ChartPoint{Start: bucketStart, End: bucketEnd, Quality: QualityPartial})
			bucketStart = bucketEnd
			continue
		}
		cyclePricing, found := pricingAt(versions, aggregate.IntervalStart)
		if !found {
			return Report{}, errors.New("no electricity pricing version applies to energy history")
		}
		cycle, err := billingcycle.CycleContaining(aggregate.IntervalStart, cyclePricing.BillingCycleStartDay, location)
		if err != nil {
			return Report{}, err
		}
		consumedBeforeWh, err := store.SumEnergyWhByInterval(ctx, config.DeviceID, config.ComponentKey, cycle.Start, aggregate.IntervalStart, intervalType)
		if err != nil {
			return Report{}, err
		}
		intervalKWh := decimal.NewFromFloat(aggregate.EnergyConsumedWh).Div(decimal.NewFromInt(1000))
		state := pricing.AllocationState{CycleConsumptionKWh: decimal.NewFromFloat(consumedBeforeWh).Div(decimal.NewFromInt(1000))}
		cost := pricing.EnergyCostBreakdown{}
		segments, err := priceSegments(aggregate.IntervalStart, aggregate.IntervalEnd, versions)
		if err != nil {
			return Report{}, err
		}
		actualSegmentEnergy, hasActualSegments, segmentErr := retainedMinuteSegmentEnergy(ctx, store, config, aggregate, segments, intervalKWh)
		if segmentErr != nil {
			return Report{}, segmentErr
		}
		allocated := decimal.Zero
		for index, segment := range segments {
			segmentKWh := intervalKWh.Sub(allocated)
			if hasActualSegments {
				segmentKWh = actualSegmentEnergy[index]
			} else if index < len(segments)-1 {
				segmentDuration := decimal.NewFromInt(segment.End.Sub(segment.Start).Nanoseconds())
				totalDuration := decimal.NewFromInt(aggregate.IntervalEnd.Sub(aggregate.IntervalStart).Nanoseconds())
				segmentKWh = intervalKWh.Mul(segmentDuration).Div(totalDuration)
			}
			rules, configErr := PricingConfig(segment.Pricing)
			if configErr != nil {
				return Report{}, configErr
			}
			var segmentCost pricing.EnergyCostBreakdown
			state, segmentCost, err = pricing.AllocateIncrement(rules, state, segmentKWh, cycle.Days)
			if err != nil {
				return Report{}, err
			}
			allocated = allocated.Add(segmentKWh)
			cost = addCost(cost, segmentCost)
		}
		report.Consumption = report.Consumption.Add(intervalKWh)
		report.Cost = addCost(report.Cost, cost)
		point := ChartPoint{Start: aggregate.IntervalStart, End: aggregate.IntervalEnd, Quality: aggregate.CalculationQuality, SampleCount: aggregate.SampleCount}
		if aggregate.SampleCount > 0 {
			energyKWh := intervalKWh.Round(3).StringFixed(3)
			costEUR := cost.TotalIncludingVAT.Round(4).StringFixed(4)
			point.EnergyKWh, point.CostEUR = &energyKWh, &costEUR
		}
		report.Points = append(report.Points, point)
		bucketStart = bucketEnd
	}
	return report, nil
}

func retainedMinuteSegmentEnergy(ctx context.Context, store *database.Store, config database.EnergyConfig, aggregate database.EnergyAggregate, segments []priceSegment, totalKWh decimal.Decimal) ([]decimal.Decimal, bool, error) {
	if len(segments) < 2 {
		return nil, false, nil
	}
	minutes, err := store.ListEnergyAggregates(ctx, config.DeviceID, config.ComponentKey, aggregate.IntervalStart, aggregate.IntervalEnd, "minute")
	if err != nil {
		return nil, false, err
	}
	if len(minutes) == 0 {
		return nil, false, nil
	}
	values := make([]decimal.Decimal, len(segments))
	total := decimal.Zero
	for _, minute := range minutes {
		energy := decimal.NewFromFloat(minute.EnergyConsumedWh).Div(decimal.NewFromInt(1000))
		for index, segment := range segments {
			if !minute.IntervalStart.Before(segment.Start) && minute.IntervalStart.Before(segment.End) {
				values[index] = values[index].Add(energy)
				total = total.Add(energy)
				break
			}
		}
	}
	if total.Sub(totalKWh).Abs().GreaterThan(decimal.RequireFromString("0.000001")) {
		return nil, false, nil
	}
	return values, true, nil
}

type priceSegment struct {
	Start   time.Time
	End     time.Time
	Pricing database.PricingVersion
}

func priceSegments(start, end time.Time, versions []database.PricingVersion) ([]priceSegment, error) {
	boundaries := []time.Time{start, end}
	for _, version := range versions {
		if version.PriceEffectiveFrom.After(start) && version.PriceEffectiveFrom.Before(end) {
			boundaries = append(boundaries, version.PriceEffectiveFrom)
		}
	}
	sort.Slice(boundaries, func(left, right int) bool { return boundaries[left].Before(boundaries[right]) })
	segments := make([]priceSegment, 0, len(boundaries)-1)
	for index := 0; index < len(boundaries)-1; index++ {
		version, found := pricingAt(versions, boundaries[index])
		if !found {
			return nil, errors.New("no electricity pricing version applies to energy history")
		}
		segments = append(segments, priceSegment{Start: boundaries[index], End: boundaries[index+1], Pricing: version})
	}
	return segments, nil
}

func pricingAt(versions []database.PricingVersion, at time.Time) (database.PricingVersion, bool) {
	var selected database.PricingVersion
	found := false
	for _, version := range versions {
		if !version.PriceEffectiveFrom.After(at) && (!found || version.PriceEffectiveFrom.After(selected.PriceEffectiveFrom) || (version.PriceEffectiveFrom.Equal(selected.PriceEffectiveFrom) && version.ID > selected.ID)) {
			selected, found = version, true
		}
	}
	return selected, found
}

func PricingConfig(version database.PricingVersion) (pricing.Config, error) {
	parse := func(name, raw string) (decimal.Decimal, error) {
		return pricing.ParseFixedDecimal(name, raw)
	}
	price, err := parse("energy price", version.EnergyPricePerKWhExcludingVAT)
	if err != nil {
		return pricing.Config{}, err
	}
	power, err := parse("contracted power", version.ContractedPowerKVA)
	if err != nil {
		return pricing.Config{}, err
	}
	reducedRate, err := parse("reduced VAT rate", version.ReducedVATRate)
	if err != nil {
		return pricing.Config{}, err
	}
	standardRate, err := parse("standard VAT rate", version.StandardVATRate)
	if err != nil {
		return pricing.Config{}, err
	}
	standardLimit, err := parse("standard VAT limit", version.StandardReducedVATLimitKWhPer30Days)
	if err != nil {
		return pricing.Config{}, err
	}
	largeLimit, err := parse("large-family VAT limit", version.LargeFamilyReducedVATLimitKWhPer30Days)
	if err != nil {
		return pricing.Config{}, err
	}
	excise, err := parse("excise tax", version.ElectricityExciseTaxPerKWh)
	if err != nil {
		return pricing.Config{}, err
	}
	exciseVAT, err := parse("excise tax VAT rate", version.ElectricityExciseTaxVATRate)
	if err != nil {
		return pricing.Config{}, err
	}
	config := pricing.Config{Currency: version.Currency, EnergyPricePerKWhExcludingVAT: price, ContractedPowerKVA: power, ReducedVATEnabled: version.ReducedVATEnabled, LargeFamilyEnabled: version.LargeFamilyEnabled, ReducedVATRate: reducedRate, StandardVATRate: standardRate, StandardReducedVATLimitKWhPer30Days: standardLimit, LargeFamilyReducedVATLimitKWhPer30Days: largeLimit, ElectricityExciseTaxPerKWh: excise, ElectricityExciseTaxVATRate: exciseVAT, IncludeExciseTax: version.IncludeExciseTax}
	return config, config.Validate()
}

func addCost(left, right pricing.EnergyCostBreakdown) pricing.EnergyCostBreakdown {
	return pricing.EnergyCostBreakdown{
		ConsumptionKWh:          left.ConsumptionKWh.Add(right.ConsumptionKWh),
		ProratedReducedLimitKWh: right.ProratedReducedLimitKWh,
		ReducedRateKWh:          left.ReducedRateKWh.Add(right.ReducedRateKWh),
		StandardRateKWh:         left.StandardRateKWh.Add(right.StandardRateKWh),
		ReducedEnergyNet:        left.ReducedEnergyNet.Add(right.ReducedEnergyNet),
		StandardEnergyNet:       left.StandardEnergyNet.Add(right.StandardEnergyNet),
		EnergyNetTotal:          left.EnergyNetTotal.Add(right.EnergyNetTotal),
		ReducedVAT:              left.ReducedVAT.Add(right.ReducedVAT),
		StandardVAT:             left.StandardVAT.Add(right.StandardVAT),
		EnergyVATTotal:          left.EnergyVATTotal.Add(right.EnergyVATTotal),
		ExciseTaxNet:            left.ExciseTaxNet.Add(right.ExciseTaxNet),
		ExciseTaxVAT:            left.ExciseTaxVAT.Add(right.ExciseTaxVAT),
		TotalIncludingVAT:       left.TotalIncludingVAT.Add(right.TotalIncludingVAT),
	}
}
