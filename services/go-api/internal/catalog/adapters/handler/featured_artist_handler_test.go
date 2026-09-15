package handler

import (
	"altune/go-api/internal/catalog/catalogtest"
	"net/http"
	"testing"
)

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
