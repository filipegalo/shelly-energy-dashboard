package devices

import (
	"encoding/hex"
	"errors"
	"strings"
)

type IdentityKind string

const (
	IdentityShellyID IdentityKind = "shelly_device_id"
	IdentityMAC      IdentityKind = "mac_address"
	IdentityIP       IdentityKind = "ip_address"
)

type Identity struct {
	Kind  IdentityKind
	Value string
}

func NormalizeMAC(raw string) (string, error) {
	normalized := strings.NewReplacer(":", "", "-", "", ".", "").Replace(strings.TrimSpace(raw))
	normalized = strings.ToLower(normalized)
	if len(normalized) != 12 {
		return "", errors.New("MAC address must contain 12 hexadecimal digits")
	}
	if _, err := hex.DecodeString(normalized); err != nil {
		return "", errors.New("MAC address contains invalid characters")
	}
	return normalized, nil
}

func StableIdentity(shellyID, macAddress, ipAddress string) Identity {
	if value := strings.ToLower(strings.TrimSpace(shellyID)); value != "" {
		return Identity{Kind: IdentityShellyID, Value: value}
	}
	if value, err := NormalizeMAC(macAddress); err == nil {
		return Identity{Kind: IdentityMAC, Value: value}
	}
	return Identity{Kind: IdentityIP, Value: strings.TrimSpace(ipAddress)}
}
