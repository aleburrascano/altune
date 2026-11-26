package handler_test

import (
	"altune/go-api/internal/library"
	"altune/go-api/internal/shared/ids"
	"altune/go-api/internal/shared/logging"
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"testing"
	"time"
)

type backfillResponse struct {
	Scanned    int  `json:"scanned"`
	Updated    int  `json:"updated"`
	Failed     int  `json:"failed"`
	Truncated  bool `json:"truncated"`
	NextOffset int  `json:"next_offset"`
}

func seedTracks(t *testing.T, repo *library.InMemoryTrackRepo, titles ...string) {
	t.Helper()
	for _, title := range titles {
		library.SeedTrack(t, repo, library.FixtureUserId(), title, "Artist", "Album")
	}
}

func TestHandleBackfillFeatured_BackToBackIsThrottled(t *testing.T) {
	repo := library.NewInMemoryTrackRepo()
	seedTracks(t, repo, "Song")
	router := trackRoutes(repo)

	first := request(t, router, http.MethodPost, "/tracks/featured-backfill", nil)
	assertStatus(t, first, http.StatusOK)

	second := request(t, router, http.MethodPost, "/tracks/featured-backfill", nil)
	assertStatus(t, second, http.StatusTooManyRequests)
	assertErrorCode(t, second, "catalog.backfill_cooling_down")
}

func TestHandleBackfillFeatured_ResumesFromTheOffsetQuery(t *testing.T) {
	repo := library.NewInMemoryTrackRepo()
	seedTracks(t, repo, "One", "Two", "Three")
	router := trackRoutes(repo)

	rec := request(t, router, http.MethodPost, "/tracks/featured-backfill?offset=2", nil)

	assertStatus(t, rec, http.StatusOK)
	var body backfillResponse
	decodeJSON(t, rec, &body)
	if body.Scanned != 1 {
		t.Errorf("scanned = %d, want 1 (the offset skipped the first two tracks)", body.Scanned)
	}
	if body.Truncated || body.NextOffset != 3 {
		t.Errorf("response = %+v, want truncated false and next offset 3", body)
	}
}

func TestHandleBackfillFeatured_RejectsANegativeOffset(t *testing.T) {
	repo := library.NewInMemoryTrackRepo()
	seedTracks(t, repo, "Song")
	router := trackRoutes(repo)

	rejected := request(t, router, http.MethodPost, "/tracks/featured-backfill?offset=-1", nil)

	assertStatus(t, rejected, http.StatusBadRequest)
	accepted := request(t, router, http.MethodPost, "/tracks/featured-backfill", nil)
	assertStatus(t, accepted, http.StatusOK)
}

func TestHandleBackfillFeatured_RejectsANonNumericOffset(t *testing.T) {
	repo := library.NewInMemoryTrackRepo()
	seedTracks(t, repo, "Song")
	router := trackRoutes(repo)

	rejected := request(t, router, http.MethodPost, "/tracks/featured-backfill?offset=abc", nil)

	assertStatus(t, rejected, http.StatusBadRequest)
	assertProblem(t, rejected, "catalog.validation_error", "offset must be an integer")
}

type listFailsAfterFirstPage struct {
	*library.InMemoryTrackRepo
	served bool
}

func (r *listFailsAfterFirstPage) ListForUser(ctx context.Context, userId ids.UserId, limit, offset int) ([]*library.Track, int, error) {
	if r.served {
		return nil, 0, errors.New("track store unreachable")
	}
	r.served = true
	tracks, _, err := r.InMemoryTrackRepo.ListForUser(ctx, userId, limit, offset)
	return tracks, len(tracks) + 1, err
}

