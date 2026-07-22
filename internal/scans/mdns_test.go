package scans

import (
	"context"
	"testing"
	"time"

	"github.com/grandcat/zeroconf"
)

func TestMergeMDNSEntriesAllowsEveryBrowseToCloseItsOwnChannel(t *testing.T) {
	t.Parallel()

	first := make(chan *zeroconf.ServiceEntry, 1)
	second := make(chan *zeroconf.ServiceEntry, 1)
	first <- &zeroconf.ServiceEntry{ServiceRecord: zeroconf.ServiceRecord{Instance: "shelly"}}
	second <- &zeroconf.ServiceEntry{ServiceRecord: zeroconf.ServiceRecord{Instance: "http"}}
	close(first)
	close(second)

	merged := mergeMDNSEntries(context.Background(), first, second)
	instances := map[string]bool{}
	for entry := range merged {
		instances[entry.Instance] = true
	}

	if !instances["shelly"] || !instances["http"] {
		t.Fatalf("merged instances = %v, want shelly and http", instances)
	}
}

func TestForwardMDNSEntriesDrainsSourceAfterCancellation(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	source := make(chan *zeroconf.ServiceEntry, 1)
	source <- &zeroconf.ServiceEntry{}
	merged := make(chan *zeroconf.ServiceEntry)
	done := make(chan struct{})
	go func() {
		forwardMDNSEntries(ctx, source, merged)
		close(done)
	}()

	select {
	case source <- &zeroconf.ServiceEntry{}:
	case <-time.After(time.Second):
		t.Fatal("cancelled forwarder did not drain its source")
	}
	close(source)

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("cancelled forwarder did not stop after its source closed")
	}
}
