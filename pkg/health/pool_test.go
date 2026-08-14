package health

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestPoolRetiresRouteAfterFailedFullDownload(t *testing.T) {
	t.Parallel()

	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "bad proxy", http.StatusBadGateway)
	}))
	defer proxy.Close()

	pool, err := NewPool([]Route{{Tag: "socks-1", ProxyURL: proxy.URL}}, []string{"http://example.com/check"}, Options{
		ActiveRoutesCheckInterval: time.Hour,
		FullDownloadCheckTimeout:  time.Second,
		SuccessfulCheckReadyTTL:   time.Minute,
		MaxConcurrentChecks:       1,
		RequiredSuccessfulChecks:  1,
	})
	if err != nil {
		t.Fatal(err)
	}

	ctx := t.Context()
	pool.Start(ctx)

	snapshot := waitSnapshot(t, pool, func(snapshot Snapshot) bool {
		return snapshot.Retired == 1
	})
	if snapshot.Ready != 0 {
		t.Fatalf("expected no ready routes, got %d", snapshot.Ready)
	}
	if !snapshot.States[0].Retired || snapshot.States[0].RetiredReason == "" {
		t.Fatalf("expected retired reason in snapshot: %+v", snapshot.States[0])
	}
}

func TestPoolRetiresRouteWhenBodyExceedsLimit(t *testing.T) {
	t.Parallel()

	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("0123456789"))
	}))
	defer proxy.Close()

	pool, err := NewPool([]Route{{Tag: "socks-1", ProxyURL: proxy.URL}}, []string{"http://example.com/check"}, Options{
		ActiveRoutesCheckInterval: time.Hour,
		FullDownloadCheckTimeout:  time.Second,
		SuccessfulCheckReadyTTL:   time.Minute,
		MaxConcurrentChecks:       1,
		MaxDownloadBytes:          4,
		RequiredSuccessfulChecks:  1,
	})
	if err != nil {
		t.Fatal(err)
	}

	ctx := t.Context()
	pool.Start(ctx)

	snapshot := waitSnapshot(t, pool, func(snapshot Snapshot) bool {
		return snapshot.Retired == 1
	})
	if snapshot.States[0].RetiredReason == "" {
		t.Fatalf("expected retired reason in snapshot: %+v", snapshot.States[0])
	}
}

func TestPoolWaitsForConfiguredFailuresBeforeRetiringRoute(t *testing.T) {
	t.Parallel()

	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "bad proxy", http.StatusBadGateway)
	}))
	defer proxy.Close()

	route := Route{Tag: "socks-1", ProxyURL: proxy.URL}
	pool, err := NewPool([]Route{route}, []string{"http://example.com/check"}, Options{
		ActiveRoutesCheckInterval: time.Hour,
		FullDownloadCheckTimeout:  time.Second,
		SuccessfulCheckReadyTTL:   time.Minute,
		MaxConcurrentChecks:       1,
		RequiredSuccessfulChecks:  1,
		FailedChecksBeforeRetire:  2,
	})
	if err != nil {
		t.Fatal(err)
	}

	pool.checkOnce(route)
	snapshot := waitSnapshot(t, pool, func(snapshot Snapshot) bool {
		return snapshot.States[0].ConsecutiveFailures == 1
	})
	if snapshot.Retired != 0 || snapshot.States[0].Retired {
		t.Fatalf("expected route to stay active after first failure: %+v", snapshot.States[0])
	}

	pool.checkOnce(route)
	snapshot = waitSnapshot(t, pool, func(snapshot Snapshot) bool {
		return snapshot.Retired == 1
	})
	if snapshot.States[0].ConsecutiveFailures != 2 {
		t.Fatalf("expected two failures before retire: %+v", snapshot.States[0])
	}
}

func waitSnapshot(t *testing.T, pool *Pool, ready func(Snapshot) bool) Snapshot {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		snapshot := pool.Snapshot()
		if ready(snapshot) {
			return snapshot
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("condition was not reached; last snapshot: %+v", pool.Snapshot())
	return Snapshot{}
}
