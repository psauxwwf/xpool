package configgen

import (
	"fmt"
	"net"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	appconfig "xpool/internal/config"
	"xpool/pkg/fs"
	"xpool/pkg/health"
	"xpool/pkg/xray"
)

const (
	DefaultListenAddr     = "0.0.0.0"
	DefaultListenPort     = 8080
	DefaultLocalListen    = "127.0.0.1"
	DefaultLocalPort      = 8000
	DefaultCheckPortBase  = 18000
	DefaultBalancerTag    = "proxy-balancer"
	DefaultOutboundPrefix = "socks-"
)

type Result struct {
	GeneratedConfigPath string
	ProxyCount          int
	Mode                string
	Tags                []string
	CheckRoutes         []health.Route
}

type Proxy struct {
	Username string
	Password string
	Host     string
	Port     int
}

type Generator struct {
	Config appconfig.Config
}

func NewGenerator(config appconfig.Config) Generator {
	return Generator{Config: config}
}

type ParseError struct {
	Line  int    `json:"line"`
	Error string `json:"error"`
}

func (g Generator) Generate(proxies []Proxy) (Result, error) {
	config := g.Config
	if len(proxies) == 0 {
		return Result{}, fmt.Errorf("no proxies found")
	}
	if len(proxies) > 65535-DefaultCheckPortBase {
		return Result{}, fmt.Errorf("too many proxies: %d exceeds local check port capacity", len(proxies))
	}

	tags := proxyTags(len(proxies))
	checkRoutes := checkRoutes(tags)
	cfg, err := BuildConfig(proxies, config)
	if err != nil {
		return Result{}, err
	}

	if err := fs.WriteJSON(config.Xray.GeneratedConfigPath, cfg); err != nil {
		return Result{}, err
	}

	return Result{
		GeneratedConfigPath: config.Xray.GeneratedConfigPath,
		ProxyCount:          len(proxies),
		Mode:                "xray leastPing + background pool",
		Tags:                tags,
		CheckRoutes:         checkRoutes,
	}, nil
}

func ReadProxies(path string) ([]Proxy, []ParseError, error) {
	lines, err := fs.ReadLines(path)
	if err != nil {
		return nil, nil, err
	}

	var proxies []Proxy
	var parseErrors []ParseError
	for _, rawLine := range lines {
		line := strings.TrimSpace(rawLine.Text)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		parsed, err := ParseProxy(line)
		if err != nil {
			parseErrors = append(parseErrors, ParseError{Line: rawLine.Number, Error: err.Error()})
			continue
		}
		proxies = append(proxies, parsed)
	}

	return proxies, parseErrors, nil
}

func ParseProxy(raw string) (Proxy, error) {
	parsed, err := url.Parse(raw)
	if err != nil {
		return Proxy{}, fmt.Errorf("parse proxy URL: %w", err)
	}
	if parsed.Scheme != "socks5h" {
		return Proxy{}, fmt.Errorf("unsupported scheme %q, expected socks5h", parsed.Scheme)
	}
	if parsed.User == nil {
		return Proxy{}, fmt.Errorf("missing username and password")
	}

	username := parsed.User.Username()
	password, hasPassword := parsed.User.Password()
	if username == "" || !hasPassword {
		return Proxy{}, fmt.Errorf("missing username or password")
	}

	host := parsed.Hostname()
	if host == "" {
		return Proxy{}, fmt.Errorf("missing host")
	}

	portText := parsed.Port()
	if portText == "" {
		return Proxy{}, fmt.Errorf("missing port")
	}
	port, err := strconv.Atoi(portText)
	if err != nil || port < 1 || port > 65535 {
		return Proxy{}, fmt.Errorf("invalid port %q", portText)
	}

	return Proxy{Username: username, Password: password, Host: host, Port: port}, nil
}

