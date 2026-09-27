package chromaprint

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"golang.org/x/time/rate"

	"altune/go-api/internal/acquisition/ports"
)

func TestBestMatch_PicksHighestScoreAboveThreshold(t *testing.T) {
	var parsed lookupResponse
	parsed.Status = "ok"
	parsed.Results = []struct {
		ID         string  `json:"id"`
		Score      float64 `json:"score"`
		Recordings []struct {
			ID string `json:"id"`
		} `json:"recordings"`
	}{
		{ID: "weak", Score: 0.9, Recordings: []struct {
			ID string `json:"id"`
		}{{ID: "mbid-a"}}},
		{ID: "strong", Score: 0.98, Recordings: []struct {
			ID string `json:"id"`
		}{{ID: "mbid-b"}}},
	}

	got := bestMatch(parsed)

	if got.AcoustID != "strong" {
		t.Errorf("AcoustID = %q, want strong", got.AcoustID)
	}
	if !got.Matches("mbid-b") {
		t.Errorf("MBIDs = %v, want mbid-b", got.MBIDs)
	}
}

func TestBestMatch_IgnoresLowScores(t *testing.T) {
	var parsed lookupResponse
	parsed.Status = "ok"
	parsed.Results = []struct {
		ID         string  `json:"id"`
		Score      float64 `json:"score"`
		Recordings []struct {
			ID string `json:"id"`
		} `json:"recordings"`
	}{
		{ID: "weak", Score: 0.3, Recordings: []struct {
			ID string `json:"id"`
		}{{ID: "mbid-a"}}},
	}

	if got := bestMatch(parsed); got.Known() {
		t.Errorf("a below-threshold result must not count as known, got %+v", got)
	}
}

func TestBestMatch_KeepsResultsWithOnlyAnAcoustID(t *testing.T) {
	var parsed lookupResponse
	parsed.Status = "ok"
	parsed.Results = []struct {
		ID         string  `json:"id"`
		Score      float64 `json:"score"`
		Recordings []struct {
			ID string `json:"id"`
		} `json:"recordings"`
	}{
		{ID: "acoustid-only", Score: 0.99},
	}

	got := bestMatch(parsed)
	if !got.Known() {
		t.Fatalf("an AcoustID with no MusicBrainz links still identifies the audio for cluster comparison, got %+v", got)
	}
	if !got.InCluster([]string{"other", "acoustid-only"}) {
		t.Errorf("InCluster must match on the AcoustID, got %+v", got)
	}
	if got.InCluster([]string{"other"}) {
		t.Error("an AcoustID outside the expected cluster is a different recording")
	}
}

func TestRecordingMatch_EmptyAcoustIDNeverMatchesACluster(t *testing.T) {
	if (ports.RecordingMatch{MBIDs: []string{"mb"}}).InCluster([]string{"", "a"}) {
		t.Error("a match with no AcoustID must never be read as being in the cluster")
	}
}

func TestAcoustIDsFor_SendsAPIKeyInPostFormNotURL(t *testing.T) {
	const apiKey = "secret-api-key"
	const mbid = "mbid-123"

	var gotURL string
	var gotMethod string
	var gotForm url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotURL = r.URL.String()
		gotMethod = r.Method
		if err := r.ParseForm(); err != nil {
			t.Errorf("parse form: %v", err)
		}
		gotForm = r.PostForm
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok","tracks":[{"id":"track-1"}]}`))
	}))
	defer srv.Close()

	id := NewIdentifier("", apiKey).WithClusterEndpoint(srv.URL)

	ids, err := id.AcoustIDsFor(context.Background(), mbid)
	if err != nil {
		t.Fatalf("AcoustIDsFor: %v", err)
	}
	if len(ids) != 1 || ids[0] != "track-1" {
		t.Errorf("ids = %v, want [track-1]", ids)
	}

	if gotMethod != http.MethodPost {
		t.Errorf("method = %q, want POST", gotMethod)
	}
	if strings.Contains(gotURL, apiKey) {
		t.Errorf("API key leaked into request URL %q", gotURL)
	}
	if strings.Contains(gotURL, mbid) {
		t.Errorf("mbid leaked into request URL %q", gotURL)
	}
	if got := gotForm.Get("client"); got != apiKey {
		t.Errorf("form client = %q, want %q", got, apiKey)
	}
	if got := gotForm.Get("mbid"); got != mbid {
		t.Errorf("form mbid = %q, want %q", got, mbid)
	}
}

