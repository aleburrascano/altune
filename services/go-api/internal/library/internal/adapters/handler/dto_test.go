package handler_test

import (
	"altune/go-api/internal/library/internal/adapters/handler"
	"altune/go-api/internal/library/internal/adapters/memory"
	"altune/go-api/internal/library/internal/domain"
	"altune/go-api/internal/library/internal/service"
	"altune/go-api/internal/library/librarytest"
	"altune/go-api/internal/shared/events"
	"altune/go-api/internal/shared/ids"
	"altune/go-api/internal/shared/requestuser"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func dtoTrackRoutes(addTrack *service.AddTrackService, repo *memory.TrackRepo) http.Handler {
	return handler.MustNewTrackHandler(
		addTrack,
		service.NewListTracksService(repo),
		nil, nil, nil,
		handler.NewFeaturedArtistHandler(nil, nil), time.Now).Routes()
}

func serveAs(routes http.Handler, userId ids.UserId, method, body string) (int, []byte) {
	req := httptest.NewRequest(method, "/", strings.NewReader(body))
	req = req.WithContext(requestuser.ContextWithUserID(req.Context(), userId))
	rec := httptest.NewRecorder()
	routes.ServeHTTP(rec, req)
	return rec.Code, rec.Body.Bytes()
}

func postTrack(t *testing.T, addTrack *service.AddTrackService, userId ids.UserId, body string) (int, []byte) {
	t.Helper()
	return serveAs(dtoTrackRoutes(addTrack, memory.NewTrackRepo()), userId, http.MethodPost, body)
}

func listTracksBody(t *testing.T, repo *memory.TrackRepo, userId ids.UserId) string {
	t.Helper()
	status, body := serveAs(dtoTrackRoutes(service.MustNewAddTrackService(repo, handler.NewWireEvents(events.NoopPublisher()), &memory.Scheduler{}, time.Now, ids.Real), repo), userId, http.MethodGet, "")
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
	repo := memory.NewTrackRepo()
	userId := librarytest.UserId()
	audioRef := userId.String() + "/m83/hurry-up/midnight-city.opus"
	librarytest.SeedReadyTrack(t, repo, userId, "Midnight City", "M83", "Hurry Up, We're Dreaming", audioRef)

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
	repo := memory.NewTrackRepo()
	userId := librarytest.UserId()
	stored := librarytest.RestoreTrack(librarytest.TrackSnapshot{ID: librarytest.NewTrackId(), UserId: userId, Title: "Single", Artist: "Artist", Album: ""})
	if _, _, err := repo.Add(context.Background(), stored); err != nil {
		t.Fatalf("Add: %v", err)
	}

	body := listTracksBody(t, repo, userId)

	if !strings.Contains(body, `"album":null`) {
		t.Errorf("GET /tracks = %s, want an empty album serialized as JSON null", body)
	}
}

func TestTrackToDTO_IDMarshalsAsCanonicalUUID(t *testing.T) {
	repo := memory.NewTrackRepo()
	userId := librarytest.UserId()
	track := librarytest.SeedTrack(t, repo, userId, "Midnight City", "M83", "")

	body := listTracksBody(t, repo, userId)

	want := `"id":"` + track.ID().UUID().String() + `"`
	if !strings.Contains(body, want) {
		t.Errorf("TrackDTO JSON = %s, want it to contain %s", body, want)
	}
}

func addWithRecordedEvents(t *testing.T, input service.AddTrackInput) (*domain.Track, map[string]any) {
	t.Helper()
	pub := &memory.RecordingPlaylistPublisher{}
	svc := service.MustNewAddTrackService(memory.NewTrackRepo(), handler.NewWireEvents(pub), &memory.Scheduler{}, time.Now, ids.Real)
	out, err := svc.Execute(context.Background(), librarytest.UserId(), input)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	payload := pub.LastFields(events.TypeTrackAddedToLibrary)
	if payload == nil {
		t.Fatal("no track_added_to_library event published")
	}
	return out.Track, payload
}

func TestTrackAddedPayload(t *testing.T) {
	track, p := addWithRecordedEvents(t, service.AddTrackInput{
		Title: "Midnight City", Artist: "M83", Album: "Hurry Up, We're Dreaming",
	})

	if p["id"] != track.ID().String() {
		t.Errorf("id = %v, want %s", p["id"], track.ID().String())
	}
	if p["track_id"] != track.ID().String() {
		t.Errorf("track_id (legacy) = %v, want %s", p["track_id"], track.ID().String())
	}
	if p["title"] != "Midnight City" || p["artist"] != "M83" {
		t.Errorf("title/artist = %v/%v", p["title"], p["artist"])
	}
	if album, ok := p["album"].(string); !ok || album != "Hurry Up, We're Dreaming" {
		t.Errorf("album = %v, want the album string", p["album"])
	}
	if p["acquisition_status"] != track.DownloadStatus().String() {
		t.Errorf("acquisition_status = %v", p["acquisition_status"])
	}
	if _, ok := p["audio_ref"]; ok {
		t.Error("track_added payload exposes audio_ref")
	}
}

func TestTrackAddedPayload_SingleCarriesTheTitleAsAlbum(t *testing.T) {
	_, p := addWithRecordedEvents(t, service.AddTrackInput{Title: "Single", Artist: "Artist"})

	if album := p["album"]; album != "Single" {
		t.Errorf("album = %v, want the title", album)
	}
}

func TestTrackAddedPayload_MatchesRESTDTOByteForByte(t *testing.T) {
	pub := &memory.RecordingPlaylistPublisher{}
	svc := service.MustNewAddTrackService(memory.NewTrackRepo(), handler.NewWireEvents(pub), &memory.Scheduler{}, time.Now, ids.Real)

	status, restBody := postTrack(t, svc, librarytest.UserId(), `{
		"title": "Midnight City", "artist": "M83", "album": "Hurry Up, We're Dreaming",
		"duration_seconds": 240.5, "artwork_url": "https://cdn.example/art.jpg", "year": 2011,
		"genre": "electronic", "track_number": 4, "album_artist": "M83", "isrc": "USUM71100001",
		"featured_artists": [{"name": "Susanne Sundfor", "mbid": "11111111-2222-3333-4444-555555555555", "deezer_id": 4567}]
	}`)
	if status != 201 {
		t.Fatalf("POST /tracks status = %d, body %s", status, restBody)
	}

	payload := pub.LastFields(events.TypeTrackAddedToLibrary)
	if payload == nil || payload["track_id"] != payload["id"] {
		t.Fatalf("payload = %v, want track_id equal to id", payload)
	}
	delete(payload, "track_id")
	restJSON := canonicalJSON(t, restBody)
	if sseJSON := canonicalJSON(t, mustMarshal(t, payload)); restJSON != sseJSON {
		t.Errorf("SSE payload diverged from REST DTO\n REST: %s\n  SSE: %s", restJSON, sseJSON)
	}
	if !strings.Contains(restJSON, `"featured_artists":[{`) || !strings.Contains(restJSON, `"isrc":"USUM71100001"`) {
		t.Errorf("REST DTO = %s, want every saved field present so the comparison covers them", restJSON)
	}
}
