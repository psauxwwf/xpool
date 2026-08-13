package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
)

const (
	defaultListenAddr = "0.0.0.0"
	defaultListenPort = 8080
	defaultTestURL    = "http://cachefly.cachefly.net/1mb.test"
	defaultInterval   = "1m"
	defaultIdleTime   = "10m"
	defaultTolerance  = 50
	defaultInputPath  = "proxy.txt"
	outputConfigPath  = "config.json"
	selectorTag       = "rotate"
	clashAPIAddress   = "127.0.0.1:9090"
	clashAPISecret    = "CHANGE_ME_SECRET"
)

type config struct {
	Log       logConfig        `json:"log"`
	Inbounds  []inboundConfig  `json:"inbounds"`
	Outbounds []outboundConfig `json:"outbounds"`
	Route     routeConfig      `json:"route"`
	Extra     *extraConfig     `json:"experimental,omitempty"`
}

type logConfig struct {
	Level string `json:"level"`
}

type inboundConfig struct {
	Type       string `json:"type"`
	Tag        string `json:"tag"`
	Listen     string `json:"listen"`
	ListenPort int    `json:"listen_port"`
	Users      []user `json:"users"`
}

type user struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type outboundConfig struct {
	Type      string   `json:"type"`
	Tag       string   `json:"tag"`
	Outbounds []string `json:"outbounds,omitempty"`
	URL       string   `json:"url,omitempty"`
	Interval  string   `json:"interval,omitempty"`
	IdleTime  string   `json:"idle_timeout,omitempty"`
	Tolerance int      `json:"tolerance,omitempty"`
	Interrupt *bool    `json:"interrupt_exist_connections,omitempty"`

	Server     string `json:"server,omitempty"`
	ServerPort int    `json:"server_port,omitempty"`
	Version    string `json:"version,omitempty"`
	Username   string `json:"username,omitempty"`
	Password   string `json:"password,omitempty"`
}

type routeConfig struct {
	Final string `json:"final"`
}

type extraConfig struct {
	ClashAPI clashAPIConfig `json:"clash_api"`
}

type clashAPIConfig struct {
	ExternalController string `json:"external_controller"`
	Secret             string `json:"secret"`
}

type proxy struct {
	Username string
	Password string
	Host     string
	Port     int
}

func main() {
	apiEnabled := flag.Bool("api", false, "generate config with selector and Clash API for external rotation")
	flag.Usage = func() {
		fmt.Fprintf(flag.CommandLine.Output(), "usage: %s [-api] [proxies.txt]\n", os.Args[0])
	}
	flag.Parse()

	if flag.NArg() > 1 {
		flag.Usage()
		os.Exit(2)
	}

	inputPath := defaultInputPath
	if flag.NArg() == 1 {
		inputPath = flag.Arg(0)
	}

	proxies, err := readProxies(inputPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if len(proxies) == 0 {
		fmt.Fprintln(os.Stderr, "no proxies found")
		os.Exit(1)
	}

	cfg := buildConfig(proxies, *apiEnabled)
	file, err := os.Create(outputConfigPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "create %s: %v\n", outputConfigPath, err)
		os.Exit(1)
	}
	defer file.Close()

	encoder := json.NewEncoder(file)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(cfg); err != nil {
		fmt.Fprintf(os.Stderr, "encode config: %v\n", err)
		os.Exit(1)
	}

	mode := "urltest"
	if *apiEnabled {
		mode = "selector+clash_api"
	}
	fmt.Printf("wrote %s with %d proxies in %s mode\n", outputConfigPath, len(proxies), mode)
}

func readProxies(path string) ([]proxy, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	defer file.Close()

	var proxies []proxy
	scanner := bufio.NewScanner(file)
	lineNumber := 0
	for scanner.Scan() {
		lineNumber++
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		parsed, err := parseProxy(line)
		if err != nil {
			return nil, fmt.Errorf("line %d: %w", lineNumber, err)
		}
		proxies = append(proxies, parsed)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}

	return proxies, nil
}

func parseProxy(raw string) (proxy, error) {
	parsed, err := url.Parse(raw)
	if err != nil {
		return proxy{}, fmt.Errorf("parse proxy URL: %w", err)
	}
	if parsed.Scheme != "socks5h" {
		return proxy{}, fmt.Errorf("unsupported scheme %q, expected socks5h", parsed.Scheme)
	}
	if parsed.User == nil {
		return proxy{}, fmt.Errorf("missing username and password")
	}

	username := parsed.User.Username()
	password, hasPassword := parsed.User.Password()
	if username == "" || !hasPassword {
		return proxy{}, fmt.Errorf("missing username or password")
	}

	host := parsed.Hostname()
	if host == "" {
		return proxy{}, fmt.Errorf("missing host")
	}

	portText := parsed.Port()
	if portText == "" {
		return proxy{}, fmt.Errorf("missing port")
	}
	port, err := strconv.Atoi(portText)
	if err != nil || port < 1 || port > 65535 {
		return proxy{}, fmt.Errorf("invalid port %q", portText)
	}

	return proxy{
		Username: username,
		Password: password,
		Host:     host,
		Port:     port,
	}, nil
}

func buildConfig(proxies []proxy, apiEnabled bool) config {
	outbounds := make([]outboundConfig, 0, len(proxies)+1)
	outboundTags := make([]string, 0, len(proxies))

	for i := range proxies {
		outboundTags = append(outboundTags, fmt.Sprintf("socks-%d", i+1))
	}

	finalOutbound := "auto"
	if apiEnabled {
		finalOutbound = selectorTag
		outbounds = append(outbounds, outboundConfig{
			Type:      "selector",
			Tag:       selectorTag,
			Outbounds: outboundTags,
			Interrupt: boolPtr(false),
		})
	} else {
		outbounds = append(outbounds, outboundConfig{
			Type:      "urltest",
			Tag:       "auto",
			Outbounds: outboundTags,
			URL:       defaultTestURL,
			Interval:  defaultInterval,
			IdleTime:  defaultIdleTime,
			Tolerance: defaultTolerance,
			Interrupt: boolPtr(false),
		})
	}

	for i, p := range proxies {
		outbounds = append(outbounds, outboundConfig{
			Type:       "socks",
			Tag:        fmt.Sprintf("socks-%d", i+1),
			Server:     p.Host,
			ServerPort: p.Port,
			Version:    "5",
			Username:   p.Username,
			Password:   p.Password,
		})
	}

	cfg := config{
		Log: logConfig{Level: "info"},
		Inbounds: []inboundConfig{
			{
				Type:       "http",
				Tag:        "http-in",
				Listen:     defaultListenAddr,
				ListenPort: defaultListenPort,
				Users: []user{
					{
						Username: envOrDefault("HTTP_USERNAME", "HTTP_USERNAME"),
						Password: envOrDefault("HTTP_PASSWORD", "HTTP_PASSWORD"),
					},
				},
			},
		},
		Outbounds: outbounds,
		Route:     routeConfig{Final: finalOutbound},
	}
	if apiEnabled {
		cfg.Extra = &extraConfig{
			ClashAPI: clashAPIConfig{
				ExternalController: clashAPIAddress,
				Secret:             envOrDefault("CLASH_API_SECRET", clashAPISecret),
			},
		}
	}

	return cfg
}

func envOrDefault(key, fallback string) string {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	return value
}

func boolPtr(value bool) *bool {
	return &value
}
