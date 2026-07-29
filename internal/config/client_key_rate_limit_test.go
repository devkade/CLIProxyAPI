package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadConfigClientKeyRateLimitDefaultsDisabled(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("port: 8317\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	if cfg.ClientKeyRateLimit.Enabled {
		t.Fatal("client key rate limiting is enabled by default")
	}
	if cfg.ClientKeyRateLimit.MaxTrackedKeys != DefaultClientKeyRateLimitMaxTrackedKeys {
		t.Fatalf("max tracked keys = %d, want %d", cfg.ClientKeyRateLimit.MaxTrackedKeys, DefaultClientKeyRateLimitMaxTrackedKeys)
	}
}

func TestLoadConfigValidatesClientKeyRateLimit(t *testing.T) {
	tests := []struct{ name, yaml, want string }{
		{"missing request rate", "enabled: true\nburst: 1\n", "requests-per-minute must be greater than zero"},
		{"missing burst", "enabled: true\nrequests-per-minute: 60\n", "burst must be greater than zero"},
		{"negative disabled rate", "enabled: false\nrequests-per-minute: -1\n", "requests-per-minute must not be negative"},
		{"negative disabled burst", "enabled: false\nburst: -1\n", "burst must not be negative"},
		{"invalid bound", "enabled: true\nrequests-per-minute: 60\nburst: 1\nmax-tracked-keys: -1\n", "max-tracked-keys must be greater than zero"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.yaml")
			lines := strings.Split(test.yaml, "\n")
			for i := range lines {
				if lines[i] != "" {
					lines[i] = "  " + lines[i]
				}
			}
			if err := os.WriteFile(path, []byte("client-key-rate-limit:\n"+strings.Join(lines, "\n")), 0o600); err != nil {
				t.Fatal(err)
			}
			_, err := LoadConfig(path)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("LoadConfig() error = %v, want containing %q", err, test.want)
			}
		})
	}
}