func BuildConfig(proxies []Proxy, config appconfig.Config) (xray.Config, error) {
	apiListen, apiPort, err := splitHostPort(config.Xray.GRPCAPIAddress)
	if err != nil {
		return xray.Config{}, err
	}

	tags := proxyTags(len(proxies))
	inbounds := []xray.InboundConfig{
		{
			Tag:      "http-in",
			Listen:   DefaultListenAddr,
			Port:     DefaultListenPort,
			Protocol: "http",
			Settings: xray.HTTPInboundSettings{
				Accounts: []xray.HTTPAccount{{
					User: envOrDefault("HTTP_USERNAME", "HTTP_USERNAME"),
					Pass: envOrDefault("HTTP_PASSWORD", "HTTP_PASSWORD"),
				}},
			},
		},
		{
			Tag:      "http-local-in",
			Listen:   DefaultLocalListen,
			Port:     DefaultLocalPort,
			Protocol: "http",
			Settings: xray.HTTPInboundSettings{},
		},
		{
			Tag:      "api",
			Listen:   apiListen,
			Port:     apiPort,
			Protocol: "dokodemo-door",
			Settings: xray.DokodemoDoorSettings{Address: apiListen},
		},
	}

	rules := []xray.RoutingRule{
		{Type: "field", InboundTag: []string{"api"}, OutboundTag: "api"},
	}

	for i, tag := range tags {
		checkTag := "check-" + tag
		inbounds = append(inbounds, xray.InboundConfig{
			Tag:      checkTag,
			Listen:   DefaultLocalListen,
			Port:     DefaultCheckPortBase + i + 1,
			Protocol: "http",
			Settings: xray.HTTPInboundSettings{},
		})
		rules = append(rules, xray.RoutingRule{Type: "field", InboundTag: []string{checkTag}, OutboundTag: tag})
	}

	rules = append(rules,
		xray.RoutingRule{Type: "field", InboundTag: []string{"http-in", "http-local-in"}, BalancerTag: DefaultBalancerTag},
	)

	outbounds := make([]xray.OutboundConfig, 0, len(proxies)+2)
	for i, p := range proxies {
		outbounds = append(outbounds, xray.OutboundConfig{
			Tag:      tags[i],
			Protocol: "socks",
			Settings: xray.SocksOutboundSettings{
				Address: p.Host,
				Port:    p.Port,
				User:    p.Username,
				Pass:    p.Password,
			},
		})
	}
	outbounds = append(outbounds,
		xray.OutboundConfig{Tag: "direct", Protocol: "freedom"},
		xray.OutboundConfig{Tag: "blocked", Protocol: "blackhole"},
	)

	return xray.Config{
		Log: xray.LogConfig{LogLevel: config.Xray.GeneratedConfigLogLevel},
		API: xray.APIConfig{
			Tag:      "api",
			Services: []string{"HandlerService", "LoggerService", "RoutingService", "StatsService"},
		},
		Stats: map[string]any{},
		Policy: xray.PolicyConfig{
			Levels: map[string]xray.PolicyLevel{
				"0": {StatsUserUplink: true, StatsUserDownlink: true},
			},
			System: xray.PolicySystem{
				StatsInboundUplink:    true,
				StatsInboundDownlink:  true,
				StatsOutboundUplink:   true,
				StatsOutboundDownlink: true,
			},
		},
		Inbounds:  inbounds,
		Outbounds: outbounds,
		Routing: xray.RoutingConfig{
			Rules: rules,
			Balancers: []xray.BalancingRule{{
				Tag:         DefaultBalancerTag,
				Selector:    []string{DefaultOutboundPrefix},
				Strategy:    xray.StrategyConfig{Type: "leastPing"},
				FallbackTag: "blocked",
			}},
		},
		BurstObservatory: xray.BurstObservatoryConfig{
			SubjectSelector: []string{DefaultOutboundPrefix},
			PingConfig: xray.PingConfig{
				Destination: config.Health.FullDownloadCheckURLs[0],
				Interval:    formatDuration(config.Health.ActiveRoutesCheckInterval.Duration()),
				Sampling:    config.Xray.ObservatorySampling,
				Timeout:     formatDuration(config.Xray.ObservatoryPingTimeout.Duration()),
				HTTPMethod:  "GET",
			},
		},
	}, nil
}

func CheckRoutes(tags []string) []health.Route {
	return checkRoutes(tags)
}

func proxyTags(count int) []string {
	tags := make([]string, count)
	for i := range tags {
		tags[i] = fmt.Sprintf("%s%d", DefaultOutboundPrefix, i+1)
	}
	return tags
}

func checkRoutes(tags []string) []health.Route {
	routes := make([]health.Route, len(tags))
	for i, tag := range tags {
		routes[i] = health.Route{
			Tag:      tag,
			ProxyURL: fmt.Sprintf("http://%s:%d", DefaultLocalListen, DefaultCheckPortBase+i+1),
		}
	}
	return routes
}

func splitHostPort(address string) (string, int, error) {
	host, portText, err := net.SplitHostPort(address)
	if err != nil {
		return "", 0, fmt.Errorf("parse API address %q: %w", address, err)
	}
	port, err := strconv.Atoi(portText)
	if err != nil || port < 1 || port > 65535 {
		return "", 0, fmt.Errorf("invalid API port %q", portText)
	}
	return host, port, nil
}

func formatDuration(duration time.Duration) string {
	if duration%time.Second == 0 {
		return fmt.Sprintf("%ds", int(duration/time.Second))
	}
	return duration.String()
}

func envOrDefault(key, fallback string) string {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	return value
}