func TestNewIdentifier_DefaultsToAcoustIDEndpoint(t *testing.T) {
	id := NewIdentifier("", "key")
	if id.endpoint != defaultEndpoint {
		t.Errorf("endpoint = %q, want %q", id.endpoint, defaultEndpoint)
	}
	if id.client == nil {
		t.Error("expected an http client")
	}
}

// warnCapture records slog records at Warn or above so a test can assert that a
// misconfiguration announced itself at startup rather than staying silent.
type warnCapture struct {
	warned bool
	msgs   []string
}

func (h *warnCapture) Enabled(_ context.Context, level slog.Level) bool {
	return level >= slog.LevelWarn
}

func (h *warnCapture) Handle(_ context.Context, r slog.Record) error {
	if r.Level >= slog.LevelWarn {
		h.warned = true
		h.msgs = append(h.msgs, r.Message)
	}
	return nil
}

func (h *warnCapture) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h *warnCapture) WithGroup(string) slog.Handler      { return h }

func captureWarnings(t *testing.T) *warnCapture {
	t.Helper()
	h := &warnCapture{}
	prev := slog.Default()
	slog.SetDefault(slog.New(h))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return h
}

// A misconfigured (empty) key must fail loudly at startup — the constructor
// logs a warning — instead of degrading silently until the first lookup.
func TestNewIdentifier_EmptyKeyWarnsAtStartup(t *testing.T) {
	logs := captureWarnings(t)

	NewIdentifier("", "")

	if !logs.warned {
		t.Fatal("an empty AcoustID API key must warn at startup, got no warning")
	}
}

// A whitespace-only key is just as misconfigured as an empty one and must not
// masquerade as a usable secret.
func TestNewIdentifier_WhitespaceOnlyKeyWarnsAndDisables(t *testing.T) {
	logs := captureWarnings(t)

	id := NewIdentifier("", "   \t\n ")

	if !logs.warned {
		t.Fatal("a whitespace-only AcoustID API key must warn at startup, got no warning")
	}
	if id.Available() {
		t.Error("a whitespace-only key must not report the identifier as available")
	}
}

// A genuine configuration stays quiet: a real key produces no startup warning.
func TestNewIdentifier_ValidKeyDoesNotWarn(t *testing.T) {
	logs := captureWarnings(t)

	NewIdentifier("", "real-key")

	if logs.warned {
		t.Errorf("a valid API key must not warn, got %v", logs.msgs)
	}
}

// At first use a missing key must return ErrMissingAPIKey, not a silent empty
// match, so a misconfig stays distinguishable from a genuine no-match.
func TestLookup_EmptyKeyReturnsError(t *testing.T) {
	id := NewIdentifier("", "")

	_, err := id.lookup(context.Background(), fingerprint{Duration: 100, Fingerprint: "abc"})

	if !errors.Is(err, ErrMissingAPIKey) {
		t.Fatalf("lookup with empty key err = %v, want ErrMissingAPIKey", err)
	}
}

func TestAcoustIDsFor_EmptyKeyReturnsError(t *testing.T) {
	id := NewIdentifier("", "")

	_, err := id.AcoustIDsFor(context.Background(), "mbid-1")

	if !errors.Is(err, ErrMissingAPIKey) {
		t.Fatalf("AcoustIDsFor with empty key err = %v, want ErrMissingAPIKey", err)
	}
}

// The regression's flip side: a valid key with no results is a genuine no-match
// — an empty slice and a nil error — and must stay distinct from a misconfig.
func TestAcoustIDsFor_ValidKeyNoResultsIsNilError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok","tracks":[]}`))
	}))
	defer srv.Close()

	id := NewIdentifier("", "real-key").WithClusterEndpoint(srv.URL)

	ids, err := id.AcoustIDsFor(context.Background(), "mbid-1")
	if err != nil {
		t.Fatalf("a genuine no-match must not error, got %v", err)
	}
	if len(ids) != 0 {
		t.Errorf("ids = %v, want empty", ids)
	}
}

