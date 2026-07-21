package scans

import (
	"context"
	"net/netip"
	"time"

	"github.com/grandcat/zeroconf"
)

func appendMDNSCandidates(parent context.Context, existing []netip.Addr, allowed netip.Prefix) []netip.Addr {
	resolver, err := zeroconf.NewResolver(nil)
	if err != nil {
		return existing
	}
	ctx, cancel := context.WithTimeout(parent, 750*time.Millisecond)
	defer cancel()
	entries := make(chan *zeroconf.ServiceEntry, 16)
	for _, service := range []string{"_shelly._tcp", "_http._tcp"} {
		_ = resolver.Browse(ctx, service, "local.", entries)
	}
	discovered := []netip.Addr{}
	for {
		select {
		case <-ctx.Done():
			return prioritizeCandidates(existing, discovered, allowed)
		case entry := <-entries:
			if entry == nil {
				continue
			}
			for _, address := range entry.AddrIPv4 {
				candidate, ok := netip.AddrFromSlice(address)
				if ok {
					discovered = append(discovered, candidate)
				}
			}
		}
	}
}

func prioritizeCandidates(existing, discovered []netip.Addr, allowed netip.Prefix) []netip.Addr {
	usable := make(map[netip.Addr]struct{}, len(existing))
	for _, address := range existing {
		usable[address] = struct{}{}
	}
	seen := make(map[netip.Addr]struct{}, len(existing))
	result := make([]netip.Addr, 0, len(existing))
	for _, candidate := range discovered {
		if !allowed.Contains(candidate) {
			continue
		}
		if _, ok := usable[candidate]; !ok {
			continue
		}
		if _, duplicate := seen[candidate]; duplicate {
			continue
		}
		seen[candidate] = struct{}{}
		result = append(result, candidate)
	}
	for _, address := range existing {
		if _, prioritized := seen[address]; !prioritized {
			result = append(result, address)
		}
	}
	return result
}
