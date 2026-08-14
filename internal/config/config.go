package config

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

const (
	DefaultConfigPath        = "xpool.yaml"
	DefaultOverridePath      = "xpool.override.yaml"
	DefaultInputPath         = "proxy.txt"
	DefaultGeneratedPath     = "config.json"
	DefaultXrayPath          = "xray"
	DefaultAPIAddress        = "127.0.0.1:10085"
	DefaultStatusAddress     = "127.0.0.1:18080"
	DefaultCheckURL          = "https://web.telegram.org/js/app.js"
	DefaultGeneratedLogLevel = "warning"
	DefaultLogLevel          = "info"
)

const (
	DefaultRotationInterval = time.Minute
	DefaultCheckInterval    = time.Minute
	DefaultReadyTTL         = 10 * time.Minute
	DefaultStartupTimeout   = 30 * time.Second
	DefaultCheckTimeout     = 3 * time.Second
	DefaultCheckJitter      = 5 * time.Second
	DefaultPingTimeout      = 5 * time.Second
	DefaultFailoverCooldown = 5 * time.Second
	DefaultCheckMaxBytes    = 10 * 1024 * 1024
	DefaultCheckConcurrency = 32
	DefaultReadySuccesses   = 1
	DefaultSampling         = 3
)

type Config struct {
	Log     LogConfig     `yaml:"log"`
	Source  SourceConfig  `yaml:"source"`
	Xray    XrayConfig    `yaml:"xray"`
	Status  StatusConfig  `yaml:"status"`
	Runtime RuntimeConfig `yaml:"runtime"`
	Health  HealthConfig  `yaml:"health"`
}

type LogConfig struct {
	Level string `yaml:"level"`
	Path  string `yaml:"path"`
}

type SourceConfig struct {
	File string `yaml:"file"`
}

type XrayConfig struct {
	BinaryPath        string   `yaml:"binary_path"`
	GeneratedPath     string   `yaml:"generated_path"`
	APIAddress        string   `yaml:"api_address"`
	GeneratedLogLevel string   `yaml:"generated_log_level"`
	PingTimeout       Duration `yaml:"ping_timeout"`
	Sampling          int      `yaml:"sampling"`
}

type StatusConfig struct {
	Address string `yaml:"address"`
}

type RuntimeConfig struct {
	RotationInterval Duration `yaml:"rotation_interval"`
	StartupTimeout   Duration `yaml:"startup_timeout"`
	FailoverCooldown Duration `yaml:"failover_cooldown"`
}

type HealthConfig struct {
	CheckURLs      []string `yaml:"check_urls"`
	CheckInterval  Duration `yaml:"check_interval"`
	ReadyTTL       Duration `yaml:"ready_ttl"`
	Timeout        Duration `yaml:"timeout"`
	Concurrency    int      `yaml:"concurrency"`
	Jitter         Duration `yaml:"jitter"`
	MaxBytes       int64    `yaml:"max_bytes"`
	ReadySuccesses int      `yaml:"ready_successes"`
}

type Duration time.Duration

func Default() Config {
	return Config{
		Log: LogConfig{
			Level: DefaultLogLevel,
		},
		Source: SourceConfig{
			File: DefaultInputPath,
		},
		Xray: XrayConfig{
			BinaryPath:        DefaultXrayPath,
			GeneratedPath:     DefaultGeneratedPath,
			APIAddress:        DefaultAPIAddress,
			GeneratedLogLevel: DefaultGeneratedLogLevel,
			PingTimeout:       Duration(DefaultPingTimeout),
			Sampling:          DefaultSampling,
		},
		Status: StatusConfig{
			Address: DefaultStatusAddress,
		},
		Runtime: RuntimeConfig{
			RotationInterval: Duration(DefaultRotationInterval),
			StartupTimeout:   Duration(DefaultStartupTimeout),
			FailoverCooldown: Duration(DefaultFailoverCooldown),
		},
		Health: HealthConfig{
			CheckURLs:      []string{DefaultCheckURL},
			CheckInterval:  Duration(DefaultCheckInterval),
			ReadyTTL:       Duration(DefaultReadyTTL),
			Timeout:        Duration(DefaultCheckTimeout),
			Concurrency:    DefaultCheckConcurrency,
			Jitter:         Duration(DefaultCheckJitter),
			MaxBytes:       DefaultCheckMaxBytes,
			ReadySuccesses: DefaultReadySuccesses,
		},
	}
}

func Load(path string) (Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Config{}, fmt.Errorf("read config %s: %w", path, err)
	}

	config := Default()
	if err := yaml.Unmarshal(data, &config); err != nil {
		return Config{}, fmt.Errorf("parse config %s: %w", path, err)
	}
	if err := config.Validate(); err != nil {
		return Config{}, fmt.Errorf("validate config %s: %w", path, err)
	}

	return config, nil
}

