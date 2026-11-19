package app

import (
	"altune/go-api/internal/auth"
	"altune/go-api/internal/catalog"
	"altune/go-api/internal/discovery"
	"altune/go-api/internal/playback"
	"altune/go-api/internal/shared/config"
	shared "altune/go-api/internal/shared/kernel"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

type storedQueue struct {
	mu    sync.Mutex
	state *playback.QueueState
}

func (s *storedQueue) Upsert(_ context.Context, state *playback.QueueState) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.state = state
	return nil
}

func (s *storedQueue) GetForUser(context.Context, shared.UserId) (*playback.QueueState, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.state, nil
}

func (s *storedQueue) UpdatePosition(context.Context, *playback.QueuePosition) error { return nil }

func (s *storedQueue) DeleteForUser(context.Context, shared.UserId) error { return nil }

var _ playback.QueueStateRepository = (*storedQueue)(nil)

func TestPlaybackEnrichmentFailure_ReachesOperatorLiveMetrics(t *testing.T) {
	if testing.Short() {
		t.Skip("waits out the 3s production now-playing lookup deadline")
	}
	pool, err := pgxpool.New(context.Background(), blackHoleDatabase(t))
	if err != nil {
		t.Fatalf("pgxpool.New: %v", err)
	}
	t.Cleanup(pool.Close)

	a := &App{cfg: &config.Config{MusicDir: t.TempDir()}, sem: make(chan struct{}, 1), pool: pool}
	cat, err := a.wireCatalog(t.Context(), nil, &discovery.Discovery{})
	if err != nil {
		t.Fatalf("wireCatalog: %v", err)
	}
	operator := shared.NewUserId(uuid.New())
	nowPlayingId := uuid.NewString()
	state, err := playback.NewQueueState(playback.QueueStateInput{
		UserId: operator, TrackIds: []string{uuid.NewString(), nowPlayingId}, CurrentIdx: 1, RepeatMode: playback.RepeatOff,
	})
	if err != nil {
		t.Fatalf("NewQueueState: %v", err)
	}
	metrics := playback.NewExpvarPlaybackMetrics()
	queue := newQueueHandler(newQueueService(&storedQueue{state: state}, cat.trackRepo, metrics), metrics, playback.DefaultQueueStateRateLimit)

	verifier := auth.VerifierFunc(func(_ context.Context, token string) (auth.VerifiedToken, error) {
		if token == operatorToken {
			return auth.VerifiedToken{UserID: operator, ExpiresAt: time.Now().Add(time.Hour)}, nil
		}
		return auth.VerifiedToken{}, errors.New("bad token")
	})
	r := a.mountRoutes(verifier, cat, queue,
		discovery.NewDiscoveryHandler(discovery.DiscoveryServices{}))
	mountObserveLiveMetrics(r, verifier, operator, liveMetricsSnapshot)

	var logs bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(prev) })

	before := playback.ReadSnapshot()
	code, body := callObserveAs(t, r, http.MethodGet, "/v1/playback/queue-state", operatorToken)
	if code != http.StatusOK {
		t.Fatalf("resume against a wedged catalog: status %d, want 200; body %s", code, body)
	}
	if !bytes.Contains(body, []byte(`"current_track_unavailable":true`)) {
		t.Errorf("resume body does not flag the failed enrichment: %s", body)
	}

	code, body = callObserveAs(t, r, http.MethodGet, "/observe/metrics/live", operatorToken)
	if code != http.StatusOK {
		t.Fatalf("operator metrics read: status %d, want 200; body %s", code, body)
	}
	var got struct {
		Playback playback.MetricsSnapshot `json:"playback"`
	}
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("decode %s: %v", body, err)
	}
	if got.Playback.EnrichmentFailures != before.EnrichmentFailures+1 {
		t.Errorf("playback.now_playing_enrichment_failures_total %d, want %d; body %s",
			got.Playback.EnrichmentFailures, before.EnrichmentFailures+1, body)
	}
	if got.Playback.NowPlayingLookupTimeouts != before.NowPlayingLookupTimeouts+1 {
		t.Errorf("playback.now_playing_lookup_timeouts_total %d, want %d; body %s",
			got.Playback.NowPlayingLookupTimeouts, before.NowPlayingLookupTimeouts+1, body)
	}

	out := logs.String()
	if !strings.Contains(out, `"msg":"resume.current_track_enrichment_failed"`) ||
		!strings.Contains(out, `"user_id":"`+operator.String()+`"`) {
		t.Errorf("enrichment-failure log line missing or without user_id; logs %s", out)
	}
	if strings.Contains(out, nowPlayingId) {
		t.Errorf("logs leak the raw now-playing track id %s; logs %s", nowPlayingId, out)
	}
}

func TestWirePlayback_SweepReapsErasedQueueStates(t *testing.T) {
	pool, err := pgxpool.New(context.Background(), blackHoleDatabase(t))
	if err != nil {
		t.Fatalf("pgxpool.New: %v", err)
	}
	pool.Close()
	a := &App{cfg: &config.Config{MusicDir: t.TempDir()}, sem: make(chan struct{}, 1), pool: pool}

	_, err = a.wirePlayback(t.Context(), nil).forgetDeletedIdentities.Execute(context.Background())

	if err == nil || !strings.Contains(err.Error(), "reap erased queue states") {
		t.Fatalf("Execute error = %v, want it to carry the reap failure, proving the reaper is wired", err)
	}
}