func TestLookup_OversizedResponseBodyIsRejected(t *testing.T) {
	oversized := `{"status":"ok","results":[{"id":"` + strings.Repeat("x", lookupBodyCap+1024) + `"}]}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(oversized))
	}))
	defer srv.Close()

	id := NewIdentifier("", "real-key").WithEndpoint(srv.URL)

	_, err := id.lookup(context.Background(), fingerprint{Duration: 100, Fingerprint: "abc"})

	if err == nil {
		t.Fatal("lookup against an oversized response body must error, got nil")
	}
}

func fakeFpcalcDir(t *testing.T, duration float64) string {
	t.Helper()
	dir := t.TempDir()
	script := fmt.Sprintf("#!/bin/sh\nprintf '{\"duration\": %g, \"fingerprint\": \"AQAAdkmVJUqSpNce\"}'\n", duration)
	path := filepath.Join(dir, "fpcalc")
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake fpcalc: %v", err)
	}
	return dir
}

func TestIdentify_SendsCandidateDurationNotFpcalcsMeasuredLength(t *testing.T) {
	var gotForm url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Fatalf("parse form: %v", err)
		}
		gotForm = r.PostForm
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok","results":[]}`))
	}))
	defer srv.Close()

	id := NewIdentifier(fakeFpcalcDir(t, 130), "real-key").WithEndpoint(srv.URL)

	if _, err := id.Identify(context.Background(), "/tmp/preview.mp3", 236); err != nil {
		t.Fatalf("Identify: %v", err)
	}

	if got := gotForm.Get("duration"); got != "236" {
		t.Errorf("duration = %q, want 236 (the candidate's full length)", got)
	}
	if got := gotForm.Get("meta"); got != "recordings releasegroups" {
		t.Errorf("meta = %q, want %q", got, "recordings releasegroups")
	}
}

func TestIdentify_ReturnsEveryResultAboveTheScoreFloorWithLinkedRecordings(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok","results":[
			{"id":"ac-weak","score":0.5,"recordings":[{"id":"mb-weak","title":"Below Floor","duration":300}]},
			{"id":"ac-top","score":0.98,"recordings":[{"id":"mb-top","title":"Song One","duration":236,"artists":[{"name":"Artist A"}]}]},
			{"id":"ac-second","score":0.9,"recordings":[{"id":"mb-second","title":"Song Two","duration":220,"artists":[{"name":"Artist B"}]}]}
		]}`))
	}))
	defer srv.Close()

	id := NewIdentifier(fakeFpcalcDir(t, 236), "real-key").WithEndpoint(srv.URL)

	match, err := id.Identify(context.Background(), "/tmp/full.mp3", 0)
	if err != nil {
		t.Fatalf("Identify: %v", err)
	}

	if match.AcoustID != "ac-top" || match.Score != 0.98 || !match.Matches("mb-top") {
		t.Fatalf("top match = %+v, want the ac-top result as before", match)
	}
	if len(match.Results) != 2 {
		t.Fatalf("Results = %+v, want 2 results at or above the score floor", match.Results)
	}
	if match.Results[0].ID != "ac-top" || match.Results[1].ID != "ac-second" {
		t.Errorf("Results order = [%s %s], want [ac-top ac-second]", match.Results[0].ID, match.Results[1].ID)
	}
	got := match.Results[0].Recordings[0]
	want := ports.LinkedRecording{MBID: "mb-top", Title: "Song One", Artists: []string{"Artist A"}, Duration: 236}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("linked recording = %+v, want %+v", got, want)
	}
}

func TestIdentifier_DefaultsToThreeRequestsPerSecond(t *testing.T) {
	id := NewIdentifier("", "key")
	if id.limiter.Limit() != rate.Limit(3) {
		t.Errorf("limiter limit = %v, want %v", id.limiter.Limit(), rate.Limit(3))
	}
}

func TestLookup_ContextDeadlineAbortsTheRateLimitWaitBeforeAnyRequest(t *testing.T) {
	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits++
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok","results":[]}`))
	}))
	defer srv.Close()

	limiter := rate.NewLimiter(rate.Every(time.Hour), 1)
	limiter.Allow()
	id := NewIdentifier("", "real-key").WithEndpoint(srv.URL)
	id.limiter = limiter

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	_, err := id.lookup(ctx, fingerprint{Duration: 100, Fingerprint: "abc"})

	if err == nil {
		t.Fatal("lookup blocked on an exhausted limiter must fail, got nil error")
	}
	if hits != 0 {
		t.Errorf("hits = %d, want 0: the rate limit wait must abort before any request reaches the network", hits)
	}
}

