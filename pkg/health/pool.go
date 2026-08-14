package health

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"sync"
	"time"
)

type Route struct {
	Tag      string
	ProxyURL string
}

type Pool struct {
	routes         []Route
	checkURLs      []string
	checkInterval  time.Duration
	timeout        time.Duration
	readyTTL       time.Duration
	concurrency    int
	jitter         time.Duration
	maxBytes       int64
	readySuccesses int
	states         map[string]State
	clients        map[string]*http.Client
	mu             chan func()
}

type State struct {
	Tag                  string
	Alive                bool
	LastSuccess          time.Time
	LastError            string
	Duration             time.Duration
	ConsecutiveFailures  int
	ConsecutiveSuccesses int
	Retired              bool
	RetiredAt            time.Time
	RetiredReason        string
}

type Snapshot struct {
	Total            int             `json:"total"`
	Ready            int             `json:"ready"`
	Retired          int             `json:"retired"`
	ReadyTTL         string          `json:"ready_ttl"`
	CheckURLs        []string        `json:"check_urls"`
	CheckConcurrency int             `json:"check_concurrency"`
	CheckJitter      string          `json:"check_jitter"`
	CheckMaxBytes    int64           `json:"check_max_bytes"`
	ReadySuccesses   int             `json:"ready_successes"`
	States           []StateSnapshot `json:"states"`
}

type StateSnapshot struct {
	Tag                  string `json:"tag"`
	Alive                bool   `json:"alive"`
	Ready                bool   `json:"ready"`
	LastSuccess          any    `json:"last_success"`
	LastError            string `json:"last_error,omitempty"`
	Duration             string `json:"duration,omitempty"`
	ConsecutiveFailures  int    `json:"consecutive_failures"`
	ConsecutiveSuccesses int    `json:"consecutive_successes"`
	Retired              bool   `json:"retired"`
	RetiredAt            any    `json:"retired_at"`
	RetiredReason        string `json:"retired_reason,omitempty"`
}

type Options struct {
	CheckInterval  time.Duration
	Timeout        time.Duration
	ReadyTTL       time.Duration
	Concurrency    int
	Jitter         time.Duration
	MaxBytes       int64
	ReadySuccesses int
}

func NewPool(routes []Route, checkURLs []string, options Options) (*Pool, error) {
	if options.Concurrency <= 0 || options.Concurrency > len(routes) {
		options.Concurrency = len(routes)
	}
	if options.ReadySuccesses <= 0 {
		options.ReadySuccesses = 1
	}

	states := make(map[string]State, len(routes))
	clients := make(map[string]*http.Client, len(routes))
	for _, route := range routes {
		client, err := proxyHTTPClient(route.ProxyURL, options.Timeout)
		if err != nil {
			return nil, err
		}
		states[route.Tag] = State{Tag: route.Tag}
		clients[route.Tag] = client
	}

	pool := &Pool{
		routes:         routes,
		checkURLs:      checkURLs,
		checkInterval:  options.CheckInterval,
		timeout:        options.Timeout,
		readyTTL:       options.ReadyTTL,
		concurrency:    options.Concurrency,
		jitter:         options.Jitter,
		maxBytes:       options.MaxBytes,
		readySuccesses: options.ReadySuccesses,
		states:         states,
		clients:        clients,
		mu:             make(chan func()),
	}
	go pool.serialize()
	return pool, nil
}

func (p *Pool) Start(ctx context.Context) {
	go p.checkScheduler(ctx)
}

