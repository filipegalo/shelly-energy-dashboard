package config

import (
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	ListenAddress            string
	DatabasePath             string
	DatabaseUsername         string
	DatabasePassword         string
	DatabaseHostname         string
	DatabasePort             string
	DatabaseDB               string
	DatabaseSSLMode          string
	EncryptionKey            []byte
	LogLevel                 string
	EnableMDNS               bool
	AllowNonPrivateNetworks  bool
	MaxSubnetPrefix          int
	MaxHosts                 int
	DefaultScanConcurrency   int
	DefaultConnectionTimeout time.Duration
	DefaultRequestTimeout    time.Duration
	OfflineFailureThreshold  int
	AllowedHosts             []string
	SecureCookies            bool
	MaxConcurrentScans       int
}

func Load() (Config, error) {
	cfg := Config{
		ListenAddress:            env("APP_LISTEN_ADDRESS", "127.0.0.1:8080"),
		DatabasePath:             env("APP_DATABASE_PATH", "./data/shelly-inventory.db"),
		DatabaseUsername:         os.Getenv("DATABASE_USERNAME"),
		DatabasePassword:         os.Getenv("DATABASE_PASSWORD"),
		DatabaseHostname:         os.Getenv("DATABASE_HOSTNAME"),
		DatabasePort:             env("DATABASE_PORT", "5432"),
		DatabaseDB:               os.Getenv("DATABASE_DB"),
		DatabaseSSLMode:          env("DATABASE_SSLMODE", "disable"),
		LogLevel:                 env("APP_LOG_LEVEL", "info"),
		EnableMDNS:               envBool("APP_ENABLE_MDNS", false),
		AllowNonPrivateNetworks:  envBool("APP_ALLOW_NON_PRIVATE_NETWORKS", false),
		MaxSubnetPrefix:          envInt("APP_MAX_SUBNET_PREFIX", 16),
		MaxHosts:                 envInt("APP_MAX_HOSTS", 65534),
		DefaultScanConcurrency:   envInt("APP_DEFAULT_SCAN_CONCURRENCY", 32),
		DefaultConnectionTimeout: envDuration("APP_DEFAULT_CONNECTION_TIMEOUT", 750*time.Millisecond),
		DefaultRequestTimeout:    envDuration("APP_DEFAULT_REQUEST_TIMEOUT", 2*time.Second),
		OfflineFailureThreshold:  envInt("APP_OFFLINE_FAILURE_THRESHOLD", 3),
		AllowedHosts:             splitCSV(env("APP_ALLOWED_HOSTS", "localhost,127.0.0.1,[::1]")),
		SecureCookies:            envBool("APP_SECURE_COOKIES", false),
		MaxConcurrentScans:       envInt("APP_MAX_CONCURRENT_SCANS", 4),
	}
	postgresValues := []string{cfg.DatabaseUsername, cfg.DatabasePassword, cfg.DatabaseHostname, cfg.DatabaseDB}
	postgresConfigured := 0
	for _, value := range postgresValues {
		if value != "" {
			postgresConfigured++
		}
	}
	if postgresConfigured != 0 && postgresConfigured != len(postgresValues) {
		return Config{}, errors.New("DATABASE_USERNAME, DATABASE_PASSWORD, DATABASE_HOSTNAME, and DATABASE_DB must be set together")
	}
	key, err := encryptionKey(os.Getenv("APP_ENCRYPTION_KEY"))
	if err != nil {
		return Config{}, err
	}
	cfg.EncryptionKey = key
	if cfg.MaxSubnetPrefix < 8 || cfg.MaxSubnetPrefix > 30 {
		return Config{}, errors.New("APP_MAX_SUBNET_PREFIX must be between 8 and 30")
	}
	if cfg.DefaultScanConcurrency < 1 || cfg.DefaultScanConcurrency > 256 {
		return Config{}, errors.New("APP_DEFAULT_SCAN_CONCURRENCY must be between 1 and 256")
	}
	if cfg.MaxHosts < 1 || cfg.OfflineFailureThreshold < 1 {
		return Config{}, errors.New("host and offline thresholds must be positive")
	}
	if cfg.MaxConcurrentScans < 1 || cfg.MaxConcurrentScans > 32 {
		return Config{}, errors.New("APP_MAX_CONCURRENT_SCANS must be between 1 and 32")
	}
	return cfg, nil
}

func (c Config) UsesPostgres() bool { return c.DatabaseHostname != "" }

func splitCSV(raw string) []string {
	values := []string{}
	for _, value := range strings.Split(raw, ",") {
		value = strings.TrimSpace(strings.ToLower(value))
		if value != "" {
			values = append(values, value)
		}
	}
	return values
}

func encryptionKey(raw string) ([]byte, error) {
	if raw == "" {
		return nil, nil
	}
	decoded, err := base64.StdEncoding.DecodeString(raw)
	if err == nil && len(decoded) == 32 {
		return decoded, nil
	}
	if len(raw) == 32 {
		return []byte(raw), nil
	}
	return nil, errors.New("APP_ENCRYPTION_KEY must be 32 bytes or base64-encoded 32 bytes")
}

func env(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}

func envBool(name string, fallback bool) bool {
	value := os.Getenv(name)
	if value == "" {
		return fallback
	}
	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return fallback
	}
	return parsed
}

func envInt(name string, fallback int) int {
	value := os.Getenv(name)
	if value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return fallback
	}
	return parsed
}

func envDuration(name string, fallback time.Duration) time.Duration {
	value := os.Getenv(name)
	if value == "" {
		return fallback
	}
	parsed, err := time.ParseDuration(value)
	if err != nil {
		return fallback
	}
	return parsed
}

func (c Config) String() string {
	return fmt.Sprintf("listen=%s database=%s mdns=%t", c.ListenAddress, c.DatabasePath, c.EnableMDNS)
}
