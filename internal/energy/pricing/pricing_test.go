package pricing

import (
	"strings"
	"testing"

	"github.com/shopspring/decimal"
)

func dec(value string) decimal.Decimal {
	return decimal.RequireFromString(value)
}

func assertDecimal(t *testing.T, field string, got, want decimal.Decimal) {
	t.Helper()
	if !got.Equal(want) {
		t.Errorf("%s = %s, want %s", field, got, want)
	}
}

func TestDefaultConfigMatchesInvoiceDefaults(t *testing.T) {
	t.Parallel()
	config := DefaultConfig()

	if config.Currency != "EUR" || !config.ReducedVATEnabled || config.LargeFamilyEnabled || !config.IncludeExciseTax {
		t.Fatalf("unexpected boolean or currency defaults: %+v", config)
	}
	assertDecimal(t, "EnergyPrice", config.EnergyPricePerKWhExcludingVAT, dec("0.137378"))
	assertDecimal(t, "ContractedPower", config.ContractedPowerKVA, dec("5.75"))
	assertDecimal(t, "ReducedVATRate", config.ReducedVATRate, dec("0.06"))
	assertDecimal(t, "StandardVATRate", config.StandardVATRate, dec("0.23"))
	assertDecimal(t, "StandardLimit", config.StandardReducedVATLimitKWhPer30Days, dec("200"))
	assertDecimal(t, "LargeFamilyLimit", config.LargeFamilyReducedVATLimitKWhPer30Days, dec("300"))
	assertDecimal(t, "ExciseTax", config.ElectricityExciseTaxPerKWh, dec("0.001000"))
	assertDecimal(t, "ExciseVAT", config.ElectricityExciseTaxVATRate, dec("0.23"))
}

func TestCalculate31DayGoldenCase(t *testing.T) {
	t.Parallel()
	result, err := Calculate(DefaultConfig(), dec("583"), 31)
	if err != nil {
		t.Fatalf("Calculate() error = %v", err)
	}

	assertDecimal(t, "ConsumptionKWh", result.ConsumptionKWh, dec("583"))
	assertDecimal(t, "ProratedReducedLimitKWh", result.ProratedReducedLimitKWh, dec("206.666666666666666666666667"))
	assertDecimal(t, "ReducedRateKWh", result.ReducedRateKWh, dec("206.666666666666666666666667"))
	assertDecimal(t, "StandardRateKWh", result.StandardRateKWh, dec("376.333333333333333333333333"))
	assertDecimal(t, "EnergyNetTotal", result.EnergyNetTotal, dec("80.091374"))
	assertDecimal(t, "ExciseTaxNet", result.ExciseTaxNet, dec("0.583"))
	assertDecimal(t, "ExciseTaxVAT", result.ExciseTaxVAT, dec("0.13409"))
	assertDecimal(t, "TotalIncludingVAT", result.TotalIncludingVAT, dec("94.40293295333333333333333332554858"))
	assertDecimal(t, "RoundedTotal", result.RoundedTotal(2), dec("94.40"))
}

func TestContractedPowerAboveEligibilityTaxesAllConsumptionAtStandardRate(t *testing.T) {
	t.Parallel()
	config := DefaultConfig()
	config.ContractedPowerKVA = dec("10.35")

	result, err := Calculate(config, dec("100"), 30)
	if err != nil {
		t.Fatalf("Calculate() error = %v", err)
	}
	assertDecimal(t, "ReducedRateKWh", result.ReducedRateKWh, decimal.Zero)
	assertDecimal(t, "StandardRateKWh", result.StandardRateKWh, dec("100"))
}

func TestLargeFamilyThreshold(t *testing.T) {
	t.Parallel()
	config := DefaultConfig()
	config.LargeFamilyEnabled = true

	result, err := Calculate(config, dec("350"), 30)
	if err != nil {
		t.Fatalf("Calculate() error = %v", err)
	}
	assertDecimal(t, "ReducedRateKWh", result.ReducedRateKWh, dec("300"))
	assertDecimal(t, "StandardRateKWh", result.StandardRateKWh, dec("50"))
}

func TestReducedVATCanBeDisabled(t *testing.T) {
	t.Parallel()
	config := DefaultConfig()
	config.ReducedVATEnabled = false

	result, err := Calculate(config, dec("100"), 30)
	if err != nil {
		t.Fatalf("Calculate() error = %v", err)
	}
	assertDecimal(t, "ReducedRateKWh", result.ReducedRateKWh, decimal.Zero)
	assertDecimal(t, "StandardRateKWh", result.StandardRateKWh, dec("100"))
}

