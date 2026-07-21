package web

import (
	"context"
	"time"

	"github.com/filipegalo/shelly-energy-dashboard/internal/database"
	energyservice "github.com/filipegalo/shelly-energy-dashboard/internal/energy"
	"github.com/filipegalo/shelly-energy-dashboard/internal/shelly"
)

// roomEnergyService is deliberately narrower than the collector manager. It
// keeps device I/O behind the server and lets handlers require a fresh,
// allowlisted component test before persisting a source identity.
type roomEnergyService interface {
	DiscoverMeter(context.Context, int64) (database.Device, []shelly.EnergyComponent, error)
	TestMeter(context.Context, int64, string) (database.Device, shelly.EnergyComponent, shelly.EnergyReading, error)
	Reconfigure(context.Context) error
	SubscribeRoomEnergy() (<-chan energyservice.RoomLiveEvent, func(), error)
	RoomLiveSnapshot() energyservice.RoomLiveSnapshot
}

type roomEnergyViewModel struct {
	Configured     bool
	Message        string
	SelectedRange  string
	Overview       roomEnergyOverviewVM
	Rooms          []roomEnergyRoomCardVM
	Unmetered      roomEnergyRoomCardVM
	Room           roomEnergyRoomDetailVM
	Settings       roomEnergySettingsVM
	Reconciliation roomEnergyReconciliationVM
	Device         deviceEnergyVM
}

type roomEnergyOverviewVM struct {
	WholeHousePower string
	RoomPower       string
	WholeHouseToday string
	RoomToday       string
	UnmeteredToday  string
	Coverage        string
	Status          string
	WholeHouseCost  string
	RoomCost        string
	UnmeteredCost   string
	Quality         string
}

type roomEnergyRoomCardVM struct {
	ID               int64
	Name             string
	Icon             string
	Archived         bool
	Virtual          bool
	CurrentPower     string
	TodayEnergy      string
	TodayCost        string
	BillingEnergy    string
	BillingCost      string
	HouseShare       string
	PeakPower        string
	Quality          string
	LastUpdate       string
	ReportingSources int
	ExpectedSources  int
}

type roomEnergyRoomDetailVM struct {
	roomEnergyRoomCardVM
	WeekEnergy           string
	MonthEnergy          string
	Coverage             string
	ReconciliationStatus string
	Sources              []roomEnergySourceVM
	Cost                 roomEnergyCostVM
}

type roomEnergySourceVM struct {
	AssignmentID  int64
	DeviceID      int64
	DisplayName   string
	DeviceName    string
	DeviceModel   string
	ComponentKey  string
	Role          string
	ParentName    string
	CurrentPower  string
	TodayEnergy   string
	BillingEnergy string
	ParentShare   string
	RoomShare     string
	OnlineState   string
	Freshness     string
	Quality       string
	OtherUsage    string
	Overlap       bool
	Children      []roomEnergySourceVM
}

type roomEnergyCostVM struct {
	MeasuredEnergy       string
	HouseShare           string
	WholeHouseCost       string
	AllocatedCost        string
	AllocationMethod     string
	Quality              string
	ReconciliationStatus string
	Normalized           bool
}

type roomEnergySettingsVM struct {
	Rooms       []roomEnergyOptionVM
	Devices     []roomEnergyDeviceOptionVM
	Assignments []roomEnergyAssignmentVM
	Discovery   roomEnergyDiscoveryVM
	Test        roomEnergyTestVM
	Config      roomEnergyConfigVM
}

type roomEnergyOptionVM struct {
	ID   int64
	Name string
}

type roomEnergyDeviceOptionVM struct {
	ID           int64
	Name         string
	Model        string
	IPAddress    string
	OnlineStatus string
}

type roomEnergyAssignmentVM struct {
	ID                  int64
	RoomID              int64
	DeviceID            int64
	ParentAssignmentID  int64
	RoomName            string
	DeviceName          string
	DeviceModel         string
	IPAddress           string
	ComponentKey        string
	ComponentType       string
	DisplayName         string
	Role                string
	ParentName          string
	State               string
	Enabled             bool
	MainMeter           bool
	CanEdit             bool
	CanDisable          bool
	CanDeleteFuture     bool
	PollIntervalSeconds int
	EffectiveFrom       time.Time
	EffectiveTo         *time.Time
	Parents             []roomEnergyOptionVM
}

type roomEnergyDiscoveryVM struct {
	DeviceID   int64
	DeviceName string
	Components []roomEnergyComponentVM
	Error      string
}

type roomEnergyComponentVM struct {
	Key   string
	Type  string
	Label string
}

type roomEnergyTestVM struct {
	Attempted           bool
	Available           bool
	CumulativeAvailable bool
	PowerOnly           bool
	MainMeter           bool
	DeviceID            int64
	ComponentKey        string
	Label               string
	CurrentPower        string
	ImportedEnergy      string
	Voltage             string
	LastResponse        string
	Error               string
}

type roomEnergyConfigVM struct {
	RelativeTolerancePercent string
	AbsoluteToleranceKWh     string
	DiagnosticCollection     bool
}

type roomEnergyReconciliationVM struct {
	WholeHouseEnergy        string
	RoomEnergy              string
	UnmeteredEnergy         string
	Coverage                string
	Difference              string
	DifferencePercent       string
	Status                  string
	Quality                 string
	RelativeTolerance       string
	AbsoluteTolerance       string
	MissingSources          []string
	SuspectedDoubleCounting bool
	WarningIntervals        int
}

type deviceEnergyVM struct {
	Supported         bool
	InventoryRoomID   int64
	InventoryRoomName string
	Assignments       []roomEnergyDeviceAssignmentVM
}

type roomEnergyDeviceAssignmentVM struct {
	ID                  int64
	RoomID              int64
	DisplayName         string
	RoomName            string
	Role                string
	ParentName          string
	CurrentPower        string
	TodayEnergy         string
	BillingEnergy       string
	CanUseInventoryRoom bool
}
