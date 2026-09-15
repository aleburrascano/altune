package handler

import (
	"altune/go-api/internal/catalog/catalogtest"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	catdomain "altune/go-api/internal/catalog/domain"
)

// JSON cannot spell Infinity or NaN, and encoding/json already refuses a
// literal that overflows float64 (1e400). The reachable path to a non-finite
// value is a finite but huge duration: it is stored as-is, and summing two of
// them in a playlist's total_duration_seconds yields +Inf, which encoding/json
// cannot marshal, so the detail response is committed as 200 with a truncated
// body. Every such duration must be refused at the boundary with a 400.
func TestHandleCreateTrack_RejectsUnencodableDuration(t *testing.T) {
	for _, raw := range []string{"1e400", "-1e400", "1.7976931348623157e308", "1e300", "604801"} {
		t.Run(raw, func(t *testing.T) {
			repo := catalogtest.NewTrackRepo()
			_, router := buildTrackHandler(repo, &catalogtest.Scheduler{})

			rec := serve(t, router, http.MethodPost, "/tracks", createTrackWithRawDuration(raw))

			assertStatus(t, rec, http.StatusBadRequest)
			if len(repo.Tracks) != 0 {
				t.Errorf("stored %d tracks, want none", len(repo.Tracks))
			}
		})
	}
}

// End to end through both handlers: whatever the create endpoint accepts must
// still encode as a playlist detail once summed.
func TestHandleGetPlaylist_EncodesAfterHugeDurationsSubmitted(t *testing.T) {
	trRepo := catalogtest.NewTrackRepo()
	_, trackRouter := buildTrackHandler(trRepo, &catalogtest.Scheduler{})
	for _, title := range []string{"Dreams", "Rhiannon"} {
		body := `{"title":"` + title + `","artist":"Fleetwood Mac","duration_seconds":1.7976931348623157e308}`
		serve(t, trackRouter, http.MethodPost, "/tracks", strings.NewReader(body))
	}
	stored := make([]*catdomain.Track, 0, len(trRepo.Tracks))
	for _, track := range trRepo.Tracks {
		stored = append(stored, track)
	}
	plRepo := catalogtest.NewPlaylistRepo()
	pl, err := catdomain.NewPlaylist(testUserId, "Long", time.Now())
	if err != nil {
		t.Fatalf("new playlist: %v", err)
	}
	plRepo.SeedWithTracks(pl, stored)
	_, playlistRouter := buildPlaylistHandler(plRepo, trRepo)

	rec := serve(t, playlistRouter, http.MethodGet, "/playlists/"+pl.ID.UUID().String(), nil)

	assertStatus(t, rec, http.StatusOK)
	var resp PlaylistDetailResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("playlist detail is not valid JSON (%v): %q", err, rec.Body.String())
	}
}

func TestHandleCreateTrack_AcceptsMaxDuration(t *testing.T) {
	repo := catalogtest.NewTrackRepo()
	_, router := buildTrackHandler(repo, &catalogtest.Scheduler{})

	rec := serve(t, router, http.MethodPost, "/tracks", createTrackWithRawDuration("604800"))

	assertStatus(t, rec, http.StatusCreated)
}

func createTrackWithRawDuration(raw string) *strings.Reader {
	return strings.NewReader(`{"title":"Dreams","artist":"Fleetwood Mac","duration_seconds":` + raw + `}`)
}
