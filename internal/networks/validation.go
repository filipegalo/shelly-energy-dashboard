package networks

import (
	"errors"
	"fmt"
	"net/netip"
)

var privatePrefixes = []netip.Prefix{
	netip.MustParsePrefix("10.0.0.0/8"),
	netip.MustParsePrefix("172.16.0.0/12"),
	netip.MustParsePrefix("192.168.0.0/16"),
}

func ValidateCIDR(raw string, allowNonPrivate bool, largestAllowedPrefix int) (netip.Prefix, error) {
	prefix, err := netip.ParsePrefix(raw)
	if err != nil {
		return netip.Prefix{}, fmt.Errorf("invalid CIDR: %w", err)
	}
	if !prefix.Addr().Is4() {
		return netip.Prefix{}, errors.New("only IPv4 networks are supported")
	}
	prefix = prefix.Masked()
	address := prefix.Addr()
	if address.IsLoopback() || address.IsLinkLocalUnicast() || address.IsMulticast() || address.IsUnspecified() || address == netip.MustParseAddr("255.255.255.255") {
		return netip.Prefix{}, errors.New("loopback, link-local, multicast, unspecified, and broadcast ranges cannot be scanned")
	}
	if largestAllowedPrefix < 0 || largestAllowedPrefix > 32 || prefix.Bits() < largestAllowedPrefix {
		return netip.Prefix{}, fmt.Errorf("network is larger than the configured /%d limit", largestAllowedPrefix)
	}
	if !allowNonPrivate && !isPrivatePrefix(prefix) {
		return netip.Prefix{}, errors.New("only RFC1918 private networks are allowed")
	}
	return prefix, nil
}

func isPrivatePrefix(prefix netip.Prefix) bool {
	for _, private := range privatePrefixes {
		if private.Contains(prefix.Addr()) && private.Bits() <= prefix.Bits() {
			return true
		}
	}
	return false
}

func UsableHosts(prefix netip.Prefix, maxHosts int) ([]netip.Addr, error) {
	if !prefix.IsValid() || !prefix.Addr().Is4() {
		return nil, errors.New("valid IPv4 prefix required")
	}
	prefix = prefix.Masked()
	bits := prefix.Bits()
	if bits < 0 || bits > 32 {
		return nil, errors.New("invalid prefix length")
	}
	count := uint64(1) << uint(32-bits)
	usable := count
	if bits <= 30 {
		usable -= 2
	}
	if usable > uint64(maxHosts) {
		return nil, fmt.Errorf("network contains %d usable addresses; limit is %d", usable, maxHosts)
	}
	addresses := make([]netip.Addr, 0, usable)
	address := prefix.Addr()
	for i := uint64(0); i < count; i++ {
		if (bits > 30 || (i != 0 && i != count-1)) && prefix.Contains(address) {
			addresses = append(addresses, address)
		}
		address = address.Next()
	}
	return addresses, nil
}

func Contains(prefixes []netip.Prefix, address netip.Addr) bool {
	if !address.Is4() {
		return false
	}
	for _, prefix := range prefixes {
		if prefix.Contains(address) {
			return true
		}
	}
	return false
}
