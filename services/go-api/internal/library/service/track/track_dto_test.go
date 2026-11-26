package track_test

import (
	"altune/go-api/internal/library"
	"altune/go-api/internal/shared/ids"
	"altune/go-api/internal/shared/requestuser"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func trackRoutes(addTrack *library.AddTrackService, repo *library.InMemoryTrackRepo) http.Handler {
	return library.NewTrackHandler(
		addTrack,
		library.NewListTracksService(repo),
		nil, nil, nil,
		library.NewFeaturedArtistHandler(nil, nil),
	).Routes()
}

func serveAs(routes http.Handler, userId ids.UserId, method, body string) (int, []byte) {
	req := httptest.NewRequest(method, "/", strings.NewReader(body))
	req = req.WithContext(requestuser.ContextWithUserID(req.Context(), userId))
	rec := httptest.NewRecorder()
	routes.ServeHTTP(rec, req)
	return rec.Code, rec.Body.Bytes()
}

func postTrack(t *testing.T, addTrack *library.AddTrackService, userId ids.UserId, body string) (int, []byte) {
	t.Helper()
	return serveAs(trackRoutes(addTrack, library.NewInMemoryTrackRepo()), userId, http.MethodPost, body)
}

func listTracksBody(t *testing.T, repo *library.InMemoryTrackRepo, userId ids.UserId) string {
	t.Helper()
	status, body := serveAs(trackRoutes(library.NewAddTrackService(repo), repo), userId, http.MethodGet, "")
	if status != http.StatusOK {
		t.Fatalf("GET /tracks status = %d, body %s", status, body)
	}
	return string(body)
}

func mustMarshal(t *testing.T, v any) []byte {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return raw
}

func canonicalJSON(t *testing.T, raw []byte) string {
	t.Helper()
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatalf("unmarshal %s: %v", raw, err)
	}
	return string(mustMarshal(t, v))
}

func TestTrackToDTO_DoesNotSerializeAudioRef(t *testing.T) {
	repo := library.NewInMemoryTrackRepo()
	userId := library.FixtureUserId()
	audioRef := userId.String() + "/m83/hurry-up/midnight-city.opus"
	library.SeedReadyTrack(t, repo, userId, "Midnight City", "M83", "Hurry Up, We're Dreaming", audioRef)

	body := listTracksBody(t, repo, userId)

	if !strings.Contains(body, "Midnight City") {
		t.Fatalf("GET /tracks = %s, want the seeded track listed", body)
	}
	if strings.Contains(body, "audio_ref") || strings.Contains(body, "midnight-city.opus") {
		t.Errorf("TrackDTO JSON exposes the audio ref: %s", body)
	}
	if strings.Contains(body, userId.String()) {
		t.Errorf("TrackDTO JSON leaks the owner's user id: %s", body)
	}
}

func TestTrackToDTO_EmptyAlbumIsNull(t *testing.T) {
	repo := library.NewInMemoryTrackRepo()
	userId := library.FixtureUserId()
	stored := &library.Track{ID: library.NewTrackId(), UserId: userId, Title: "Single", Artist: "Artist", Album: ""}
	if _, _, err := repo.Add(context.Background(), stored); err != nil {
		t.Fatalf("Add: %v", err)
	}

	body := listTracksBody(t, repo, userId)

	if !strings.Contains(body, `"album":null`) {
		t.Errorf("GET /tracks = %s, want an empty album serialized as JSON null", body)
	}
}

func TestTrackToDTO_IDMarshalsAsCanonicalUUID(t *testing.T) {
	repo := library.NewInMemoryTrackRepo()
	userId := library.FixtureUserId()
	track := library.SeedTrack(t, repo, userId, "Midnight City", "M83", "")

	body := listTracksBody(t, repo, userId)

	want := `"id":"` + track.ID.UUID().String() + `"`
	if !strings.Contains(body, want) {
		t.Errorf("TrackDTO JSON = %s, want it to contain %s", body, want)
	}
}
