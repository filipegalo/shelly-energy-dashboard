package devices

import "testing"

func TestNormalizeMAC(t *testing.T) {
	t.Parallel()
	tests := map[string]string{
		"AA:BB:CC:DD:EE:FF": "aabbccddeeff",
		"aa-bb-cc-dd-ee-ff": "aabbccddeeff",
		"aabb.ccdd.eeff":    "aabbccddeeff",
	}
	for input, want := range tests {
		got, err := NormalizeMAC(input)
		if err != nil {
			t.Fatalf("NormalizeMAC(%q): %v", input, err)
		}
		if got != want {
			t.Fatalf("NormalizeMAC(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestNormalizeMACRejectsInvalid(t *testing.T) {
	t.Parallel()
	if _, err := NormalizeMAC("not-a-mac"); err == nil {
		t.Fatal("expected invalid MAC error")
	}
}

func TestStableIdentityPriority(t *testing.T) {
	t.Parallel()
	got := StableIdentity(" ShellyPlus1-ABC ", "AA:BB:CC:DD:EE:FF", "192.168.1.5")
	if got.Kind != IdentityShellyID || got.Value != "shellyplus1-abc" {
		t.Fatalf("unexpected identity: %+v", got)
	}
}

func TestStableIdentityFallsBackToMACThenIP(t *testing.T) {
	t.Parallel()
	mac := StableIdentity("", "AA:BB:CC:DD:EE:FF", "192.168.1.5")
	if mac.Kind != IdentityMAC || mac.Value != "aabbccddeeff" {
		t.Fatalf("MAC fallback = %+v", mac)
	}
	ip := StableIdentity("", "invalid", "192.168.1.5")
	if ip.Kind != IdentityIP || ip.Value != "192.168.1.5" {
		t.Fatalf("IP fallback = %+v", ip)
	}
}

func TestNormalizeMACRejectsWrongLengthAndNonHex(t *testing.T) {
	t.Parallel()
	for _, value := range []string{"aa:bb", "gg:bb:cc:dd:ee:ff"} {
		if _, err := NormalizeMAC(value); err == nil {
			t.Fatalf("accepted %q", value)
		}
	}
}