func TestExciseTaxCanBeExcluded(t *testing.T) {
	t.Parallel()
	config := DefaultConfig()
	config.IncludeExciseTax = false

	result, err := Calculate(config, dec("100"), 30)
	if err != nil {
		t.Fatalf("Calculate() error = %v", err)
	}
	assertDecimal(t, "ExciseTaxNet", result.ExciseTaxNet, decimal.Zero)
	assertDecimal(t, "ExciseTaxVAT", result.ExciseTaxVAT, decimal.Zero)
}

func TestAllocateIncrementSplitsAtVATThreshold(t *testing.T) {
	t.Parallel()
	state := AllocationState{CycleConsumptionKWh: dec("199")}

	next, result, err := AllocateIncrement(DefaultConfig(), state, dec("4"), 30)
	if err != nil {
		t.Fatalf("AllocateIncrement() error = %v", err)
	}
	assertDecimal(t, "ReducedRateKWh", result.ReducedRateKWh, dec("1"))
	assertDecimal(t, "StandardRateKWh", result.StandardRateKWh, dec("3"))
	assertDecimal(t, "next.CycleConsumptionKWh", next.CycleConsumptionKWh, dec("203"))
	assertDecimal(t, "original state", state.CycleConsumptionKWh, dec("199"))
}

func TestAllocateIntervalsKeepsAllowanceSeparateByBillingCycle(t *testing.T) {
	t.Parallel()
	inputs := []IntervalConsumption{
		{CycleID: "2026-06-11", BillingPeriodDays: 30, ConsumptionKWh: dec("199")},
		{CycleID: "2026-06-11", BillingPeriodDays: 30, ConsumptionKWh: dec("4")},
		{CycleID: "2026-07-11", BillingPeriodDays: 31, ConsumptionKWh: dec("3")},
	}

	results, err := AllocateIntervals(DefaultConfig(), inputs)
	if err != nil {
		t.Fatalf("AllocateIntervals() error = %v", err)
	}
	assertDecimal(t, "second reduced", results[1].Cost.ReducedRateKWh, dec("1"))
	assertDecimal(t, "second standard", results[1].Cost.StandardRateKWh, dec("3"))
	assertDecimal(t, "new cycle reduced", results[2].Cost.ReducedRateKWh, dec("3"))
}

func TestValidationRejectsUnsafePricingInputs(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		change func(Config) Config
	}{
		{name: "negative price", change: func(c Config) Config { c.EnergyPricePerKWhExcludingVAT = dec("-0.01"); return c }},
		{name: "VAT above 100 percent", change: func(c Config) Config { c.StandardVATRate = dec("1.01"); return c }},
		{name: "negative contracted power", change: func(c Config) Config { c.ContractedPowerKVA = dec("-1"); return c }},
		{name: "zero standard threshold", change: func(c Config) Config { c.StandardReducedVATLimitKWhPer30Days = decimal.Zero; return c }},
		{name: "empty currency", change: func(c Config) Config { c.Currency = ""; return c }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := test.change(DefaultConfig()).Validate(); err == nil {
				t.Fatal("Validate() accepted invalid configuration")
			}
		})
	}

	if _, err := Calculate(DefaultConfig(), dec("-1"), 30); err == nil {
		t.Error("Calculate() accepted negative consumption")
	}
	if _, err := Calculate(DefaultConfig(), dec("1"), 0); err == nil {
		t.Error("Calculate() accepted a zero-day period")
	}
}

func TestParseFixedDecimalRejectsExponentAndUnboundedPrecision(t *testing.T) {
	t.Parallel()
	for _, value := range []string{"1e-2147483648", "1e100", "-0.1", "1.1234567890123456789", strings.Repeat("9", 33)} {
		if _, err := ParseFixedDecimal("rate", value); err == nil {
			t.Fatalf("ParseFixedDecimal accepted %q", value)
		}
	}
	parsed, err := ParseFixedDecimal("rate", "0.137378")
	if err != nil || !parsed.Equal(dec("0.137378")) {
		t.Fatalf("fixed decimal = %s: %v", parsed, err)
	}
}
