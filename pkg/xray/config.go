package xray

type Config struct {
	Log              LogConfig              `json:"log"`
	API              APIConfig              `json:"api"`
	Stats            map[string]any         `json:"stats"`
	Policy           PolicyConfig           `json:"policy"`
	Inbounds         []InboundConfig        `json:"inbounds"`
	Outbounds        []OutboundConfig       `json:"outbounds"`
	Routing          RoutingConfig          `json:"routing"`
	BurstObservatory BurstObservatoryConfig `json:"burstObservatory"`
}

type LogConfig struct {
	LogLevel string `json:"loglevel"`
}

type APIConfig struct {
	Tag      string   `json:"tag"`
	Services []string `json:"services"`
}

type PolicyConfig struct {
	Levels map[string]PolicyLevel `json:"levels"`
	System PolicySystem           `json:"system"`
}

type PolicyLevel struct {
	StatsUserUplink   bool `json:"statsUserUplink"`
	StatsUserDownlink bool `json:"statsUserDownlink"`
}

type PolicySystem struct {
	StatsInboundUplink    bool `json:"statsInboundUplink"`
	StatsInboundDownlink  bool `json:"statsInboundDownlink"`
	StatsOutboundUplink   bool `json:"statsOutboundUplink"`
	StatsOutboundDownlink bool `json:"statsOutboundDownlink"`
}

type InboundConfig struct {
	Tag      string `json:"tag"`
	Listen   string `json:"listen"`
	Port     int    `json:"port"`
	Protocol string `json:"protocol"`
	Settings any    `json:"settings"`
}

type HTTPInboundSettings struct {
	Accounts []HTTPAccount `json:"accounts,omitempty"`
}

type HTTPAccount struct {
	User string `json:"user"`
	Pass string `json:"pass"`
}

type DokodemoDoorSettings struct {
	Address string `json:"address"`
}

type OutboundConfig struct {
	Tag      string `json:"tag"`
	Protocol string `json:"protocol"`
	Settings any    `json:"settings,omitempty"`
}

type SocksOutboundSettings struct {
	Address string `json:"address"`
	Port    int    `json:"port"`
	User    string `json:"user"`
	Pass    string `json:"pass"`
}

type RoutingConfig struct {
	Rules     []RoutingRule   `json:"rules"`
	Balancers []BalancingRule `json:"balancers"`
}

type RoutingRule struct {
	Type        string   `json:"type"`
	InboundTag  []string `json:"inboundTag,omitempty"`
	OutboundTag string   `json:"outboundTag,omitempty"`
	BalancerTag string   `json:"balancerTag,omitempty"`
}

type BalancingRule struct {
	Tag         string         `json:"tag"`
	Selector    []string       `json:"selector"`
	Strategy    StrategyConfig `json:"strategy"`
	FallbackTag string         `json:"fallbackTag,omitempty"`
}

type StrategyConfig struct {
	Type string `json:"type"`
}

type BurstObservatoryConfig struct {
	SubjectSelector []string   `json:"subjectSelector"`
	PingConfig      PingConfig `json:"pingConfig"`
}

type PingConfig struct {
	Destination string `json:"destination"`
	Interval    string `json:"interval"`
	Sampling    int    `json:"sampling"`
	Timeout     string `json:"timeout"`
	HTTPMethod  string `json:"httpMethod"`
}
