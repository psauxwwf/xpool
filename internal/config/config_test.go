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
	if err := os.WriteFile(path, []byte("source:\n  file: custom.txt\nhealth:\n  concurrency: 7\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	config, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if config.Source.File != "custom.txt" {
		t.Fatalf("source file = %q", config.Source.File)
	}
	if config.Health.Concurrency != 7 {
		t.Fatalf("health concurrency = %d", config.Health.Concurrency)
	}
	if config.Xray.APIAddress != DefaultAPIAddress {
		t.Fatalf("xray api = %q", config.Xray.APIAddress)
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
