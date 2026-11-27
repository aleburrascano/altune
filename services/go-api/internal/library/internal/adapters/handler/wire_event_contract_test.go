package handler_test

import (
	"altune/go-api/internal/library"
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
	repo := library.NewInMemoryTrackRepo()
	published := &library.InMemoryRecordingPublisher{}
	tracks := library.MustNewTrackHandler(
		library.MustNewAddTrackService(repo, published, &library.InMemoryScheduler{}, time.Now, ids.Real),
		library.NewListTracksService(repo),
		library.NewGetTrackStatusService(repo),
		library.NewDeleteTrackService(repo, library.NewInMemoryAudioStore(), events.NoopPublisher(), &library.InMemoryAudioStoreMetrics{}, library.NewInMemoryOrphanedAudioQueue(library.NewInMemoryTrackRepo(), clock.Now)),
		library.NewSetTrackNumberService(repo),
		library.NewFeaturedArtistHandler(library.MustNewBackfillFeaturedService(repo, repo, library.InMemoryFeaturedResolver{}, time.Now), library.NewListFeaturingService(repo)), time.Now)
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