func Save(path string, config Config) error {
	data, err := yaml.Marshal(config)
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create config dir %s: %w", path, err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("write config %s: %w", path, err)
	}

	return nil
}

func (config Config) Validate() error {
	if strings.TrimSpace(config.Log.Level) == "" {
		return fmt.Errorf("log.level is required")
	}
	if strings.TrimSpace(config.Source.File) == "" {
		return fmt.Errorf("source.file is required")
	}
	if strings.TrimSpace(config.Xray.BinaryPath) == "" {
		return fmt.Errorf("xray.binary_path is required")
	}
	if strings.TrimSpace(config.Xray.GeneratedPath) == "" {
		return fmt.Errorf("xray.generated_path is required")
	}
	if strings.TrimSpace(config.Xray.APIAddress) == "" {
		return fmt.Errorf("xray.api_address is required")
	}
	if strings.TrimSpace(config.Xray.GeneratedLogLevel) == "" {
		return fmt.Errorf("xray.generated_log_level is required")
	}
	if config.Xray.Sampling <= 0 {
		return fmt.Errorf("xray.sampling must be positive")
	}
	if strings.TrimSpace(config.Status.Address) == "" {
		return fmt.Errorf("status.address is required")
	}
	if err := validatePositiveDuration("xray.ping_timeout", config.Xray.PingTimeout); err != nil {
		return err
	}
	if err := validatePositiveDuration("runtime.rotation_interval", config.Runtime.RotationInterval); err != nil {
		return err
	}
	if err := validatePositiveDuration("runtime.startup_timeout", config.Runtime.StartupTimeout); err != nil {
		return err
	}
	if time.Duration(config.Runtime.FailoverCooldown) < 0 {
		return fmt.Errorf("runtime.failover_cooldown cannot be negative")
	}
	if len(config.Health.CheckURLs) == 0 {
		return fmt.Errorf("health.check_urls must not be empty")
	}
	for _, rawURL := range config.Health.CheckURLs {
		if err := validateHTTPURL(rawURL); err != nil {
			return fmt.Errorf("health.check_urls: %w", err)
		}
	}
	if err := validatePositiveDuration("health.check_interval", config.Health.CheckInterval); err != nil {
		return err
	}
	if err := validatePositiveDuration("health.ready_ttl", config.Health.ReadyTTL); err != nil {
		return err
	}
	if err := validatePositiveDuration("health.timeout", config.Health.Timeout); err != nil {
		return err
	}
	if time.Duration(config.Health.Jitter) < 0 {
		return fmt.Errorf("health.jitter cannot be negative")
	}
	if config.Health.Concurrency <= 0 {
		return fmt.Errorf("health.concurrency must be positive")
	}
	if config.Health.MaxBytes < 0 {
		return fmt.Errorf("health.max_bytes cannot be negative")
	}
	if config.Health.ReadySuccesses <= 0 {
		return fmt.Errorf("health.ready_successes must be positive")
	}

	return nil
}

func (duration Duration) MarshalYAML() (any, error) {
	return time.Duration(duration).String(), nil
}

func (duration *Duration) UnmarshalYAML(value *yaml.Node) error {
	parsed, err := ParseDuration(value.Value)
	if err != nil {
		return err
	}

	*duration = Duration(parsed)
	return nil
}

func (duration Duration) Duration() time.Duration {
	return time.Duration(duration)
}

func ParseDuration(value string) (time.Duration, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, fmt.Errorf("duration is empty")
	}
	if isDigits(value) {
		value += "m"
	}

	return time.ParseDuration(value)
}

func ExistingPath(configPath string) string {
	if strings.TrimSpace(configPath) != "" {
		return configPath
	}
	if _, err := os.Stat(DefaultConfigPath); err == nil || !os.IsNotExist(err) {
		return DefaultConfigPath
	}
	if _, err := os.Stat(DefaultOverridePath); err == nil || !os.IsNotExist(err) {
		return DefaultOverridePath
	}

	return DefaultConfigPath
}

func validatePositiveDuration(name string, duration Duration) error {
	if time.Duration(duration) <= 0 {
		return fmt.Errorf("%s must be positive", name)
	}
	return nil
}

func validateHTTPURL(rawURL string) error {
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return fmt.Errorf("parse %q: %w", rawURL, err)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" || parsed.Host == "" {
		return fmt.Errorf("invalid URL %q", rawURL)
	}
	return nil
}

func isDigits(value string) bool {
	for _, r := range value {
		if r < '0' || r > '9' {
			return false
		}
	}

	return true
}
