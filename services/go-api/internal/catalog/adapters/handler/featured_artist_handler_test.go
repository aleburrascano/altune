package handler_test

import (
	"altune/go-api/internal/catalog"
	shared "altune/go-api/internal/shared/kernel"
	"altune/go-api/internal/shared/logging"
	"context"
	"errors"
	"log/slog"
	"net/http"
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

func seedTracks(t *testing.T, repo *catalog.InMemoryTrackRepo, titles ...string) {
	t.Helper()
	for _, title := range titles {
		catalog.SeedTrack(t, repo, catalog.FixtureUserId(), title, "Artist", "Album")
	}
}

func TestHandleBackfillFeatured_BackToBackIsThrottled(t *testing.T) {
	repo := catalog.NewInMemoryTrackRepo()
	seedTracks(t, repo, "Song")
	router := trackRoutes(repo)

	first := serve(router, http.MethodPost, "/tracks/featured-backfill")
	assertStatus(t, first, http.StatusOK)

	second := serve(router, http.MethodPost, "/tracks/featured-backfill")
	assertStatus(t, second, http.StatusTooManyRequests)
	assertErrorCode(t, second, "catalog.backfill_cooling_down")
}

func TestHandleBackfillFeatured_ResumesFromTheOffsetQuery(t *testing.T) {
	repo := catalog.NewInMemoryTrackRepo()
	seedTracks(t, repo, "One", "Two", "Three")
	router := trackRoutes(repo)

	rec := serve(router, http.MethodPost, "/tracks/featured-backfill?offset=2")

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
	repo := catalog.NewInMemoryTrackRepo()
	seedTracks(t, repo, "Song")
	router := trackRoutes(repo)

	rejected := serve(router, http.MethodPost, "/tracks/featured-backfill?offset=-1")

	assertStatus(t, rejected, http.StatusBadRequest)
	accepted := serve(router, http.MethodPost, "/tracks/featured-backfill")
	assertStatus(t, accepted, http.StatusOK)
}

type listFailsAfterFirstPage struct {
	*catalog.InMemoryTrackRepo
	served bool
}

func (r *listFailsAfterFirstPage) ListForUser(ctx context.Context, userId shared.UserId, limit, offset int) ([]*catalog.Track, int, error) {
	if r.served {
		return nil, 0, errors.New("track store unreachable")
	}
	r.served = true
	tracks, _, err := r.InMemoryTrackRepo.ListForUser(ctx, userId, limit, offset)
	return tracks, len(tracks) + 1, err
}

func TestHandleBackfillFeatured_LogsPartialWorkWhenTheRunFails(t *testing.T) {
	prev := slog.Default()
	defer slog.SetDefault(prev)
	ring := logging.Setup("info", false)

	repo := &listFailsAfterFirstPage{InMemoryTrackRepo: catalog.NewInMemoryTrackRepo()}
	seedTracks(t, repo.InMemoryTrackRepo, "Song")
	router := trackRoutesWithBackfill(repo.InMemoryTrackRepo, catalog.NewBackfillFeaturedService(repo, repo, catalog.InMemoryFeaturedResolver{}))

	rec := serve(router, http.MethodPost, "/tracks/featured-backfill")

	assertStatus(t, rec, http.StatusInternalServerError)
	logged := false
	for _, record := range ring.Snapshot() {
		if record.Message != "featured_backfill.partial" {
			continue
		}
		logged = true
		if record.Attrs["scanned"] != "1" || record.Attrs["next_offset"] != "1" {
			t.Errorf("partial log attrs = %v, want scanned 1 and next_offset 1", record.Attrs)
		}
	}
	if !logged {
		t.Error("no featured_backfill.partial log line: the failed run's completed work was dropped")
	}
}

func TestHandleListFeaturing_RejectsAnUnreadableDeezerID(t *testing.T) {
	router := trackRoutes(catalog.NewInMemoryTrackRepo())

	for _, query := range []string{"deezer_id=abc", "deezer_id=0", "deezer_id=-5", "name=SZA&deezer_id=abc", "deezer_id=99999999999999999999"} {
		t.Run(query, func(t *testing.T) {
			rec := serve(router, http.MethodGet, "/tracks/featuring?"+query)

			assertStatus(t, rec, http.StatusBadRequest)
			assertErrorCode(t, rec, "catalog.invalid_deezer_id")
		})
	}
}

func TestHandleListFeaturing_RequiresOneArtistKey(t *testing.T) {
	router := trackRoutes(catalog.NewInMemoryTrackRepo())

	rec := serve(router, http.MethodGet, "/tracks/featuring")

	assertStatus(t, rec, http.StatusBadRequest)
	assertErrorCode(t, rec, "catalog.featured_artist_key_required")
}

func TestHandleListFeaturing_AcceptsAPositiveDeezerID(t *testing.T) {
	repo := catalog.NewInMemoryTrackRepo()
	track := catalog.SeedTrack(t, repo, catalog.FixtureUserId(), "Feature", "Artist", "Album")
	track.FeaturedArtists = []catalog.FeaturedArtist{catalog.NewFeaturedArtistIdentityOnly("SZA", "", 42)}
	router := trackRoutes(repo)

	rec := serve(router, http.MethodGet, "/tracks/featuring?deezer_id=42")

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

func (r *stallsAfterFirstResolver) Resolve(ctx context.Context, _, _ string) ([]catalog.FeaturedArtist, error) {
	r.calls++
	if r.calls > 1 {
		<-ctx.Done()
	}
	return nil, nil
}

func TestHandleBackfillFeatured_AnswersWithinTheBudgetAndResumesWithoutCooldown(t *testing.T) {
	repo := catalog.NewInMemoryTrackRepo()
	seedTracks(t, repo, "One", "Two", "Three")
	backfill := catalog.NewBackfillFeaturedService(repo, repo, &stallsAfterFirstResolver{}, catalog.WithBackfillTimeBudget(50*time.Millisecond))
	router := trackRoutesWithBackfill(repo, backfill)

	start := time.Now()
	first := serve(router, http.MethodPost, "/tracks/featured-backfill")
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("response took %v, want it inside the budget", elapsed)
	}
	assertStatus(t, first, http.StatusOK)
	var body backfillResponse
	decodeJSON(t, first, &body)
	if !body.Truncated || body.NextOffset <= 0 {
		t.Fatalf("response = %+v, want truncated with next offset past the start", body)
	}

	resumed := serve(router, http.MethodPost, "/tracks/featured-backfill?offset=1")
	assertStatus(t, resumed, http.StatusOK)
}
