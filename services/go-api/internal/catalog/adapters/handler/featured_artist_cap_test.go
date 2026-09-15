package handler

import (
	"altune/go-api/internal/catalog/catalogtest"
	"altune/go-api/internal/catalog/service"
	"fmt"
	"net/http"
	"strings"
	"testing"
)

func featuredDTOs(n int) []service.FeaturedArtistDTO {
	out := make([]service.FeaturedArtistDTO, n)
	for i := range out {
		out[i] = service.FeaturedArtistDTO{Name: fmt.Sprintf("Guest %d", i)}
	}
	return out
}

// Each featured artist costs two round trips inside the add transaction, so an
// oversized list must be refused at the boundary before anything is stored or
// scheduled.
func TestHandleCreateTrack_RejectsTooManyFeaturedArtists(t *testing.T) {
	repo := catalogtest.NewTrackRepo()
	sched := &catalogtest.Scheduler{}
	_, router := buildTrackHandler(repo, sched)
	body := CreateTrackRequest{Title: "Posse Cut", Artist: "Everyone", FeaturedArtists: featuredDTOs(service.MaxFeaturedArtistsPerTrack + 1)}

	rec := serve(t, router, http.MethodPost, "/tracks", jsonBody(t, body))

	assertStatus(t, rec, http.StatusBadRequest)
	if !strings.Contains(rec.Body.String(), "featured_artists") {
		t.Errorf("body = %s, want the featured_artists validation message", rec.Body.String())
	}
	if len(sched.SourceURLs) != 0 || len(repo.Tracks) != 0 {
		t.Errorf("scheduled %v and stored %d tracks, want neither", sched.SourceURLs, len(repo.Tracks))
	}
}

// The largest real credit lists (a charity single like "We Are The World"
// carries 40 Deezer contributors) sit well under the cap and must still save.
func TestHandleCreateTrack_AcceptsFeaturedArtistsAtCap(t *testing.T) {
	repo := catalogtest.NewTrackRepo()
	_, router := buildTrackHandler(repo, &catalogtest.Scheduler{})
	body := CreateTrackRequest{Title: "Posse Cut", Artist: "Everyone", FeaturedArtists: featuredDTOs(service.MaxFeaturedArtistsPerTrack)}

	rec := serve(t, router, http.MethodPost, "/tracks", jsonBody(t, body))

	assertStatus(t, rec, http.StatusCreated)
	for _, tr := range repo.Tracks {
		if got := len(tr.FeaturedArtists); got != service.MaxFeaturedArtistsPerTrack {
			t.Errorf("stored %d featured artists, want %d", got, service.MaxFeaturedArtistsPerTrack)
		}
	}
}
