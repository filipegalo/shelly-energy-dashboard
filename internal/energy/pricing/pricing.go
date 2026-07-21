// Package pricing implements decimal-safe variable electricity cost and
// Portuguese consumption VAT calculations. It intentionally excludes fixed
// invoice charges.
package pricing

import (
	"errors"
	"fmt"
	"strings"

	"github.com/shopspring/decimal"
)

var (
	maximumEnergyPrice = decimal.NewFromInt(100)
	maximumPowerKVA    = decimal.NewFromInt(1000)
	maximumLimitKWh    = decimal.NewFromInt(1_000_000)
)

// ParseFixedDecimal rejects exponent notation and bounds precision before the
// decimal library can allocate while rescaling attacker-controlled values.
func ParseFixedDecimal(name, raw string) (decimal.Decimal, error) {
	value := strings.TrimSpace(raw)
	if len(value) == 0 || len(value) > 32 {
		return decimal.Zero, fmt.Errorf("%s must contain at most 32 decimal characters", name)
	}
	digits, fractionalDigits, dots := 0, 0, 0
	fractional := false
	for _, character := range value {
		switch {
		case character == '.' && !fractional:
			dots++
			fractional = true
		case character >= '0' && character <= '9':
			digits++
			if fractional {
				fractionalDigits++
			}
		default:
			return decimal.Zero, fmt.Errorf("%s must be a fixed decimal without an exponent", name)
		}
	}
	if digits == 0 || dots > 1 || fractionalDigits > 18 {
		return decimal.Zero, fmt.Errorf("%s must have digits and at most 18 decimal places", name)
	}
	parsed, err := decimal.NewFromString(value)
	if err != nil {
		return decimal.Zero, fmt.Errorf("%s is not a valid decimal", name)
	}
	return parsed, nil
}

// Config contains rates as decimals. VAT rates are fractions: 0.06 means 6%.
type Config struct {
	Currency                               string
	EnergyPricePerKWhExcludingVAT          decimal.Decimal
	ContractedPowerKVA                     decimal.Decimal
	ReducedVATEnabled                      bool
	LargeFamilyEnabled                     bool
	ReducedVATRate                         decimal.Decimal
	StandardVATRate                        decimal.Decimal
	StandardReducedVATLimitKWhPer30Days    decimal.Decimal
	LargeFamilyReducedVATLimitKWhPer30Days decimal.Decimal
	ElectricityExciseTaxPerKWh             decimal.Decimal
	ElectricityExciseTaxVATRate            decimal.Decimal
	IncludeExciseTax                       bool
}

// DefaultConfig returns the supplied Portuguese mainland invoice defaults.
func DefaultConfig() Config {
	return Config{
		Currency:                               "EUR",
		EnergyPricePerKWhExcludingVAT:          decimal.RequireFromString("0.137378"),
		ContractedPowerKVA:                     decimal.RequireFromString("5.75"),
		ReducedVATEnabled:                      true,
		LargeFamilyEnabled:                     false,
		ReducedVATRate:                         decimal.RequireFromString("0.06"),
		StandardVATRate:                        decimal.RequireFromString("0.23"),
		StandardReducedVATLimitKWhPer30Days:    decimal.NewFromInt(200),
		LargeFamilyReducedVATLimitKWhPer30Days: decimal.NewFromInt(300),
		ElectricityExciseTaxPerKWh:             decimal.RequireFromString("0.001000"),
		ElectricityExciseTaxVATRate:            decimal.RequireFromString("0.23"),
		IncludeExciseTax:                       true,
	}
}

// Validate checks configuration at the pricing service boundary.
func (config Config) Validate() error {
	if !validCurrency(config.Currency) {
		return errors.New("currency must be a three-letter uppercase code")
	}
	if err := validateRange("energy price per kWh", config.EnergyPricePerKWhExcludingVAT, decimal.Zero, maximumEnergyPrice, true); err != nil {
		return err
	}
	if err := validateRange("contracted power", config.ContractedPowerKVA, decimal.Zero, maximumPowerKVA, true); err != nil {
		return err
	}
	if err := validateRate("reduced VAT rate", config.ReducedVATRate); err != nil {
		return err
	}
	if err := validateRate("standard VAT rate", config.StandardVATRate); err != nil {
		return err
	}
	if err := validateRange("standard reduced VAT limit", config.StandardReducedVATLimitKWhPer30Days, decimal.Zero, maximumLimitKWh, false); err != nil {
		return err
	}
	if err := validateRange("large-family reduced VAT limit", config.LargeFamilyReducedVATLimitKWhPer30Days, decimal.Zero, maximumLimitKWh, false); err != nil {
		return err
	}
	if err := validateRange("electricity excise tax", config.ElectricityExciseTaxPerKWh, decimal.Zero, maximumEnergyPrice, true); err != nil {
		return err
	}
	return validateRate("electricity excise tax VAT rate", config.ElectricityExciseTaxVATRate)
}

