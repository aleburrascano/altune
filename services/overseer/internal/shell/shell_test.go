package shell_test

import (
	"altune/overseer/internal/core"
	"altune/overseer/internal/shell"
	"bufio"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
)

func TestBucketsAPIReturnsSortedSnapshots(t *testing.T) {
	reg := core.NewRegistry()
	reg.Register(stubBucket{id: "usage", state: core.StateLive})
	reg.Register(stubBucket{id: "liveactivity", state: core.StateLive})
	srv := newServer(reg)

	rec := do(srv, withOwner(httptest.NewRequest(http.MethodGet, "/api/buckets", nil)))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "application/json") {
		t.Errorf("content-type = %q, want json", ct)
	}
	var resp struct {
		Buckets []core.Snapshot `json:"buckets"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(resp.Buckets) != 2 {
		t.Fatalf("buckets = %d, want 2", len(resp.Buckets))
	}
	if resp.Buckets[0].ID != "liveactivity" || resp.Buckets[1].ID != "usage" {
		t.Errorf("order = %s,%s, want liveactivity,usage", resp.Buckets[0].ID, resp.Buckets[1].ID)
	}
}

func TestSourceDownBucketStillServedAndShellUp(t *testing.T) {
	reg := fixedRegistry{buckets: []core.Bucket{stubBucket{id: "reliability", state: core.StateSourceDown}}}
	srv := newServer(reg)

	rec := do(srv, withOwner(httptest.NewRequest(http.MethodGet, "/api/buckets", nil)))
	var resp struct {
		Buckets []core.Snapshot `json:"buckets"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if len(resp.Buckets) != 1 || resp.Buckets[0].State != core.StateSourceDown {
		t.Fatalf("want a single source_down snapshot, got %+v", resp.Buckets)
	}
	if h := do(srv, httptest.NewRequest(http.MethodGet, "/health", nil)); h.Code != http.StatusOK {
		t.Errorf("/health = %d, want 200 while a source is down", h.Code)
	}
	if s := do(srv, httptest.NewRequest(http.MethodGet, "/", nil)); s.Code != http.StatusOK {
		t.Errorf("SPA / = %d, want 200 while a source is down", s.Code)
	}
}

