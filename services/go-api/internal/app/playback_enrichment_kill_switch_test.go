package app

import (
	"altune/go-api/internal/auth"
	"altune/go-api/internal/playback/domain"
	"altune/go-api/internal/shared"
	"altune/go-api/internal/shared/config"
	"bytes"
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	discoveryHandler "altune/go-api/internal/discovery/adapters/handler"
	playbackMetrics "altune/go-api/internal/playback/adapters/metrics"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Reproduces #1125 on the production router: with
// PLAYBACK_NOW_PLAYING_ENRICHMENT_ENABLED=false a resume against a wedged
// catalog database returns the queue immediately instead of blocking on the 3s
// lookup deadline, never flags current_track_unavailable, and records no
// enrichment failure because no lookup ran.
func TestPlaybackEnrichmentKillSwitch_ShedsCatalogLookupOnResume(t *testing.T) {
	pool, err := pgxpool.New(context.Background(), blackHoleDatabase(t))
	if err != nil {
		t.Fatalf("pgxpool.New: %v", err)
	}
	t.Cleanup(pool.Close)

	a := &App{cfg: &config.Config{MusicDir: t.TempDir(), NowPlayingEnrichmentEnabled: false}, sem: make(chan struct{}, 1), pool: pool}
	cat, err := a.wireCatalog(nil, nil, nil)
	if err != nil {
		t.Fatalf("wireCatalog: %v", err)
	}
	user := shared.NewUserId(uuid.New())
	state, err := domain.NewQueueState(domain.QueueStateInput{
		UserId: user, TrackIds: []string{uuid.NewString(), uuid.NewString()}, CurrentIdx: 1, RepeatMode: domain.RepeatOff,
	})
	if err != nil {
		t.Fatalf("NewQueueState: %v", err)
	}
	metrics := playbackMetrics.NewExpvarPlaybackMetrics()
	queue := newQueueHandler(
		newQueueService(&storedQueue{state: state}, cat.trackRepo, metrics, a.cfg.HasNowPlayingEnrichment()),
		metrics)

	verifier := auth.VerifierFunc(func(_ context.Context, token string) (auth.VerifiedToken, error) {
		if token == operatorToken {
			return auth.VerifiedToken{UserID: user, ExpiresAt: time.Now().Add(time.Hour)}, nil
		}
		return auth.VerifiedToken{}, errors.New("bad token")
	})
	r := a.mountRoutes(verifier, cat, queue,
		discoveryHandler.NewDiscoveryHandler(discoveryHandler.DiscoveryServices{}), nil)

	before := playbackMetrics.ReadSnapshot()
	start := time.Now()
	code, body := callAdmin(t, r, http.MethodGet, "/v1/playback/queue-state", operatorToken)
	elapsed := time.Since(start)

	if code != http.StatusOK {
		t.Fatalf("resume with enrichment disabled: status %d, want 200; body %s", code, body)
	}
	if elapsed >= time.Second {
		t.Errorf("resume took %s; a disabled lookup must not wait on the catalog deadline", elapsed)
	}
	if !bytes.Contains(body, []byte(`"current_index":1`)) {
		t.Errorf("resume body does not carry the queue: %s", body)
	}
	if bytes.Contains(body, []byte(`current_track`)) {
		t.Errorf("disabled enrichment must emit neither current_track nor current_track_unavailable: %s", body)
	}
	after := playbackMetrics.ReadSnapshot()
	if after.EnrichmentFailures != before.EnrichmentFailures || after.NowPlayingLookupTimeouts != before.NowPlayingLookupTimeouts {
		t.Errorf("a disabled lookup must not run: enrichment failures %d->%d, timeouts %d->%d",
			before.EnrichmentFailures, after.EnrichmentFailures, before.NowPlayingLookupTimeouts, after.NowPlayingLookupTimeouts)
	}
}
