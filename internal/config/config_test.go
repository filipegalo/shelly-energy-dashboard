package config

import (
	"encoding/base64"
	"testing"
	"time"
)

func TestLoadDefaultsAndOverrides(t *testing.T) {
	t.Setenv("APP_LISTEN_ADDRESS", "127.0.0.1:9090")
	t.Setenv("APP_DATABASE_PATH", t.TempDir()+"/test.db")
	t.Setenv("APP_ENABLE_MDNS", "true")
	t.Setenv("APP_ALLOW_NON_PRIVATE_NETWORKS", "true")
	t.Setenv("APP_MAX_SUBNET_PREFIX", "20")
	t.Setenv("APP_MAX_HOSTS", "4096")
	t.Setenv("APP_DEFAULT_SCAN_CONCURRENCY", "8")
	t.Setenv("APP_DEFAULT_CONNECTION_TIMEOUT", "125ms")
	t.Setenv("APP_DEFAULT_REQUEST_TIMEOUT", "750ms")
	t.Setenv("APP_OFFLINE_FAILURE_THRESHOLD", "4")
	t.Setenv("APP_ENCRYPTION_KEY", base64.StdEncoding.EncodeToString([]byte("0123456789abcdef0123456789abcdef")))
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ListenAddress != "127.0.0.1:9090" || !cfg.EnableMDNS || !cfg.AllowNonPrivateNetworks {
		t.Fatalf("bad configuration: %+v", cfg)
	}
	if cfg.MaxSubnetPrefix != 20 || cfg.MaxHosts != 4096 || cfg.DefaultScanConcurrency != 8 || cfg.OfflineFailureThreshold != 4 {
		t.Fatalf("bad numeric values: %+v", cfg)
	}
	if cfg.DefaultConnectionTimeout != 125*time.Millisecond || cfg.DefaultRequestTimeout != 750*time.Millisecond || len(cfg.EncryptionKey) != 32 {
		t.Fatalf("bad durations/key: %+v", cfg)
	}
	if cfg.String() == "" {
		t.Fatal("empty String output")
	}
}

func TestLoadRejectsUnsafeSettings(t *testing.T) {
	tests := []struct{ name, key, value string }{
		{name: "key", key: "APP_ENCRYPTION_KEY", value: "short"},
		{name: "prefix", key: "APP_MAX_SUBNET_PREFIX", value: "32"},
		{name: "concurrency", key: "APP_DEFAULT_SCAN_CONCURRENCY", value: "0"},
		{name: "hosts", key: "APP_MAX_HOSTS", value: "0"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("APP_ENCRYPTION_KEY", "")
			t.Setenv("APP_MAX_SUBNET_PREFIX", "16")
			t.Setenv("APP_DEFAULT_SCAN_CONCURRENCY", "32")
			t.Setenv("APP_MAX_HOSTS", "65534")
			t.Setenv(tt.key, tt.value)
			if _, err := Load(); err == nil {
				t.Fatal("expected configuration error")
			}
		})
	}
}

func TestEnvironmentHelpersUseFallbackForInvalidValues(t *testing.T) {
	t.Setenv("BOOL", "wat")
	t.Setenv("INT", "wat")
	t.Setenv("DURATION", "wat")
	if envBool("BOOL", true) != true || envInt("INT", 7) != 7 || envDuration("DURATION", time.Second) != time.Second {
		t.Fatal("invalid environment values did not use fallbacks")
	}
}
