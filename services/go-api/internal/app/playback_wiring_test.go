package app_test

import (
	"altune/go-api/internal/playback"
	"altune/go-api/internal/shared/config"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func putQueueState(t *testing.T, h http.Handler, token string) int {
	t.Helper()
	req := httptest.NewRequest(http.MethodPut, "/v1/playback/queue-state",
		strings.NewReader(`{"track_ids":[],"repeat_mode":"off","source_id":"library"}`))
	req.RemoteAddr = "192.0.2.1:1234"
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec.Code
}

func TestQueueRateLimit_TheConfiguredBurstThrottlesQueueState(t *testing.T) {
	fixture := newJWKSFixture(t)
	fixture.cfg.PlaybackQueueRateEvery, fixture.cfg.PlaybackQueueRateBurst = time.Hour, 2
	h := assembleWith(t, fixture.cfg, refuseEgress{}).assembly.Handler()
	token := signedToken(t, fixture.key, fixture.operator)

	var codes []int
	for range 3 {
		codes = append(codes, putQueueState(t, h, token))
	}

	if codes[0] == http.StatusTooManyRequests || codes[1] == http.StatusTooManyRequests || codes[2] != http.StatusTooManyRequests {
		t.Errorf("statuses = %v, want the third PUT throttled at burst 2", codes)
	}
}

func TestQueueRateLimit_TheConfigDefaultsMatchPlaybacksOwnLimit(t *testing.T) {
	t.Setenv("SUPABASE_PROJECT_URL", "https://example.supabase.co")
	t.Setenv("SUPABASE_JWT_JWKS_URL", "https://example.supabase.co/auth/v1/.well-known/jwks.json")
	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}

	got := playback.QueueSaveLimit{Every: cfg.PlaybackQueueRateEvery, Burst: cfg.PlaybackQueueRateBurst}
	if got != playback.DefaultQueueStateRateLimit {
		t.Errorf("default limit = %+v, want %+v", got, playback.DefaultQueueStateRateLimit)
	}
}

func TestQueueRateLimit_TheConfiguredIntervalRefillsTheBurst(t *testing.T) {
	fixture := newJWKSFixture(t)
	fixture.cfg.PlaybackQueueRateEvery, fixture.cfg.PlaybackQueueRateBurst = time.Second, 1
	h := assembleWith(t, fixture.cfg, refuseEgress{}).assembly.Handler()
	token := signedToken(t, fixture.key, fixture.operator)

	first, throttled := putQueueState(t, h, token), putQueueState(t, h, token)
	time.Sleep(1100 * time.Millisecond)
	refilled := putQueueState(t, h, token)

	if first == http.StatusTooManyRequests || throttled != http.StatusTooManyRequests || refilled == http.StatusTooManyRequests {
		t.Errorf("statuses = [%d %d %d], want the second PUT throttled and the third admitted once the one-second interval refilled the burst", first, throttled, refilled)
	}
}

func TestPlayback_TheErasureSweepReapsErasedQueueStates(t *testing.T) {
	run := assembleOn(t, jwksConfig(t), emptyDatabasePool(t), refuseEgress{})

	deadline := time.Now().Add(time.Minute)
	for {
		for _, line := range strings.Split(run.logs.String(), "\n") {
			if strings.Contains(line, `msg="deleted identity erasure failed"`) && strings.Contains(line, "reap erased queue states") {
				return
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("no erasure run tried to reap erased queue states within a minute of leading a database with no tables:\n%s", run.logs.String())
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func livePlaybackMetrics(t *testing.T, h http.Handler, token string) playback.MetricsSnapshot {
	t.Helper()
	code, body := getAs(t, h, "/observe/metrics/live", token)
	if code != http.StatusOK {
		t.Fatalf("operator read of /observe/metrics/live = %d, want 200; body %s", code, body)
	}
	var got struct {
		Playback playback.MetricsSnapshot `json:"playback"`
	}
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("decode %s: %v", body, err)
	}
	return got.Playback
}

func holdTracksLocked(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	tx, err := pool.Begin(t.Context())
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	t.Cleanup(func() { _ = tx.Rollback(context.Background()) })
	if _, err := tx.Exec(t.Context(), "LOCK TABLE tracks IN ACCESS EXCLUSIVE MODE"); err != nil {
		t.Fatalf("lock tracks: %v", err)
	}
}

func TestPlayback_AWedgedCatalogFlagsTheResumeAndReachesOperatorLiveMetrics(t *testing.T) {
	pool := migratedDatabasePool(t)
	f := newJWKSFixture(t)
	f.cfg.PlaybackNowPlayingLookupTimeout = time.Nanosecond
	run := assembleOn(t, f.cfg, pool, refuseEgress{})
	h := run.assembly.Handler()
	token := signedToken(t, f.key, f.operator)
	nowPlaying := uuid.NewString()
	if _, err := pool.Exec(t.Context(), `INSERT INTO playback_queue_state (user_id, track_ids, current_idx) VALUES ($1, $2, 1)`,
		f.operator, []string{uuid.NewString(), nowPlaying}); err != nil {
		t.Fatalf("store the queue state: %v", err)
	}
	before := livePlaybackMetrics(t, h, token)
	holdTracksLocked(t, pool)

	code, body := getAs(t, h, "/v1/playback/queue-state", token)

	if code != http.StatusOK {
		t.Fatalf("resume against a wedged catalog: status %d, want 200; body %s", code, body)
	}
	if !strings.Contains(string(body), `"current_track_unavailable":true`) {
		t.Errorf("resume body does not flag the failed enrichment: %s", body)
	}
	after := livePlaybackMetrics(t, h, token)
	if after.EnrichmentFailures != before.EnrichmentFailures+1 || after.NowPlayingLookupTimeouts != before.NowPlayingLookupTimeouts+1 {
		t.Errorf("live playback enrichment failures %d and lookup timeouts %d, want %d and %d",
			after.EnrichmentFailures, after.NowPlayingLookupTimeouts, before.EnrichmentFailures+1, before.NowPlayingLookupTimeouts+1)
	}
	logged := run.logs.String()
	if !strings.Contains(logged, "msg=resume.current_track_enrichment_failed") || !strings.Contains(logged, "user_id="+f.operator.String()) {
		t.Errorf("enrichment-failure log line missing or without user_id; logs %s", logged)
	}
	if strings.Contains(logged, nowPlaying) {
		t.Errorf("logs leak the raw now-playing track id %s; logs %s", nowPlaying, logged)
	}
}
