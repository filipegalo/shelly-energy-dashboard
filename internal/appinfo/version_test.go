package appinfo

import "testing"

func TestCurrentFallsBackToDevForBlankVersion(t *testing.T) {
	original := Version
	t.Cleanup(func() { Version = original })
	Version = "  "

	if got := Current(); got != "dev" {
		t.Fatalf("Current() = %q, want dev", got)
	}
}

func TestCurrentShortensCommitVersion(t *testing.T) {
	original := Version
	t.Cleanup(func() { Version = original })
	Version = "0123456789abcdef0123456789abcdef01234567"

	if got := Current(); got != "0123456" {
		t.Fatalf("Current() = %q, want short commit", got)
	}
}
