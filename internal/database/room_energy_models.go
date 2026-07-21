package database

import "time"

const (
	RoomEnergyRolePrimary  = "primary"
	RoomEnergyRoleSubmeter = "submeter"
	RoomEnergyRoleExcluded = "excluded"
)

type RoomEnergyConfig struct {
	ID                   int64
	RelativeTolerance    float64
	AbsoluteToleranceWh  float64
	DiagnosticCollection bool
	CreatedAt            time.Time
	UpdatedAt            time.Time
}

type RoomEnergyAssignment struct {
	ID                    int64
	RoomID                int64
	RoomName              string
	RoomArchived          bool
	DeviceID              int64
	DeviceName            string
	DeviceModel           string
	IPAddress             string
	ComponentKey          string
	ComponentType         string
	DisplayName           string
	Enabled               bool
	AggregationRole       string
	ParentAssignmentID    int64
	ParentDisplayName     string
	PollIntervalSeconds   int
	MainMeterAcknowledged bool
	EffectiveFrom         time.Time
	EffectiveTo           *time.Time
	CreatedAt             time.Time
	UpdatedAt             time.Time
}

// RoomEnergyAssignmentRevision describes the next effective-dated version of
// an assignment. Source identity is intentionally absent and remains immutable.
type RoomEnergyAssignmentRevision struct {
	RoomID                int64
	DisplayName           string
	Enabled               bool
	AggregationRole       string
	ParentAssignmentID    int64
	PollIntervalSeconds   int
	MainMeterAcknowledged bool
	EffectiveFrom         time.Time
}
