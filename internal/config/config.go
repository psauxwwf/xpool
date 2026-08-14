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
	DefaultRetireFailures   = 1
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
	MinimumLevel string `yaml:"minimum_level"`
	FilePath     string `yaml:"file_path"`
}

type SourceConfig struct {
	ProxyListFilePath string `yaml:"proxy_list_file_path"`
}

type XrayConfig struct {
	ExecutablePath          string   `yaml:"executable_path"`
	GeneratedConfigPath     string   `yaml:"generated_config_path"`
	GRPCAPIAddress          string   `yaml:"grpc_api_address"`
	GeneratedConfigLogLevel string   `yaml:"generated_config_log_level"`
	ObservatoryPingTimeout  Duration `yaml:"observatory_ping_timeout"`
	ObservatorySampling     int      `yaml:"observatory_sampling"`
}

type StatusConfig struct {
	ListenAddress string `yaml:"listen_address"`
}

type RuntimeConfig struct {
	ProxyRotationInterval   Duration `yaml:"proxy_rotation_interval"`
	StartupReadyTimeout     Duration `yaml:"startup_ready_timeout"`
	FailoverAttemptCooldown Duration `yaml:"failover_attempt_cooldown"`
}

type HealthConfig struct {
	FullDownloadCheckURLs     []string `yaml:"full_download_check_urls"`
	ActiveRoutesCheckInterval Duration `yaml:"active_routes_check_interval"`
	SuccessfulCheckReadyTTL   Duration `yaml:"successful_check_ready_ttl"`
	FullDownloadCheckTimeout  Duration `yaml:"full_download_check_timeout"`
	MaxConcurrentChecks       int      `yaml:"max_concurrent_checks"`
	CheckStartJitter          Duration `yaml:"check_start_jitter"`
	MaxDownloadBytes          int64    `yaml:"max_download_bytes"`
	RequiredSuccessfulChecks  int      `yaml:"required_successful_checks"`
	FailedChecksBeforeRetire  int      `yaml:"failed_checks_before_retire"`
}

type Duration time.Duration

func Default() Config {
	return Config{
		Log: LogConfig{
			MinimumLevel: DefaultLogLevel,
		},
		Source: SourceConfig{
			ProxyListFilePath: DefaultInputPath,
		},
		Xray: XrayConfig{
			ExecutablePath:          DefaultXrayPath,
			GeneratedConfigPath:     DefaultGeneratedPath,
			GRPCAPIAddress:          DefaultAPIAddress,
			GeneratedConfigLogLevel: DefaultGeneratedLogLevel,
			ObservatoryPingTimeout:  Duration(DefaultPingTimeout),
			ObservatorySampling:     DefaultSampling,
		},
		Status: StatusConfig{
			ListenAddress: DefaultStatusAddress,
		},
		Runtime: RuntimeConfig{
			ProxyRotationInterval:   Duration(DefaultRotationInterval),
			StartupReadyTimeout:     Duration(DefaultStartupTimeout),
			FailoverAttemptCooldown: Duration(DefaultFailoverCooldown),
		},
		Health: HealthConfig{
			FullDownloadCheckURLs:     []string{DefaultCheckURL},
			ActiveRoutesCheckInterval: Duration(DefaultCheckInterval),
			SuccessfulCheckReadyTTL:   Duration(DefaultReadyTTL),
			FullDownloadCheckTimeout:  Duration(DefaultCheckTimeout),
			MaxConcurrentChecks:       DefaultCheckConcurrency,
			CheckStartJitter:          Duration(DefaultCheckJitter),
			MaxDownloadBytes:          DefaultCheckMaxBytes,
			RequiredSuccessfulChecks:  DefaultReadySuccesses,
			FailedChecksBeforeRetire:  DefaultRetireFailures,
		},
	}
}

func New(path string) (Config, error) {
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

func Load(path string) (Config, error) {
	return New(path)
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
	if strings.TrimSpace(config.Log.MinimumLevel) == "" {
		return fmt.Errorf("log.minimum_level is required")
	}
	if strings.TrimSpace(config.Source.ProxyListFilePath) == "" {
		return fmt.Errorf("source.proxy_list_file_path is required")
	}
	if strings.TrimSpace(config.Xray.ExecutablePath) == "" {
		return fmt.Errorf("xray.executable_path is required")
	}
	if strings.TrimSpace(config.Xray.GeneratedConfigPath) == "" {
		return fmt.Errorf("xray.generated_config_path is required")
	}
	if strings.TrimSpace(config.Xray.GRPCAPIAddress) == "" {
		return fmt.Errorf("xray.grpc_api_address is required")
	}
	if strings.TrimSpace(config.Xray.GeneratedConfigLogLevel) == "" {
		return fmt.Errorf("xray.generated_config_log_level is required")
	}
	if config.Xray.ObservatorySampling <= 0 {
		return fmt.Errorf("xray.observatory_sampling must be positive")
	}
	if strings.TrimSpace(config.Status.ListenAddress) == "" {
		return fmt.Errorf("status.listen_address is required")
	}
	if err := validatePositiveDuration("xray.observatory_ping_timeout", config.Xray.ObservatoryPingTimeout); err != nil {
		return err
	}
	if err := validatePositiveDuration("runtime.proxy_rotation_interval", config.Runtime.ProxyRotationInterval); err != nil {
		return err
	}
	if err := validatePositiveDuration("runtime.startup_ready_timeout", config.Runtime.StartupReadyTimeout); err != nil {
		return err
	}
	if time.Duration(config.Runtime.FailoverAttemptCooldown) < 0 {
		return fmt.Errorf("runtime.failover_attempt_cooldown cannot be negative")
	}
	if len(config.Health.FullDownloadCheckURLs) == 0 {
		return fmt.Errorf("health.full_download_check_urls must not be empty")
	}
	for _, rawURL := range config.Health.FullDownloadCheckURLs {
		if err := validateHTTPURL(rawURL); err != nil {
			return fmt.Errorf("health.full_download_check_urls: %w", err)
		}
	}
	if err := validatePositiveDuration("health.active_routes_check_interval", config.Health.ActiveRoutesCheckInterval); err != nil {
		return err
	}
	if err := validatePositiveDuration("health.successful_check_ready_ttl", config.Health.SuccessfulCheckReadyTTL); err != nil {
		return err
	}
	if err := validatePositiveDuration("health.full_download_check_timeout", config.Health.FullDownloadCheckTimeout); err != nil {
		return err
	}
	if time.Duration(config.Health.CheckStartJitter) < 0 {
		return fmt.Errorf("health.check_start_jitter cannot be negative")
	}
	if config.Health.MaxConcurrentChecks <= 0 {
		return fmt.Errorf("health.max_concurrent_checks must be positive")
	}
	if config.Health.MaxDownloadBytes < 0 {
		return fmt.Errorf("health.max_download_bytes cannot be negative")
	}
	if config.Health.RequiredSuccessfulChecks <= 0 {
		return fmt.Errorf("health.required_successful_checks must be positive")
	}
	if config.Health.FailedChecksBeforeRetire <= 0 {
		return fmt.Errorf("health.failed_checks_before_retire must be positive")
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
