package handler_test

import (
	"altune/go-api/internal/library/internal/adapters/handler"
	"altune/go-api/internal/library/internal/adapters/memory"
	"altune/go-api/internal/library/internal/service"
	"altune/go-api/internal/shared/clock"
	"altune/go-api/internal/shared/events"
	"altune/go-api/internal/shared/ids"
	"altune/go-api/internal/shared/jsoncontract"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
)

func TestWire_TrackAddedToLibraryEventCarriesTheTrackResponseShape(t *testing.T) {
	repo := memory.NewTrackRepo()
	published := &memory.RecordingPlaylistPublisher{}
	tracks := handler.MustNewTrackHandler(
		service.MustNewAddTrackService(repo, published, &memory.Scheduler{}, time.Now, ids.Real),
		service.NewListTracksService(repo),
		service.NewGetTrackStatusService(repo),
		service.NewDeleteTrackService(repo, memory.NewAudioStore(), events.NoopPublisher(), &memory.Metrics{}, memory.NewOrphanedAudioQueue(memory.NewTrackRepo(), clock.Now)),
		service.NewSetTrackNumberService(repo),
		handler.NewFeaturedArtistHandler(service.MustNewBackfillFeaturedService(repo, repo, memory.FeaturedResolver{}, time.Now), service.NewListFeaturingService(repo)), time.Now)
	router := newRouter()
	router.Route("/tracks", func(r chi.Router) { r.Mount("/", tracks.Routes()) })

	rec := request(t, router, http.MethodPost, "/tracks", strings.NewReader(`{"title":"Paranoid Android","artist":"Radiohead","album":"OK Computer","duration_seconds":383.5}`))
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d body %q, want 201", rec.Code, rec.Body.String())
	}

	payload := published.Last(events.TypeTrackAddedToLibrary)
	if payload == nil {
		t.Fatal("no track_added_to_library event was published")
	}
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	jsoncontract.AssertWire(t, "events_track_added_to_library", body)
}