func TestQueueRateLimit_EnvThrottlesQueueStateAtConfiguredBurst(t *testing.T) {
	t.Setenv("SUPABASE_PROJECT_URL", "https://example.supabase.co")
	t.Setenv("SUPABASE_JWT_JWKS_URL", "https://example.supabase.co/auth/v1/.well-known/jwks.json")
	t.Setenv("PLAYBACK_QUEUE_RATE_EVERY", "1h")
	t.Setenv("PLAYBACK_QUEUE_RATE_BURST", "2")
	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	user := shared.NewUserId(uuid.New())
	metrics := playback.NewExpvarPlaybackMetrics()
	queue := newQueueHandler(newQueueService(&storedQueue{}, nil, metrics), metrics, queueRateLimit(cfg))
	routes := queue.Routes()

	var codes []int
	for range 3 {
		req := httptest.NewRequest(http.MethodPut, "/queue-state",
			strings.NewReader(`{"track_ids":[],"repeat_mode":"off","source_id":"library"}`))
		req = req.WithContext(auth.ContextWithUserID(req.Context(), user))
		rec := httptest.NewRecorder()
		routes.ServeHTTP(rec, req)
		codes = append(codes, rec.Code)
	}
	if codes[0] == http.StatusTooManyRequests || codes[1] == http.StatusTooManyRequests || codes[2] != http.StatusTooManyRequests {
		t.Errorf("statuses = %v, want the third PUT throttled at burst 2", codes)
	}
}

func TestQueueRateLimit_DefaultsMatchCompileTimeLimit(t *testing.T) {
	t.Setenv("SUPABASE_PROJECT_URL", "https://example.supabase.co")
	t.Setenv("SUPABASE_JWT_JWKS_URL", "https://example.supabase.co/auth/v1/.well-known/jwks.json")
	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	if got := queueRateLimit(cfg); got != playback.DefaultQueueStateRateLimit {
		t.Errorf("limit = %+v, want %+v", got, playback.DefaultQueueStateRateLimit)
	}
}

type catalogTracksFunc func(id catalog.TrackId, userId shared.UserId) (*catalog.Track, error)

func (f catalogTracksFunc) GetByID(_ context.Context, id catalog.TrackId, userId shared.UserId) (*catalog.Track, error) {
	return f(id, userId)
}

func TestCatalogTrackSource_MapsTheUsersCatalogTrack(t *testing.T) {
	user := shared.NewUserId(uuid.New())
	id := catalog.NewTrackId()
	artwork, duration := "https://img.example/a.jpg", 212.5
	var askedFor catalog.TrackId
	var askedBy shared.UserId
	source := catalogTrackSource{tracks: catalogTracksFunc(func(trackId catalog.TrackId, userId shared.UserId) (*catalog.Track, error) {
		askedFor, askedBy = trackId, userId
		return &catalog.Track{
			ID: id, Title: "Track Y", Artist: "Artist", ArtworkURL: &artwork, DurationSeconds: &duration,
			AcquisitionStatus: catalog.AcquisitionReady,
		}, nil
	})}

	got, err := source.NowPlayingTrack(context.Background(), user, id)
	if err != nil {
		t.Fatalf("NowPlayingTrack: %v", err)
	}
	if askedFor != id || askedBy != user {
		t.Errorf("catalog asked for (%v, %v), want (%v, %v)", askedFor, askedBy, id, user)
	}
	want := playback.NowPlayingTrack{
		Id: id.String(), Title: "Track Y", Artist: "Artist", ArtworkURL: &artwork, DurationSeconds: &duration,
		AcquisitionStatus: "ready",
	}
	if got == nil || *got != want {
		t.Errorf("NowPlayingTrack = %+v, want %+v", got, want)
	}
}

func TestCatalogTrackSource_AbsentTrackIsNoTrack(t *testing.T) {
	source := catalogTrackSource{tracks: catalogTracksFunc(func(catalog.TrackId, shared.UserId) (*catalog.Track, error) { return nil, nil })}

	got, err := source.NowPlayingTrack(context.Background(), shared.NewUserId(uuid.New()), catalog.NewTrackId())
	if got != nil || err != nil {
		t.Errorf("NowPlayingTrack = (%+v, %v), want no track and no error", got, err)
	}
}

func TestCatalogTrackSource_TransientCatalogFailureIsASourceOutage(t *testing.T) {
	transient := fmt.Errorf("get track: %w", catalog.ErrDBTransient)
	source := catalogTrackSource{tracks: catalogTracksFunc(func(catalog.TrackId, shared.UserId) (*catalog.Track, error) { return nil, transient })}

	_, err := source.NowPlayingTrack(context.Background(), shared.NewUserId(uuid.New()), catalog.NewTrackId())
	if !errors.Is(err, playback.ErrTrackSourceUnavailable) || !errors.Is(err, catalog.ErrDBTransient) {
		t.Errorf("err = %v, want it to be both ErrTrackSourceUnavailable and catalog's ErrDBTransient", err)
	}
}

func TestCatalogTrackSource_RowErrorIsNotASourceOutage(t *testing.T) {
	rowErr := errors.New("acquisition status no longer parses")
	source := catalogTrackSource{tracks: catalogTracksFunc(func(catalog.TrackId, shared.UserId) (*catalog.Track, error) { return nil, rowErr })}

	_, err := source.NowPlayingTrack(context.Background(), shared.NewUserId(uuid.New()), catalog.NewTrackId())
	if !errors.Is(err, rowErr) || errors.Is(err, playback.ErrTrackSourceUnavailable) {
		t.Errorf("err = %v, want the row error itself, not a source outage", err)
	}
}
