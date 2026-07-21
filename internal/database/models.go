package database

import (
	"encoding/json"
	"time"
)

type Network struct {
	ID                   int64
	Name                 string
	CIDR                 string
	Enabled              bool
	AutomaticScanEnabled bool
	ScanIntervalSeconds  int
	ScanConcurrency      int
	ConnectionTimeoutMS  int
	RequestTimeoutMS     int
	CreatedAt            time.Time
	UpdatedAt            time.Time
}

type Room struct {
	ID           int64
	Name         string
	Description  string
	Icon         string
	SortOrder    int
	IsSystem     bool
	Archived     bool
	ArchivedAt   *time.Time
	DeviceCount  int
	OnlineCount  int
	OfflineCount int
}

type Device struct {
	ID                     int64
	ShellyDeviceID         string
	Name                   string
	CustomName             string
	Model                  string
	Generation             int
	DeviceType             string
	MACAddress             string
	IPAddress              string
	Hostname               string
	FirmwareVersion        string
	FirmwareBuild          string
	HardwareRevision       string
	RoomID                 int64
	RoomName               string
	NetworkID              int64
	NetworkName            string
	CredentialProfileID    int64
	AuthenticationRequired bool
	OnlineStatus           string
	ConsecutiveFailures    int
	CapabilitiesJSON       string
	FirstSeenAt            time.Time
	LastSeenAt             time.Time
	LastSuccessfulScanAt   *time.Time
	LastError              string
	RawDeviceInfo          string
	RawStatus              string
}

func (d Device) DisplayName() string {
	if d.CustomName != "" {
		return d.CustomName
	}
	if d.Name != "" {
		return d.Name
	}
	return d.Model
}

func (d Device) Capabilities() []string {
	var values []string
	_ = json.Unmarshal([]byte(d.CapabilitiesJSON), &values)
	return values
}

type CredentialProfile struct {
	ID       int64
	Name     string
	Username string
}

type Scan struct {
	ID               int64      `json:"id"`
	NetworkID        int64      `json:"network_id"`
	NetworkName      string     `json:"network_name"`
	TriggerType      string     `json:"trigger_type"`
	Status           string     `json:"status"`
	StartedAt        *time.Time `json:"started_at,omitempty"`
	CompletedAt      *time.Time `json:"completed_at,omitempty"`
	AddressesTotal   int        `json:"addresses_total"`
	AddressesScanned int        `json:"addresses_scanned"`
	DevicesFound     int        `json:"devices_found"`
	NewDevicesFound  int        `json:"new_devices_found"`
	UpdatedDevices   int        `json:"updated_devices"`
	ErrorsCount      int        `json:"errors_count"`
	ErrorSummary     string     `json:"error_summary,omitempty"`
	CreatedAt        time.Time  `json:"created_at"`
}

func (s Scan) Percentage() int {
	if s.AddressesTotal == 0 {
		return 0
	}
	return s.AddressesScanned * 100 / s.AddressesTotal
}