func TestAcoustIDsFor_ContextDeadlineAbortsTheRateLimitWaitBeforeAnyRequest(t *testing.T) {
	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits++
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok","tracks":[]}`))
	}))
	defer srv.Close()

	limiter := rate.NewLimiter(rate.Every(time.Hour), 1)
	limiter.Allow()
	id := NewIdentifier("", "real-key").WithClusterEndpoint(srv.URL)
	id.limiter = limiter

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	_, err := id.AcoustIDsFor(ctx, "mbid-1")

	if err == nil {
		t.Fatal("AcoustIDsFor blocked on an exhausted limiter must fail, got nil error")
	}
	if hits != 0 {
		t.Errorf("hits = %d, want 0: the rate limit wait must abort before any request reaches the network", hits)
	}
}

func TestIdentifier_SharesOneLimiterAcrossLookupAndAcoustIDsFor(t *testing.T) {
	var lookupHits, clusterHits int
	lookupSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		lookupHits++
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok","results":[]}`))
	}))
	defer lookupSrv.Close()
	clusterSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		clusterHits++
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok","tracks":[]}`))
	}))
	defer clusterSrv.Close()

	limiter := rate.NewLimiter(rate.Every(50*time.Millisecond), 1)
	id := NewIdentifier(fakeFpcalcDir(t, 100), "real-key").
		WithEndpoint(lookupSrv.URL).
		WithClusterEndpoint(clusterSrv.URL)
	id.limiter = limiter

	start := time.Now()
	if _, err := id.Identify(context.Background(), "/tmp/full.mp3", 0); err != nil {
		t.Fatalf("Identify: %v", err)
	}
	if _, err := id.AcoustIDsFor(context.Background(), "mbid-1"); err != nil {
		t.Fatalf("AcoustIDsFor: %v", err)
	}
	elapsed := time.Since(start)

	if lookupHits != 1 || clusterHits != 1 {
		t.Fatalf("lookupHits = %d, clusterHits = %d, want 1 and 1", lookupHits, clusterHits)
	}
	if elapsed < 40*time.Millisecond {
		t.Errorf("elapsed = %v, want at least ~50ms: a shared limiter with a single burst token must make the second call wait", elapsed)
	}
}

func TestIdentify_ZeroDurationHintKeepsFpcalcsMeasuredDuration(t *testing.T) {
	var gotForm url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Fatalf("parse form: %v", err)
		}
		gotForm = r.PostForm
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok","results":[]}`))
	}))
	defer srv.Close()

	id := NewIdentifier(fakeFpcalcDir(t, 130), "real-key").WithEndpoint(srv.URL)

	if _, err := id.Identify(context.Background(), "/tmp/preview.mp3", 0); err != nil {
		t.Fatalf("Identify: %v", err)
	}

	if got := gotForm.Get("duration"); got != "130" {
		t.Errorf("duration = %q, want 130 (fpcalc's own measured duration, since no hint was given)", got)
	}
}

func TestIdentify_KeepsResultAtExactlyTheScoreFloor(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok","results":[
			{"id":"ac-floor","score":0.85,"recordings":[{"id":"mb-floor","title":"At Floor","duration":200}]}
		]}`))
	}))
	defer srv.Close()

	id := NewIdentifier(fakeFpcalcDir(t, 200), "real-key").WithEndpoint(srv.URL)

	match, err := id.Identify(context.Background(), "/tmp/floor.mp3", 0)
	if err != nil {
		t.Fatalf("Identify: %v", err)
	}

	if len(match.Results) != 1 || match.Results[0].ID != "ac-floor" {
		t.Fatalf("Results = %+v, want the result scored exactly at the floor kept", match.Results)
	}
}

func TestIdentify_TiedScoresKeepStableInputOrder(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok","results":[
			{"id":"ac-first","score":0.9,"recordings":[{"id":"mb-first","title":"First","duration":200}]},
			{"id":"ac-second","score":0.9,"recordings":[{"id":"mb-second","title":"Second","duration":210}]}
		]}`))
	}))
	defer srv.Close()

	id := NewIdentifier(fakeFpcalcDir(t, 200), "real-key").WithEndpoint(srv.URL)

	match, err := id.Identify(context.Background(), "/tmp/tied.mp3", 0)
	if err != nil {
		t.Fatalf("Identify: %v", err)
	}

	if len(match.Results) != 2 || match.Results[0].ID != "ac-first" || match.Results[1].ID != "ac-second" {
		t.Fatalf("Results = %+v, want tied scores to keep the committed input order [ac-first ac-second]", match.Results)
	}
}
