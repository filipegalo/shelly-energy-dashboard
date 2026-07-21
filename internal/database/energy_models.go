package database

import "time"

type EnergyConfig struct {
	ID                    int64
	Enabled               bool
	DeviceID              int64
	ComponentKey          string
	ComponentType         string
	CapabilitiesJSON      string
	PollIntervalSeconds   int
	BillingCycleStartDay  int
	Timezone              string
	Currency              string
	MaximumExpectedPowerW float64
	SampleRetentionDays   int
	MinuteRetentionDays   int
	HourlyRetentionDays   int
	CreatedAt             time.Time
	UpdatedAt             time.Time
}

type PricingVersion struct {
	ID                                     int64
	EnergyPricePerKWhExcludingVAT          string
	ReferencePricePerKWhExcludingVAT       string
	ContractedPowerKVA                     string
	ReducedVATEnabled                      bool
	LargeFamilyEnabled                     bool
	ReducedVATRate                         string
	StandardVATRate                        string
	StandardReducedVATLimitKWhPer30Days    string
	LargeFamilyReducedVATLimitKWhPer30Days string
	ElectricityExciseTaxPerKWh             string
	ElectricityExciseTaxVATRate            string
	IncludeExciseTax                       bool
	BillingCycleStartDay                   int
	Currency                               string
	Region                                 string
	PriceEffectiveFrom                     time.Time
	CreatedAt                              time.Time
	UpdatedAt                              time.Time
}

func DefaultPricingVersion() PricingVersion {
	return PricingVersion{
		EnergyPricePerKWhExcludingVAT:          "0.137378",
		ReferencePricePerKWhExcludingVAT:       "0.165516",
		ContractedPowerKVA:                     "5.75",
		ReducedVATEnabled:                      true,
		ReducedVATRate:                         "0.06",
		StandardVATRate:                        "0.23",
		StandardReducedVATLimitKWhPer30Days:    "200",
		LargeFamilyReducedVATLimitKWhPer30Days: "300",
		ElectricityExciseTaxPerKWh:             "0.001000",
		ElectricityExciseTaxVATRate:            "0.23",
		IncludeExciseTax:                       true,
		BillingCycleStartDay:                   11,
		Currency:                               "EUR",
		Region:                                 "Portugal Continental",
	}
}

type EnergySample struct {
	ID                    int64
	DeviceID              int64
	ComponentKey          string
	RecordedAt            time.Time
	PowerW                *float64
	ImportedEnergyTotalWh *float64
	ExportedEnergyTotalWh *float64
	VoltageV              *float64
	CurrentA              *float64
	PowerFactor           *float64
	FrequencyHz           *float64
	ReadingQuality        string
	CreatedAt             time.Time
}

type EnergyAggregate struct {
	ID                 int64
	IntervalType       string
	IntervalStart      time.Time
	IntervalEnd        time.Time
	DeviceID           int64
	ComponentKey       string
	EnergyConsumedWh   float64
	AveragePowerW      *float64
	MinimumPowerW      *float64
	MaximumPowerW      *float64
	SampleCount        int
	MissingSampleCount int
	CalculationQuality string
	CreatedAt          time.Time
	UpdatedAt          time.Time
}

type MeterEvent struct {
	ID           int64
	DeviceID     int64
	ComponentKey string
	EventType    string
	OccurredAt   time.Time
	DetailsJSON  string
	CreatedAt    time.Time
}

type EnergyCleanupResult struct {
	SamplesDeleted          int64
	MinuteAggregatesDeleted int64
	HourlyAggregatesDeleted int64
}
