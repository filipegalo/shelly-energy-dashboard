package networks

import (
	"net/netip"
	"reflect"
	"testing"
)

func TestValidateCIDR(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		cidr      string
		allowLAN  bool
		maxPrefix int
		want      string
		wantErr   bool
	}{
		{name: "private slash 24", cidr: "192.168.20.0/24", maxPrefix: 16, want: "192.168.20.0/24"},
		{name: "canonicalizes host bits", cidr: "10.8.1.42/24", maxPrefix: 16, want: "10.8.1.0/24"},
		{name: "rejects public", cidr: "8.8.8.0/24", maxPrefix: 16, wantErr: true},
		{name: "rejects too large", cidr: "10.0.0.0/8", maxPrefix: 16, wantErr: true},
		{name: "allows approved local range", cidr: "100.64.1.0/24", allowLAN: true, maxPrefix: 16, want: "100.64.1.0/24"},
		{name: "still rejects loopback with override", cidr: "127.0.0.0/24", allowLAN: true, maxPrefix: 16, wantErr: true},
		{name: "still rejects link local with override", cidr: "169.254.0.0/24", allowLAN: true, maxPrefix: 16, wantErr: true},
		{name: "rejects ipv6 for mvp", cidr: "fd00::/64", maxPrefix: 16, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := ValidateCIDR(tt.cidr, tt.allowLAN, tt.maxPrefix)
			if (err != nil) != tt.wantErr {
				t.Fatalf("ValidateCIDR() error = %v, wantErr %v", err, tt.wantErr)
			}
			if err == nil && got.String() != tt.want {
				t.Fatalf("ValidateCIDR() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestUsableHosts(t *testing.T) {
	t.Parallel()
	prefix := netip.MustParsePrefix("192.168.20.0/30")
	want := []netip.Addr{netip.MustParseAddr("192.168.20.1"), netip.MustParseAddr("192.168.20.2")}
	got, err := UsableHosts(prefix, 65534)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("UsableHosts() = %v, want %v", got, want)
	}
}

func TestUsableHostsRejectsExcessiveRange(t *testing.T) {
	t.Parallel()
	_, err := UsableHosts(netip.MustParsePrefix("10.0.0.0/16"), 100)
	if err == nil {
		t.Fatal("expected host limit error")
	}
}

func TestTargetBelongsToEnabledNetwork(t *testing.T) {
	t.Parallel()
	prefixes := []netip.Prefix{netip.MustParsePrefix("192.168.20.0/24")}
	if !Contains(prefixes, netip.MustParseAddr("192.168.20.42")) {
		t.Fatal("expected address to be allowed")
	}
	if Contains(prefixes, netip.MustParseAddr("192.168.21.42")) {
		t.Fatal("expected address to be rejected")
	}
}