// EnergyCostBreakdown is an unrounded, auditable variable-cost breakdown.
// Call RoundedTotal only at a display or final monetary boundary.
type EnergyCostBreakdown struct {
	ConsumptionKWh          decimal.Decimal
	ProratedReducedLimitKWh decimal.Decimal
	ReducedRateKWh          decimal.Decimal
	StandardRateKWh         decimal.Decimal

	ReducedEnergyNet  decimal.Decimal
	StandardEnergyNet decimal.Decimal
	EnergyNetTotal    decimal.Decimal

	ReducedVAT     decimal.Decimal
	StandardVAT    decimal.Decimal
	EnergyVATTotal decimal.Decimal

	ExciseTaxNet decimal.Decimal
	ExciseTaxVAT decimal.Decimal

	TotalIncludingVAT decimal.Decimal
}

// RoundedTotal rounds the final variable total using decimal half-away-from-zero.
func (breakdown EnergyCostBreakdown) RoundedTotal(places int32) decimal.Decimal {
	return breakdown.TotalIncludingVAT.Round(places)
}

// Calculate allocates a complete billing cycle's consumption and calculates cost.
func Calculate(config Config, consumptionKWh decimal.Decimal, billingPeriodDays int) (EnergyCostBreakdown, error) {
	if err := validateInputs(config, consumptionKWh, billingPeriodDays); err != nil {
		return EnergyCostBreakdown{}, err
	}
	limit := proratedLimit(config, billingPeriodDays)
	reducedKWh := minDecimal(consumptionKWh, limit)
	standardKWh := consumptionKWh.Sub(reducedKWh)
	return calculateAllocated(config, consumptionKWh, reducedKWh, standardKWh, limit), nil
}

// AllocationState holds cumulative consumption within exactly one billing cycle.
// AllocateIncrement returns a new state and does not modify the supplied value.
type AllocationState struct {
	CycleConsumptionKWh decimal.Decimal
}

// AllocateIncrement prices an interval against allowance remaining in its cycle.
func AllocateIncrement(config Config, state AllocationState, incrementKWh decimal.Decimal, billingPeriodDays int) (AllocationState, EnergyCostBreakdown, error) {
	if state.CycleConsumptionKWh.IsNegative() {
		return state, EnergyCostBreakdown{}, errors.New("cycle consumption before interval must not be negative")
	}
	if err := validateInputs(config, incrementKWh, billingPeriodDays); err != nil {
		return state, EnergyCostBreakdown{}, err
	}

	limit := proratedLimit(config, billingPeriodDays)
	remaining := limit.Sub(state.CycleConsumptionKWh)
	if remaining.IsNegative() {
		remaining = decimal.Zero
	}
	reducedKWh := minDecimal(incrementKWh, remaining)
	standardKWh := incrementKWh.Sub(reducedKWh)
	next := AllocationState{CycleConsumptionKWh: state.CycleConsumptionKWh.Add(incrementKWh)}
	return next, calculateAllocated(config, incrementKWh, reducedKWh, standardKWh, limit), nil
}

// IntervalConsumption identifies the billing cycle for an ordered chart interval.
type IntervalConsumption struct {
	CycleID           string
	BillingPeriodDays int
	ConsumptionKWh    decimal.Decimal
}

// AllocatedInterval preserves the input and attaches its incremental cost.
type AllocatedInterval struct {
	Interval IntervalConsumption
	Cost     EnergyCostBreakdown
}

