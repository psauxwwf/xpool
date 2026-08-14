package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestDefaultConfigIsValid(t *testing.T) {
	if err := Default().Validate(); err != nil {
		t.Fatalf("default config must be valid: %v", err)
	}
}

func TestLoadMergesDefaults(t *testing.T) {
	path := filepath.Join(t.TempDir(), "xpool.yaml")
	if err := os.WriteFile(path, []byte("source:\n  proxy_list_file_path: custom.txt\nhealth:\n  max_concurrent_checks: 7\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	config, err := New(path)
	if err != nil {
		t.Fatal(err)
	}
	if config.Source.ProxyListFilePath != "custom.txt" {
		t.Fatalf("source file = %q", config.Source.ProxyListFilePath)
	}
	if config.Health.MaxConcurrentChecks != 7 {
		t.Fatalf("health concurrency = %d", config.Health.MaxConcurrentChecks)
	}
	if config.Xray.GRPCAPIAddress != DefaultAPIAddress {
		t.Fatalf("xray api = %q", config.Xray.GRPCAPIAddress)
	}
}

func TestDurationAcceptsBareMinutes(t *testing.T) {
	duration, err := ParseDuration("15")
	if err != nil {
		t.Fatal(err)
	}
	if duration != 15*time.Minute {
		t.Fatalf("duration = %s", duration)
	}
}
