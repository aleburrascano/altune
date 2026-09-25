package handler

import (
	"altune/go-api/internal/auth"
	"altune/go-api/internal/catalog/catalogtest"
	"altune/go-api/internal/catalog/domain"
	"altune/go-api/internal/catalog/service"
	"altune/go-api/internal/shared"
	"altune/go-api/internal/shared/logging"
	"context"
	"errors"
	"log/slog"
	"net/http"
	"testing"

	"github.com/go-chi/chi/v5"
)

type backfillResponse struct {
	Scanned    int  `json:"scanned"`
	Updated    int  `json:"updated"`
	Failed     int  `json:"failed"`
	Truncated  bool `json:"truncated"`
	NextOffset int  `json:"next_offset"`
}

// Back-to-back backfill calls from the same user must be throttled: the second
// POST lands inside the cooldown and is rejected with 429 instead of starting
// another run of up to 10k external lookups.
func TestHandleBackfillFeatured_BackToBackIsThrottled(t *testing.T) {
	trackRepo := catalogtest.NewTrackRepo()
	trackRepo.Seed(makeTrack(testUserId, "Song", "Artist", "Album"))
	_, router := buildTrackHandler(trackRepo, nil)

	first := serve(t, router, http.MethodPost, "/tracks/featured-backfill", nil)
	assertStatus(t, first, http.StatusOK)

	second := serve(t, router, http.MethodPost, "/tracks/featured-backfill", nil)
	assertStatus(t, second, http.StatusTooManyRequests)
	var body struct {
		Code string `json:"code"`
	}
	decodeJSON(t, second, &body)
	if body.Code != "catalog.backfill_cooling_down" {
		t.Errorf("code = %q, want catalog.backfill_cooling_down", body.Code)
	}
}

// A run truncated by the page cap hands back a next_offset; the follow-up call
// passes it as ?offset= to continue where that run stopped.
func TestHandleBackfillFeatured_ResumesFromTheOffsetQuery(t *testing.T) {
	trackRepo := catalogtest.NewTrackRepo()
	for _, title := range []string{"One", "Two", "Three"} {
		trackRepo.Seed(makeTrack(testUserId, title, "Artist", "Album"))
	}
	_, router := buildTrackHandler(trackRepo, nil)

	rec := serve(t, router, http.MethodPost, "/tracks/featured-backfill?offset=2", nil)

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

// A negative offset is a client mistake, not a run: it answers 400 and leaves
// the caller's next real backfill admitted.
func TestHandleBackfillFeatured_RejectsANegativeOffset(t *testing.T) {
	trackRepo := catalogtest.NewTrackRepo()
	trackRepo.Seed(makeTrack(testUserId, "Song", "Artist", "Album"))
	_, router := buildTrackHandler(trackRepo, nil)

	rejected := serve(t, router, http.MethodPost, "/tracks/featured-backfill?offset=-1", nil)

	assertStatus(t, rejected, http.StatusBadRequest)
	accepted := serve(t, router, http.MethodPost, "/tracks/featured-backfill", nil)
	assertStatus(t, accepted, http.StatusOK)
}

// The error response carries no counts, so a run that failed after resolving
// part of the library must leave that work, and its resume point, in the log.
func TestHandleBackfillFeatured_LogsPartialWorkWhenTheRunFails(t *testing.T) {
	prev := slog.Default()
	defer slog.SetDefault(prev)
	ring := logging.Setup("info", false)

	repo := &listFailsAfterFirstPage{TrackRepo: catalogtest.NewTrackRepo()}
	repo.Seed(makeTrack(testUserId, "Song", "Artist", "Album"))
	router := backfillRouter(repo)

	rec := serve(t, router, http.MethodPost, "/tracks/featured-backfill", nil)

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

// A deezer_id the caller typed wrong used to be dropped: ?name=X&deezer_id=abc
// answered 200 about a different artist, and ?deezer_id=abc alone fell through
// to a 400 blaming the parameter the caller did send.
func TestHandleListFeaturing_RejectsAnUnreadableDeezerID(t *testing.T) {
	_, router := buildTrackHandler(catalogtest.NewTrackRepo(), nil)

	for _, query := range []string{"deezer_id=abc", "deezer_id=0", "deezer_id=-5", "name=SZA&deezer_id=abc"} {
		t.Run(query, func(t *testing.T) {
			rec := serve(t, router, http.MethodGet, "/tracks/featuring?"+query, nil)

			assertStatus(t, rec, http.StatusBadRequest)
			assertErrorCode(t, rec, "catalog.invalid_deezer_id")
		})
	}
}

// A query naming no artist at all is a different client mistake from an
// unreadable id, and carries its own code to say so.
func TestHandleListFeaturing_RequiresOneArtistKey(t *testing.T) {
	_, router := buildTrackHandler(catalogtest.NewTrackRepo(), nil)

	rec := serve(t, router, http.MethodGet, "/tracks/featuring", nil)

	assertStatus(t, rec, http.StatusBadRequest)
	assertErrorCode(t, rec, "catalog.featured_artist_key_required")
}

// The rejection must not swallow the ids that are fine: a positive deezer_id
// still reaches the lookup and returns the tracks featuring that artist.
func TestHandleListFeaturing_AcceptsAPositiveDeezerID(t *testing.T) {
	trackRepo := catalogtest.NewTrackRepo()
	track := makeTrack(testUserId, "Feature", "Artist", "Album")
	track.FeaturedArtists = []domain.FeaturedArtist{domain.NewFeaturedArtistIdentityOnly("SZA", "", 42)}
	trackRepo.Seed(track)
	_, router := buildTrackHandler(trackRepo, nil)

	rec := serve(t, router, http.MethodGet, "/tracks/featuring?deezer_id=42", nil)

	assertStatus(t, rec, http.StatusOK)
	var body struct {
		Total int `json:"total"`
	}
	decodeJSON(t, rec, &body)
	if body.Total != 1 {
		t.Errorf("total = %d, want 1 (the track featuring deezer artist 42)", body.Total)
	}
}

func backfillRouter(repo *listFailsAfterFirstPage) chi.Router {
	featured := NewFeaturedArtistHandler(
		service.NewBackfillFeaturedService(repo, repo, fakeResolver{}),
		service.NewListFeaturingService(repo),
	)
	router := chi.NewRouter()
	router.Use(auth.Middleware(verifyAsTestUser))
	router.Route("/tracks", featured.Routes)
	return router
}

// listFailsAfterFirstPage serves one page, reporting one more track than it
// returned so the run pages again, and fails that call: the run reaches the
// error having already scanned real tracks.
type listFailsAfterFirstPage struct {
	*catalogtest.TrackRepo
	served bool
}

func (r *listFailsAfterFirstPage) ListForUser(
	ctx context.Context,
	userId shared.UserId,
	limit, offset int,
) ([]*domain.Track, int, error) {
	if r.served {
		return nil, 0, errors.New("track store unreachable")
	}
	r.served = true
	tracks, _, err := r.TrackRepo.ListForUser(ctx, userId, limit, offset)
	return tracks, len(tracks) + 1, err
}