func TestPanickingBucketContained(t *testing.T) {
	reg := fixedRegistry{buckets: []core.Bucket{panicBucket{}, stubBucket{id: "ok", state: core.StateLive}}}
	srv := newServer(reg)

	rec := do(srv, withOwner(httptest.NewRequest(http.MethodGet, "/api/buckets", nil)))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 despite a panicking bucket", rec.Code)
	}
	var resp struct {
		Buckets []core.Snapshot `json:"buckets"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	byID := map[string]core.Snapshot{}
	for _, s := range resp.Buckets {
		byID[s.ID] = s
	}
	if byID["boom"].State != core.StateSourceDown {
		t.Errorf("panicking bucket state = %q, want source_down (contained)", byID["boom"].State)
	}
	if byID["ok"].State != core.StateLive {
		t.Errorf("healthy sibling state = %q, want live", byID["ok"].State)
	}
}

func TestStreamIsGuarded(t *testing.T) {
	srv := newServer(fixedRegistry{buckets: []core.Bucket{stubBucket{id: "a", state: core.StateLive}}})

	if rec := do(srv, httptest.NewRequest(http.MethodGet, "/api/stream", nil)); rec.Code != http.StatusUnauthorized {
		t.Errorf("unauth /api/stream = %d, want 401", rec.Code)
	}
	req := httptest.NewRequest(http.MethodGet, "/api/stream", nil)
	req.Header.Set("Authorization", "Bearer "+nonOwnerToken)
	if rec := do(srv, req); rec.Code != http.StatusForbidden {
		t.Errorf("non-owner /api/stream = %d, want 403", rec.Code)
	}
}

func TestStreamEmitsFrames(t *testing.T) {
	srv := newServer(fixedRegistry{buckets: []core.Bucket{stubBucket{id: "liveactivity", state: core.StateLive}}})

	ctx, cancel := context.WithCancel(context.Background())
	req := withOwner(httptest.NewRequest(http.MethodGet, "/api/stream", nil)).WithContext(ctx)
	rec := httptest.NewRecorder()

	done := make(chan struct{})
	go func() {
		srv.ServeHTTP(rec, req)
		close(done)
	}()
	time.Sleep(30 * time.Millisecond)
	cancel()
	<-done

	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "text/event-stream") {
		t.Errorf("content-type = %q, want text/event-stream", ct)
	}
	sc := bufio.NewScanner(strings.NewReader(rec.Body.String()))
	sawData := false
	for sc.Scan() {
		if strings.HasPrefix(sc.Text(), "data: ") {
			sawData = true
			var snap core.Snapshot
			if err := json.Unmarshal([]byte(strings.TrimPrefix(sc.Text(), "data: ")), &snap); err != nil {
				t.Fatalf("frame is not a Snapshot JSON: %v", err)
			}
			if snap.ID != "liveactivity" {
				t.Errorf("frame id = %q, want liveactivity", snap.ID)
			}
		}
	}
	if !sawData {
		t.Errorf("no SSE data frame emitted:\n%s", rec.Body.String())
	}
}

func TestOpenRoutes(t *testing.T) {
	srv := newServer(fixedRegistry{})

	if rec := do(srv, httptest.NewRequest(http.MethodGet, "/health", nil)); rec.Code != http.StatusOK {
		t.Errorf("/health = %d, want 200", rec.Code)
	}
	if rec := do(srv, httptest.NewRequest(http.MethodGet, "/", nil)); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "Overseer") {
		t.Errorf("/ = %d body=%q, want 200 SPA index", rec.Code, rec.Body.String())
	}
	if rec := do(srv, httptest.NewRequest(http.MethodGet, "/assets/app.js", nil)); rec.Code != http.StatusOK {
		t.Errorf("/assets/app.js = %d, want 200", rec.Code)
	}
	cfg := do(srv, httptest.NewRequest(http.MethodGet, "/config.json", nil))
	if cfg.Code != http.StatusOK {
		t.Fatalf("/config.json = %d, want 200", cfg.Code)
	}
	var c struct {
		SupabaseURL     string `json:"supabaseUrl"`
		SupabaseAnonKey string `json:"supabaseAnonKey"`
	}
	if err := json.Unmarshal(cfg.Body.Bytes(), &c); err != nil {
		t.Fatalf("config unmarshal: %v", err)
	}
	if c.SupabaseURL != "https://x.supabase.co" || c.SupabaseAnonKey != "anon-key" {
		t.Errorf("config = %+v, want the public Supabase client values", c)
	}
}

func TestOnlyGetRoutes(t *testing.T) {
	srv := newServer(fixedRegistry{})
	routes, ok := srv.(chi.Routes)
	if !ok {
		t.Fatal("router is not a chi.Routes; cannot walk it")
	}
	err := chi.Walk(routes, func(method, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		if method != http.MethodGet {
			t.Errorf("route %s %s is not a GET; observe-only forbids a mutating route", method, route)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk routes: %v", err)
	}
}

func TestHealthIsNon200WhenTheCollectLoopStalled(t *testing.T) {
	lastCycle := time.Date(2026, 9, 18, 10, 0, 0, 0, time.UTC)
	srv := newServer(fixedRegistry{}, fixedCollectStatus(shell.CollectStatus{
		Healthy:   false,
		LastCycle: lastCycle,
		OK:        2,
		Failed:    1,
	}))

	rec := do(srv, httptest.NewRequest(http.MethodGet, "/health", nil))

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("/health = %d with a stalled collect loop, want 503", rec.Code)
	}
	var body struct {
		Status        string `json:"status"`
		LastCycle     string `json:"last_cycle"`
		BucketsOK     int    `json:"buckets_ok"`
		BucketsFailed int    `json:"buckets_failed"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("health unmarshal: %v", err)
	}
	if body.Status == "ok" {
		t.Errorf("health status = %q, want a stalled status", body.Status)
	}
	if body.LastCycle != lastCycle.Format(time.RFC3339) || body.BucketsOK != 2 || body.BucketsFailed != 1 {
		t.Errorf("health body = %+v, want the loop's last cycle and counts for diagnosis", body)
	}
}

func TestHealthStaysOKWhenEveryBucketFailedButTheLoopRan(t *testing.T) {
	srv := newServer(fixedRegistry{}, fixedCollectStatus(shell.CollectStatus{
		Healthy:   true,
		LastCycle: time.Now(),
		OK:        0,
		Failed:    3,
	}))

	rec := do(srv, httptest.NewRequest(http.MethodGet, "/health", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("/health = %d with a live loop and every source down, want 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), `"status":"ok"`) {
		t.Errorf("health body = %s, want status ok", rec.Body.String())
	}
}

func TestSPAFallback(t *testing.T) {
	srv := newServer(fixedRegistry{})
	rec := do(srv, httptest.NewRequest(http.MethodGet, "/some/deep/link", nil))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "Overseer") {
		t.Errorf("deep link = %d, want 200 SPA index fallback", rec.Code)
	}
}
