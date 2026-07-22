package scans

import (
	"context"
	"net/netip"
	"sync"
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
	browseResults := make([]<-chan *zeroconf.ServiceEntry, 0, 2)
	for _, service := range []string{"_shelly._tcp", "_http._tcp"} {
		entries := make(chan *zeroconf.ServiceEntry, 16)
		if err := resolver.Browse(ctx, service, "local.", entries); err == nil {
			browseResults = append(browseResults, entries)
		}
	}
	entries := mergeMDNSEntries(ctx, browseResults...)
	discovered := []netip.Addr{}
	for {
		select {
		case <-ctx.Done():
			return prioritizeCandidates(existing, discovered, allowed)
		case entry, ok := <-entries:
			if !ok {
				return prioritizeCandidates(existing, discovered, allowed)
			}
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

func mergeMDNSEntries(ctx context.Context, sources ...<-chan *zeroconf.ServiceEntry) <-chan *zeroconf.ServiceEntry {
	merged := make(chan *zeroconf.ServiceEntry, 16)
	var forwarders sync.WaitGroup
	forwarders.Add(len(sources))
	for _, source := range sources {
		go func() {
			defer forwarders.Done()
			forwardMDNSEntries(ctx, source, merged)
		}()
	}
	go func() {
		forwarders.Wait()
		close(merged)
	}()
	return merged
}

func forwardMDNSEntries(ctx context.Context, source <-chan *zeroconf.ServiceEntry, merged chan<- *zeroconf.ServiceEntry) {
	for {
		select {
		case <-ctx.Done():
			drainMDNSEntries(source)
			return
		case entry, ok := <-source:
			if !ok {
				return
			}
			select {
			case merged <- entry:
			case <-ctx.Done():
				drainMDNSEntries(source)
				return
			}
		}
	}
}

func drainMDNSEntries(source <-chan *zeroconf.ServiceEntry) {
	for range source {
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