func (p *Pool) Ready() []State {
	result := make(chan []State, 1)
	p.mu <- func() {
		now := time.Now()
		ready := make([]State, 0, len(p.states))
		for _, route := range p.routes {
			state := p.states[route.Tag]
			if p.isReadyState(state, now) {
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
			Total:            len(p.states),
			ReadyTTL:         p.readyTTL.String(),
			CheckURLs:        append([]string(nil), p.checkURLs...),
			CheckConcurrency: p.concurrency,
			CheckJitter:      p.jitter.String(),
			CheckMaxBytes:    p.maxBytes,
			ReadySuccesses:   p.readySuccesses,
			States:           make([]StateSnapshot, 0, len(p.routes)),
		}
		for _, route := range p.routes {
			state := p.states[route.Tag]
			ready := p.isReadyState(state, now)
			if ready {
				snapshot.Ready++
			}
			if state.Retired {
				snapshot.Retired++
			}
			lastSuccess := any(nil)
			if !state.LastSuccess.IsZero() {
				lastSuccess = state.LastSuccess.UTC().Format(time.RFC3339)
			}
			retiredAt := any(nil)
			if !state.RetiredAt.IsZero() {
				retiredAt = state.RetiredAt.UTC().Format(time.RFC3339)
			}
			duration := ""
			if state.Duration > 0 {
				duration = state.Duration.String()
			}
			snapshot.States = append(snapshot.States, StateSnapshot{
				Tag:                  state.Tag,
				Alive:                state.Alive,
				Ready:                ready,
				LastSuccess:          lastSuccess,
				LastError:            state.LastError,
				Duration:             duration,
				ConsecutiveFailures:  state.ConsecutiveFailures,
				ConsecutiveSuccesses: state.ConsecutiveSuccesses,
				Retired:              state.Retired,
				RetiredAt:            retiredAt,
				RetiredReason:        state.RetiredReason,
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

func (p *Pool) checkScheduler(ctx context.Context) {
	for {
		p.checkBatch(ctx)

		timer := time.NewTimer(p.checkInterval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
	}
}

func (p *Pool) checkBatch(ctx context.Context) {
	routes := p.activeRoutes()
	if len(routes) == 0 {
		return
	}

	jobs := make(chan Route)
	var workers sync.WaitGroup
	workerCount := min(p.concurrency, len(routes))
	for i := 0; i < workerCount; i++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for route := range jobs {
				if p.jitter > 0 {
					timer := time.NewTimer(jitterDelay(route.Tag, p.jitter))
					select {
					case <-ctx.Done():
						timer.Stop()
						return
					case <-timer.C:
					}
				}
				p.checkOnce(route)
			}
		}()
	}

	for _, route := range routes {
		select {
		case <-ctx.Done():
			close(jobs)
			workers.Wait()
			return
		case jobs <- route:
		}
	}
	close(jobs)
	workers.Wait()
}

func (p *Pool) activeRoutes() []Route {
	result := make(chan []Route, 1)
	p.mu <- func() {
		routes := make([]Route, 0, len(p.routes))
		for _, route := range p.routes {
			if !p.states[route.Tag].Retired {
				routes = append(routes, route)
			}
		}
		result <- routes
	}
	return <-result
}

func (p *Pool) checkOnce(route Route) {
	start := time.Now()
	err := p.check(route.Tag)
	duration := time.Since(start)
	p.mu <- func() {
		state := p.states[route.Tag]
		if state.Retired {
			return
		}
		if err != nil {
			state.Alive = false
			state.LastError = err.Error()
			state.ConsecutiveFailures++
			state.Retired = true
			state.RetiredAt = time.Now()
			state.RetiredReason = err.Error()
			p.states[route.Tag] = state
			delete(p.clients, route.Tag)
			slog.Warn("retired proxy after failed full-download check", "tag", route.Tag, "proxy", route.ProxyURL, "error", err)
			return
		}

		state.Alive = true
		state.LastSuccess = time.Now()
		state.LastError = ""
		state.Duration = duration
		state.ConsecutiveFailures = 0
		state.ConsecutiveSuccesses++
		p.states[route.Tag] = state
		slog.Info("background proxy ready", "tag", route.Tag, "proxy", route.ProxyURL, "duration", duration)
	}
}

func (p *Pool) check(tag string) error {
	client := p.clients[tag]
	for _, rawURL := range p.checkURLs {
		if err := checkURL(client, tag, rawURL, p.maxBytes); err != nil {
			return err
		}
	}
	return nil
}

func checkURL(client *http.Client, tag, rawURL string, maxBytes int64) error {
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
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("%s through %s: status %d: %s", rawURL, tag, resp.StatusCode, bytes.TrimSpace(body))
	}
	reader := resp.Body
	if maxBytes > 0 {
		reader = io.NopCloser(io.LimitReader(resp.Body, maxBytes+1))
	}
	bytesRead, err := io.Copy(io.Discard, reader)
	if err != nil {
		return fmt.Errorf("download %s through %s: %w", rawURL, tag, err)
	}
	if maxBytes > 0 && bytesRead > maxBytes {
		return fmt.Errorf("download %s through %s exceeded max body size %d", rawURL, tag, maxBytes)
	}
	slog.Debug("background check downloaded URL", "tag", tag, "url", rawURL, "status", resp.StatusCode, "bytes", bytesRead)
	return nil
}

func (p *Pool) isReadyState(state State, now time.Time) bool {
	return !state.Retired && state.Alive && state.ConsecutiveSuccesses >= p.readySuccesses && now.Sub(state.LastSuccess) <= p.readyTTL
}

func jitterDelay(key string, max time.Duration) time.Duration {
	if max <= 0 {
		return 0
	}
	var sum uint64
	for i := 0; i < len(key); i++ {
		sum = sum*131 + uint64(key[i])
	}
	return time.Duration(sum % uint64(max))
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