// AllocateIntervals applies each cycle's VAT allowance once, even when a report
// crosses billing-cycle boundaries. Inputs are returned in their original order.
func AllocateIntervals(config Config, intervals []IntervalConsumption) ([]AllocatedInterval, error) {
	if err := config.Validate(); err != nil {
		return nil, err
	}
	type cycleState struct {
		allocation AllocationState
		days       int
	}
	states := make(map[string]cycleState)
	results := make([]AllocatedInterval, 0, len(intervals))
	for index, interval := range intervals {
		if strings.TrimSpace(interval.CycleID) == "" {
			return nil, fmt.Errorf("interval %d: cycle ID is required", index)
		}
		state, exists := states[interval.CycleID]
		if exists && state.days != interval.BillingPeriodDays {
			return nil, fmt.Errorf("interval %d: billing-period days changed within cycle %q", index, interval.CycleID)
		}
		next, cost, err := AllocateIncrement(config, state.allocation, interval.ConsumptionKWh, interval.BillingPeriodDays)
		if err != nil {
			return nil, fmt.Errorf("interval %d: %w", index, err)
		}
		states[interval.CycleID] = cycleState{allocation: next, days: interval.BillingPeriodDays}
		results = append(results, AllocatedInterval{Interval: interval, Cost: cost})
	}
	return results, nil
}

func calculateAllocated(config Config, consumptionKWh, reducedKWh, standardKWh, limit decimal.Decimal) EnergyCostBreakdown {
	reducedNet := reducedKWh.Mul(config.EnergyPricePerKWhExcludingVAT)
	standardNet := standardKWh.Mul(config.EnergyPricePerKWhExcludingVAT)
	energyNet := reducedNet.Add(standardNet)
	reducedVAT := reducedNet.Mul(config.ReducedVATRate)
	standardVAT := standardNet.Mul(config.StandardVATRate)
	energyVAT := reducedVAT.Add(standardVAT)

	exciseNet := decimal.Zero
	exciseVAT := decimal.Zero
	if config.IncludeExciseTax {
		exciseNet = consumptionKWh.Mul(config.ElectricityExciseTaxPerKWh)
		exciseVAT = exciseNet.Mul(config.ElectricityExciseTaxVATRate)
	}
	total := energyNet.Add(energyVAT).Add(exciseNet).Add(exciseVAT)
	return EnergyCostBreakdown{
		ConsumptionKWh:          consumptionKWh,
		ProratedReducedLimitKWh: limit,
		ReducedRateKWh:          reducedKWh,
		StandardRateKWh:         standardKWh,
		ReducedEnergyNet:        reducedNet,
		StandardEnergyNet:       standardNet,
		EnergyNetTotal:          energyNet,
		ReducedVAT:              reducedVAT,
		StandardVAT:             standardVAT,
		EnergyVATTotal:          energyVAT,
		ExciseTaxNet:            exciseNet,
		ExciseTaxVAT:            exciseVAT,
		TotalIncludingVAT:       total,
	}
}

func proratedLimit(config Config, billingPeriodDays int) decimal.Decimal {
	if !config.ReducedVATEnabled || config.ContractedPowerKVA.GreaterThan(decimal.RequireFromString("6.9")) {
		return decimal.Zero
	}
	limit := config.StandardReducedVATLimitKWhPer30Days
	if config.LargeFamilyEnabled {
		limit = config.LargeFamilyReducedVATLimitKWhPer30Days
	}
	return limit.Mul(decimal.NewFromInt(int64(billingPeriodDays))).DivRound(decimal.NewFromInt(30), 24)
}

func validateInputs(config Config, consumptionKWh decimal.Decimal, billingPeriodDays int) error {
	if err := config.Validate(); err != nil {
		return fmt.Errorf("pricing configuration: %w", err)
	}
	if consumptionKWh.IsNegative() {
		return errors.New("consumption must not be negative")
	}
	if billingPeriodDays < 1 || billingPeriodDays > 366 {
		return errors.New("billing period must contain between 1 and 366 days")
	}
	return nil
}

func validateRange(name string, value, minimum, maximum decimal.Decimal, allowZero bool) error {
	if value.LessThan(minimum) || value.GreaterThan(maximum) || (!allowZero && value.Equal(minimum)) {
		return fmt.Errorf("%s must be %s %s and no greater than %s", name, map[bool]string{true: "at least", false: "greater than"}[allowZero], minimum, maximum)
	}
	return nil
}

func validateRate(name string, value decimal.Decimal) error {
	if value.IsNegative() || value.GreaterThan(decimal.NewFromInt(1)) {
		return fmt.Errorf("%s must be between 0 and 1", name)
	}
	return nil
}

func validCurrency(currency string) bool {
	if len(currency) != 3 {
		return false
	}
	for _, character := range currency {
		if character < 'A' || character > 'Z' {
			return false
		}
	}
	return true
}

func minDecimal(left, right decimal.Decimal) decimal.Decimal {
	if left.LessThan(right) {
		return left
	}
	return right
}
