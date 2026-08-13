package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"time"
)

const (
	defaultAPIURL     = "http://127.0.0.1:9090"
	defaultConfigPath = "config.json"
	defaultInterval   = time.Minute
	defaultSelector   = "rotate"
	defaultSecret     = "CHANGE_ME_SECRET"
	defaultTestURL    = "http://cachefly.cachefly.net/1mb.test"
	defaultTimeoutMS  = 5000
)

type config struct {
	Outbounds []outbound `json:"outbounds"`
}

type outbound struct {
	Type      string   `json:"type"`
	Tag       string   `json:"tag"`
	Outbounds []string `json:"outbounds"`
}

func main() {
	apiURL := flag.String("api", defaultAPIURL, "Clash API URL")
	configPath := flag.String("config", defaultConfigPath, "sing-box config path")
	interval := flag.Duration("interval", defaultInterval, "rotation interval")
	selector := flag.String("selector", defaultSelector, "selector outbound tag")
	testURL := flag.String("url", defaultTestURL, "proxy test URL")
	timeoutMS := flag.Int("timeout", defaultTimeoutMS, "proxy test timeout in milliseconds")
	flag.Parse()

	tags, err := readSelectorTags(*configPath, *selector)
	if err != nil {
		fatal(err)
	}
	if len(tags) == 0 {
		fatal(fmt.Errorf("selector %q has no outbounds", *selector))
	}

	secret := os.Getenv("CLASH_API_SECRET")
	if secret == "" {
		secret = defaultSecret
	}
	client := &http.Client{Timeout: time.Duration(*timeoutMS+1000) * time.Millisecond}
	rotator := rotator{
		client:    client,
		apiURL:    stringsTrimRightSlash(*apiURL),
		secret:    secret,
		selector:  *selector,
		tags:      tags,
		testURL:   *testURL,
		timeoutMS: *timeoutMS,
	}

	for {
		if err := rotator.rotateOnce(); err != nil {
			fmt.Fprintf(os.Stderr, "rotate: %v\n", err)
		}
		time.Sleep(*interval)
	}
}

type rotator struct {
	client    *http.Client
	apiURL    string
	secret    string
	selector  string
	tags      []string
	testURL   string
	timeoutMS int
	current   int
}

func (r *rotator) rotateOnce() error {
	for offset := 1; offset <= len(r.tags); offset++ {
		index := (r.current + offset) % len(r.tags)
		tag := r.tags[index]
		if err := r.check(tag); err != nil {
			fmt.Fprintf(os.Stderr, "%s check failed: %v\n", tag, err)
			continue
		}
		if err := r.selectTag(tag); err != nil {
			return err
		}
		r.current = index
		fmt.Printf("selected %s\n", tag)
		return nil
	}
	return fmt.Errorf("no working proxies found")
}

func (r *rotator) check(tag string) error {
	endpoint := fmt.Sprintf(
		"%s/proxies/%s/delay?timeout=%d&url=%s",
		r.apiURL,
		url.PathEscape(tag),
		r.timeoutMS,
		url.QueryEscape(r.testURL),
	)
	req, err := http.NewRequest(http.MethodGet, endpoint, nil)
	if err != nil {
		return err
	}
	r.authorize(req)

	resp, err := r.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("status %d: %s", resp.StatusCode, bytes.TrimSpace(body))
	}
	return nil
}

func (r *rotator) selectTag(tag string) error {
	body, err := json.Marshal(map[string]string{"name": tag})
	if err != nil {
		return err
	}
	endpoint := fmt.Sprintf("%s/proxies/%s", r.apiURL, url.PathEscape(r.selector))
	req, err := http.NewRequest(http.MethodPut, endpoint, bytes.NewReader(body))
	if err != nil {
		return err
	}
	r.authorize(req)
	req.Header.Set("Content-Type", "application/json")

	resp, err := r.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("select %s: status %d: %s", tag, resp.StatusCode, bytes.TrimSpace(body))
	}
	return nil
}

func (r *rotator) authorize(req *http.Request) {
	if r.secret != "" {
		req.Header.Set("Authorization", "Bearer "+r.secret)
	}
}

func readSelectorTags(path, selector string) ([]string, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	var cfg config
	if err := json.NewDecoder(file).Decode(&cfg); err != nil {
		return nil, err
	}
	for _, outbound := range cfg.Outbounds {
		if outbound.Type == "selector" && outbound.Tag == selector {
			return outbound.Outbounds, nil
		}
	}
	return nil, fmt.Errorf("selector %q not found in %s", selector, path)
}

func stringsTrimRightSlash(value string) string {
	for len(value) > 0 && value[len(value)-1] == '/' {
		value = value[:len(value)-1]
	}
	return value
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
