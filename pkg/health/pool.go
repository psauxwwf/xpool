package health

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"time"
)

type Route struct {
	Tag      string
	ProxyURL string
}

type Pool struct {
	routes        []Route
	checkURLs     []string
	checkInterval time.Duration
	timeout       time.Duration
	readyTTL      time.Duration
	states        map[string]State
	clients       map[string]*http.Client
	mu            chan func()
}

type State struct {
	Tag                 string
	Alive               bool
	LastSuccess         time.Time
	LastError           string
	Duration            time.Duration
	ConsecutiveFailures int
}

type Snapshot struct {
	Total     int             `json:"total"`
	Ready     int             `json:"ready"`
	ReadyTTL  string          `json:"ready_ttl"`
	CheckURLs []string        `json:"check_urls"`
	States    []StateSnapshot `json:"states"`
}

type StateSnapshot struct {
	Tag                 string `json:"tag"`
	Alive               bool   `json:"alive"`
	Ready               bool   `json:"ready"`
	LastSuccess         any    `json:"last_success"`
	LastError           string `json:"last_error,omitempty"`
	Duration            string `json:"duration,omitempty"`
	ConsecutiveFailures int    `json:"consecutive_failures"`
}

func NewPool(routes []Route, checkURLs []string, checkInterval, timeout, readyTTL time.Duration) (*Pool, error) {
	states := make(map[string]State, len(routes))
	clients := make(map[string]*http.Client, len(routes))
	for _, route := range routes {
		client, err := proxyHTTPClient(route.ProxyURL, timeout)
		if err != nil {
			return nil, err
		}
		states[route.Tag] = State{Tag: route.Tag}
		clients[route.Tag] = client
	}

	pool := &Pool{
		routes:        routes,
		checkURLs:     checkURLs,
		checkInterval: checkInterval,
		timeout:       timeout,
		readyTTL:      readyTTL,
		states:        states,
		clients:       clients,
		mu:            make(chan func()),
	}
	go pool.serialize()
	return pool, nil
}

func (p *Pool) Start(ctx context.Context) {
	for _, route := range p.routes {
		go p.checkLoop(ctx, route)
	}
}

func (p *Pool) Ready() []State {
	result := make(chan []State, 1)
	p.mu <- func() {
		now := time.Now()
		ready := make([]State, 0, len(p.states))
		for _, route := range p.routes {
			state := p.states[route.Tag]
			if state.Alive && now.Sub(state.LastSuccess) <= p.readyTTL {
				ready = append(ready, state)
			}
		}
		result <- ready
	}
	return <-result
}

func (p *Pool) Snapshot() Snapshot {
	result := make(chan Snapshot, 1)
	p.mu <- func() {
		now := time.Now()
		snapshot := Snapshot{
			Total:     len(p.states),
			ReadyTTL:  p.readyTTL.String(),
			CheckURLs: append([]string(nil), p.checkURLs...),
			States:    make([]StateSnapshot, 0, len(p.routes)),
		}
		for _, route := range p.routes {
			state := p.states[route.Tag]
			ready := state.Alive && now.Sub(state.LastSuccess) <= p.readyTTL
			if ready {
				snapshot.Ready++
			}
			lastSuccess := any(nil)
			if !state.LastSuccess.IsZero() {
				lastSuccess = state.LastSuccess.UTC().Format(time.RFC3339)
			}
			duration := ""
			if state.Duration > 0 {
				duration = state.Duration.String()
			}
			snapshot.States = append(snapshot.States, StateSnapshot{
				Tag:                 state.Tag,
				Alive:               state.Alive,
				Ready:               ready,
				LastSuccess:         lastSuccess,
				LastError:           state.LastError,
				Duration:            duration,
				ConsecutiveFailures: state.ConsecutiveFailures,
			})
		}
		result <- snapshot
	}
	return <-result
}

func (p *Pool) IsReady(tag string) bool {
	ready := p.Ready()
	for _, state := range ready {
		if state.Tag == tag {
			return true
		}
	}
	return false
}

func StateTags(states []State) []string {
	tags := make([]string, len(states))
	for i, state := range states {
		tags[i] = state.Tag
	}
	return tags
}

func (p *Pool) serialize() {
	for fn := range p.mu {
		fn()
	}
}

func (p *Pool) checkLoop(ctx context.Context, route Route) {
	for {
		p.checkOnce(route)

		timer := time.NewTimer(p.checkInterval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
	}
}

func (p *Pool) checkOnce(route Route) {
	start := time.Now()
	err := p.check(route.Tag)
	duration := time.Since(start)
	p.mu <- func() {
		state := p.states[route.Tag]
		if err != nil {
			state.Alive = false
			state.LastError = err.Error()
			state.ConsecutiveFailures++
			p.states[route.Tag] = state
			slog.Warn("background proxy check failed", "tag", route.Tag, "proxy", route.ProxyURL, "error", err)
			return
		}

		state.Alive = true
		state.LastSuccess = time.Now()
		state.LastError = ""
		state.Duration = duration
		state.ConsecutiveFailures = 0
		p.states[route.Tag] = state
		slog.Info("background proxy ready", "tag", route.Tag, "proxy", route.ProxyURL, "duration", duration)
	}
}

func (p *Pool) check(tag string) error {
	client := p.clients[tag]
	for _, rawURL := range p.checkURLs {
		if err := checkURL(client, tag, rawURL); err != nil {
			return err
		}
	}
	return nil
}

func checkURL(client *http.Client, tag, rawURL string) error {
	req, err := http.NewRequest(http.MethodGet, rawURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "xpool-health-checker/1.0")

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("%s through %s: %w", rawURL, tag, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("%s through %s: status %d: %s", rawURL, tag, resp.StatusCode, bytes.TrimSpace(body))
	}
	bytesRead, err := io.Copy(io.Discard, resp.Body)
	if err != nil {
		return fmt.Errorf("download %s through %s: %w", rawURL, tag, err)
	}
	slog.Debug("background check downloaded URL", "tag", tag, "url", rawURL, "status", resp.StatusCode, "bytes", bytesRead)
	return nil
}

func proxyHTTPClient(proxyRaw string, timeout time.Duration) (*http.Client, error) {
	proxyURL, err := url.Parse(proxyRaw)
	if err != nil {
		return nil, fmt.Errorf("parse check proxy URL %q: %w", proxyRaw, err)
	}
	if proxyURL.Scheme == "" || proxyURL.Host == "" {
		return nil, fmt.Errorf("invalid check proxy URL %q", proxyRaw)
	}

	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = http.ProxyURL(proxyURL)

	return &http.Client{Timeout: timeout, Transport: transport}, nil
}
