package shelly

import "encoding/json"

type DeviceInfo struct {
	ShellyDeviceID         string
	Name                   string
	Model                  string
	Generation             int
	DeviceType             string
	MACAddress             string
	Hostname               string
	FirmwareVersion        string
	FirmwareBuild          string
	HardwareRevision       string
	AuthenticationRequired bool
	Capabilities           []string
	RawDeviceInfo          json.RawMessage
	RawStatus              json.RawMessage
}