func TestHandleBackfillFeatured_LogsTheFailedRunsProgressOnceAtError(t *testing.T) {
	prev := slog.Default()
	defer slog.SetDefault(prev)
	ring := logging.Setup("info", false)

	repo := &listFailsAfterFirstPage{InMemoryTrackRepo: library.NewInMemoryTrackRepo()}
	seedTracks(t, repo.InMemoryTrackRepo, "Song")
	router := trackRoutesWith(repo.InMemoryTrackRepo, trackDeps{backfill: library.NewBackfillFeaturedService(repo, repo, library.InMemoryFeaturedResolver{})})

	rec := request(t, router, http.MethodPost, "/tracks/featured-backfill", nil)

	assertStatus(t, rec, http.StatusInternalServerError)
	var failures []string
	for _, record := range ring.Snapshot() {
		if record.Level == slog.LevelError.String() || record.Level == slog.LevelWarn.String() {
			failures = append(failures, record.Message+": "+record.Attrs["error"])
		}
	}
	if len(failures) != 1 {
		t.Fatalf("warning and error lines = %q, want the one request failure", failures)
	}
	if !strings.Contains(failures[0], "scanned 1,") || !strings.Contains(failures[0], "next offset 1:") {
		t.Errorf("failure line = %q, want the run's progress (scanned 1, next offset 1) in its error", failures[0])
	}
	if strings.Contains(rec.Body.String(), "scanned") {
		t.Errorf("body = %s, want the progress kept out of the client's answer", rec.Body.String())
	}
}

func TestHandleListFeaturing_RejectsAnUnreadableDeezerID(t *testing.T) {
	router := trackRoutes(library.NewInMemoryTrackRepo())

	for _, query := range []string{"deezer_id=abc", "deezer_id=0", "deezer_id=-5", "name=SZA&deezer_id=abc", "deezer_id=99999999999999999999"} {
		t.Run(query, func(t *testing.T) {
			rec := request(t, router, http.MethodGet, "/tracks/featuring?"+query, nil)

			assertStatus(t, rec, http.StatusBadRequest)
			assertErrorCode(t, rec, "catalog.invalid_deezer_id")
		})
	}
}

func TestHandleListFeaturing_RequiresOneArtistKey(t *testing.T) {
	router := trackRoutes(library.NewInMemoryTrackRepo())

	rec := request(t, router, http.MethodGet, "/tracks/featuring", nil)

	assertStatus(t, rec, http.StatusBadRequest)
	assertErrorCode(t, rec, "catalog.featured_artist_key_required")
}

func TestHandleListFeaturing_AcceptsAPositiveDeezerID(t *testing.T) {
	repo := library.NewInMemoryTrackRepo()
	track := library.SeedTrack(t, repo, library.FixtureUserId(), "Feature", "Artist", "Album")
	track.FeaturedArtists = []library.FeaturedArtist{library.NewFeaturedArtistIdentityOnly("SZA", "", 42)}
	router := trackRoutes(repo)

	rec := request(t, router, http.MethodGet, "/tracks/featuring?deezer_id=42", nil)

	assertStatus(t, rec, http.StatusOK)
	var body struct {
		Total int `json:"total"`
	}
	decodeJSON(t, rec, &body)
	if body.Total != 1 {
		t.Errorf("total = %d, want 1 (the track featuring deezer artist 42)", body.Total)
	}
}

type stallsAfterFirstResolver struct {
	calls int
}

func (r *stallsAfterFirstResolver) Resolve(ctx context.Context, _, _ string) ([]library.FeaturedArtist, error) {
	r.calls++
	if r.calls > 1 {
		<-ctx.Done()
	}
	return nil, nil
}

func TestHandleBackfillFeatured_AnswersWithinTheBudgetAndResumesWithoutCooldown(t *testing.T) {
	repo := library.NewInMemoryTrackRepo()
	seedTracks(t, repo, "One", "Two", "Three")
	backfill := library.NewBackfillFeaturedService(repo, repo, &stallsAfterFirstResolver{}, library.WithBackfillTimeBudget(50*time.Millisecond))
	router := trackRoutesWith(repo, trackDeps{backfill: backfill})

	start := time.Now()
	first := request(t, router, http.MethodPost, "/tracks/featured-backfill", nil)
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("response took %v, want it inside the budget", elapsed)
	}
	assertStatus(t, first, http.StatusOK)
	var body backfillResponse
	decodeJSON(t, first, &body)
	if !body.Truncated || body.NextOffset <= 0 {
		t.Fatalf("response = %+v, want truncated with next offset past the start", body)
	}

	resumed := request(t, router, http.MethodPost, "/tracks/featured-backfill?offset=1", nil)
	assertStatus(t, resumed, http.StatusOK)
}
