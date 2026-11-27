package handler_test

import (
	"altune/go-api/internal/playback"
	"altune/go-api/internal/playback/internal/core/domain"
	"altune/go-api/internal/shared/ids"
	"altune/go-api/internal/shared/jsoncontract"
	"altune/go-api/internal/shared/requestuser"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

const (
	wireTrackA = "00000000-0000-4000-8000-000000000001"
	wireTrackB = "00000000-0000-4000-8000-000000000002"
)

type artworkNowPlaying struct{}

func (artworkNowPlaying) Lookup(_ context.Context, _ ids.UserId, trackId string) (*playback.NowPlayingTrack, error) {
	artwork := "https://cdn.example.com/art/ok-computer.jpg"
	duration := 233.5
	return &playback.NowPlayingTrack{
		Id:                trackId,
		Title:             "Paranoid Android",
		Artist:            "Radiohead",
		ArtworkURL:        &artwork,
		DurationSeconds:   &duration,
		AcquisitionStatus: "ready",
	}, nil
}

func wireGet(t *testing.T, sourceId string, nowPlaying nowPlayingReader) []byte {
	t.Helper()
	repo := &recordingRepo{saved: &playback.QueueState{
		TrackIds:     []string{wireTrackA, wireTrackB},
		CurrentIdx:   1,
		PositionMs:   42000,
		Shuffled:     true,
		SourceId:     sourceId,
		NaturalOrder: []string{wireTrackB, wireTrackA},
	}}
	h := playback.MustNewQueueHandler(playback.MustNewQueueService(repo, nowPlaying, time.Now), playback.DefaultQueueStateRateLimit, time.Now, noopRateLimitMetrics{})
	req := httptest.NewRequest(http.MethodGet, "/queue-state", nil)
	req = req.WithContext(requestuser.ContextWithUserID(req.Context(), ids.NewUserId(uuid.New())))
	rec := httptest.NewRecorder()

	h.Routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body %q, want 200", rec.Code, rec.Body.String())
	}
	return rec.Body.Bytes()
}

func TestWire_QueueStateResponses(t *testing.T) {
	cases := []struct {
		fixture  string
		sourceId string
	}{
		{"playback_queue_state_playlist", "playlist:5d1c3f0e-6f4e-4f55-9a4e-0d6a2c0c9b11:Road+trip"},
		{"playback_queue_state_library", "library"},
		{"playback_queue_state_search", "search:radiohead"},
		{"playback_queue_state_nosource", ""},
	}
	for _, tc := range cases {
		t.Run(tc.fixture, func(t *testing.T) {
			jsoncontract.AssertWire(t, tc.fixture, wireGet(t, tc.sourceId, artworkNowPlaying{}))
		})
	}
}

type bareNowPlaying struct{}

func (bareNowPlaying) Lookup(_ context.Context, _ ids.UserId, trackId string) (*playback.NowPlayingTrack, error) {
	return &playback.NowPlayingTrack{Id: trackId, Title: "Untitled", Artist: "Unknown", AcquisitionStatus: "pending"}, nil
}

func TestWire_QueueStateWithACurrentTrackWithoutArtworkOrDuration(t *testing.T) {
	jsoncontract.AssertWire(t, "playback_queue_state_track_bare", wireGet(t, "library", bareNowPlaying{}))
}

func TestWire_QueueStateWithAnUnavailableCurrentTrack(t *testing.T) {
	jsoncontract.AssertWire(t, "playback_queue_state_track_unavailable", wireGet(t, "library", failingNowPlaying{}))
}

func TestWire_QueueSourceKinds(t *testing.T) {
	kinds, err := json.Marshal([]string{domain.SourceKindLibrary, domain.SourceKindPlaylist, domain.SourceKindSearch})
	if err != nil {
		t.Fatal(err)
	}

	jsoncontract.AssertWireExact(t, "playback_queue_source_kinds", kinds)
}

func TestWire_SaveQueueStateRequestIsAccepted(t *testing.T) {
	body := `{
  "track_ids": ["` + wireTrackA + `", "` + wireTrackB + `"],
  "current_index": 1,
  "position_ms": 42000,
  "shuffled": true,
  "repeat_mode": "all",
  "source": {"kind": "playlist", "playlist_id": "5d1c3f0e-6f4e-4f55-9a4e-0d6a2c0c9b11", "name": "Road trip"},
  "natural_order": ["` + wireTrackB + `", "` + wireTrackA + `"]
}`
	repo := &recordingRepo{}
	h := newHandler(repo)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/queue-state", strings.NewReader(body))
	req = req.WithContext(requestuser.ContextWithUserID(req.Context(), ids.NewUserId(uuid.New())))

	h.Routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d body %q, want 204", rec.Code, rec.Body.String())
	}
	if repo.saved == nil || repo.saved.CurrentIdx != 1 || !repo.saved.Shuffled {
		t.Fatalf("saved = %+v, want the request fields persisted", repo.saved)
	}
	jsoncontract.AssertWire(t, "req_playback_queue_state", []byte(body))
}
