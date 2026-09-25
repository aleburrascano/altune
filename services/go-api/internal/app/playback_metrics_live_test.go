package app

import (
	"altune/go-api/internal/auth"
	"altune/go-api/internal/playback/domain"
	"altune/go-api/internal/playback/ports"
	"altune/go-api/internal/shared"
	"altune/go-api/internal/shared/config"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	adminHandler "altune/go-api/internal/admin/handler"
	discoveryHandler "altune/go-api/internal/discovery/adapters/handler"
	playbackMetrics "altune/go-api/internal/playback/adapters/metrics"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// storedQueue is a single-user QueueStateRepository standing in for the
// queue-state table, so the test can wedge only the catalog side.
type storedQueue struct {
	mu    sync.Mutex
	state *domain.QueueState
}

func (s *storedQueue) Upsert(_ context.Context, state *domain.QueueState) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.state = state
	return nil
}

func (s *storedQueue) GetForUser(context.Context, shared.UserId) (*domain.QueueState, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.state, nil
}

func (s *storedQueue) UpdatePosition(context.Context, *domain.QueuePosition) error { return nil }

func (s *storedQueue) DeleteForUser(context.Context, shared.UserId) error { return nil }

var _ ports.QueueStateRepository = (*storedQueue)(nil)

// A queue resume whose now-playing lookup hits a wedged catalog database still
// succeeds, logs the owning user without the raw track id, and the operator
// reads the enrichment failure back from the playback section of
// GET /admin/metrics/live on the production router.
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
	cat, err := a.wireCatalog(nil, nil, nil)
	if err != nil {
		t.Fatalf("wireCatalog: %v", err)
	}
	operator := shared.NewUserId(uuid.New())
	nowPlayingId := uuid.NewString()
	state, err := domain.NewQueueState(domain.QueueStateInput{
		UserId: operator, TrackIds: []string{uuid.NewString(), nowPlayingId}, CurrentIdx: 1, RepeatMode: domain.RepeatOff,
	})
	if err != nil {
		t.Fatalf("NewQueueState: %v", err)
	}
	metrics := playbackMetrics.NewExpvarPlaybackMetrics()
	queue := newQueueHandler(newQueueService(&storedQueue{state: state}, cat.trackRepo, metrics, true), metrics)

	verifier := auth.VerifierFunc(func(_ context.Context, token string) (auth.VerifiedToken, error) {
		if token == operatorToken {
			return auth.VerifiedToken{UserID: operator, ExpiresAt: time.Now().Add(time.Hour)}, nil
		}
		return auth.VerifiedToken{}, errors.New("bad token")
	})
	r := a.mountRoutes(verifier, cat, queue,
		discoveryHandler.NewDiscoveryHandler(discoveryHandler.DiscoveryServices{}), nil)
	mountAdmin(r, verifier, adminPrincipals{operator: operator.String()}, adminHandler.New(nil, nil).WithLiveMetrics(liveMetricsSnapshot))

	var logs bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(prev) })

	before := playbackMetrics.ReadSnapshot()
	code, body := callAdmin(t, r, http.MethodGet, "/v1/playback/queue-state", operatorToken)
	if code != http.StatusOK {
		t.Fatalf("resume against a wedged catalog: status %d, want 200; body %s", code, body)
	}
	if !bytes.Contains(body, []byte(`"current_track_unavailable":true`)) {
		t.Errorf("resume body does not flag the failed enrichment: %s", body)
	}

	code, body = callAdmin(t, r, http.MethodGet, "/admin/metrics/live", operatorToken)
	if code != http.StatusOK {
		t.Fatalf("operator metrics read: status %d, want 200; body %s", code, body)
	}
	var got struct {
		Playback playbackMetrics.Snapshot `json:"playback"`
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
