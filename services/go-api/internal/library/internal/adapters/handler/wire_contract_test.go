package handler_test

import (
	"altune/go-api/internal/library"
	"altune/go-api/internal/library/librarytest"
	"altune/go-api/internal/shared/jsoncontract"
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
)

type wireSigner struct {
	*library.InMemoryAudioStore
}

func (wireSigner) PresignGet(_ context.Context, audioRef string, _ time.Duration) (string, error) {
	return "https://signed.example/" + audioRef + "?sig=abc", nil
}

type wireRig struct {
	router chi.Router
	tracks *library.InMemoryTrackRepo
}

func newWireRig(t *testing.T) *wireRig {
	t.Helper()
	clk := newFakeClock()
	tracks := library.NewInMemoryTrackRepo()
	playlists := library.NewInMemoryPlaylistRepo()
	playlists.Tracks = tracks
	store := library.NewInMemoryAudioStore()
	store.Seed("audio/ok.opus", []byte("opus"))
	resolver := library.InMemoryFeaturedResolver{ByTitle: map[string][]library.FeaturedArtist{"Song": {{Name: "Guest"}}}}
	deps := trackDeps{backfill: library.MustNewBackfillFeaturedService(tracks, tracks, resolver, clk.now)}
	router := newRouter()
	mountTracks(router, tracks, deps, clk.now)
	mountPlaylists(router, playlists, tracks, clk.now)
	router.Mount("/library", library.NewLibraryHandler(library.NewLibraryLensService(tracks)).Routes())
	audio := library.MustNewAudioURLHandler(library.MustNewAudioURLService(tracks, wireSigner{store}, clk.now, &librarytest.Metrics{}), clk.now)
	audio.Routes(router)
	return &wireRig{router: router, tracks: tracks}
}

func (rig *wireRig) exchange(t *testing.T, method, path, requestName string, body any, wantStatus int) []byte {
	t.Helper()
	var reader *bytes.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal %s request: %v", requestName, err)
		}
		jsoncontract.AssertWire(t, "req_"+requestName, raw)
		reader = bytes.NewReader(raw)
	}
	var rec *httptest.ResponseRecorder
	if reader == nil {
		rec = request(t, rig.router, method, path, nil)
	} else {
		rec = request(t, rig.router, method, path, reader)
	}
	if rec.Code != wantStatus {
		t.Fatalf("%s %s status = %d, want %d (body: %s)", method, path, rec.Code, wantStatus, rec.Body.String())
	}
	return rec.Body.Bytes()
}

func wireTrackBody() map[string]any {
	return map[string]any{
		"title":            "Song",
		"artist":           "Artist",
		"album":            "Album",
		"duration_seconds": 215.5,
		"artwork_url":      "https://example.com/art.jpg",
		"isrc":             "USRC17607839",
		"year":             2020,
		"genre":            "Rock",
		"album_artist":     "Artist",
		"track_number":     3,
		"featured_artists": []map[string]any{{"name": "Guest", "mbid": "5b11f4ce-a62d-471e-81fc-a69a8278c7da", "deezer_id": 42}},
		"source_url":       "https://soundcloud.com/artist/song",
		"mbid":             "9d2e8d0a-2e5b-4f2f-9c7c-4f3d5b7a1c11",
		"explicit":         false,
		"sources":          []map[string]any{{"provider": "deezer", "external_id": "12345"}},
	}
}

func (rig *wireRig) createWireTrack(t *testing.T) string {
	t.Helper()
	out := rig.exchange(t, http.MethodPost, "/tracks", "tracks_create", wireTrackBody(), http.StatusCreated)
	var created struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(out, &created); err != nil {
		t.Fatalf("decode created track: %v", err)
	}
	return created.ID
}

func (rig *wireRig) createWirePlaylist(t *testing.T) string {
	t.Helper()
	out := rig.exchange(t, http.MethodPost, "/playlists", "playlists_create", map[string]any{"name": "Road trip"}, http.StatusCreated)
	var created struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(out, &created); err != nil {
		t.Fatalf("decode created playlist: %v", err)
	}
	return created.ID
}

func TestWire_CreateTrackAnswersTheTrackResponseTheClientDecodes(t *testing.T) {
	rig := newWireRig(t)

	out := rig.exchange(t, http.MethodPost, "/tracks", "tracks_create", wireTrackBody(), http.StatusCreated)

	jsoncontract.AssertWire(t, "tracks_create", out)
}

func TestWire_ListTracksAnswersTheListEnvelopeOfTrackResponses(t *testing.T) {
	rig := newWireRig(t)
	rig.createWireTrack(t)

	out := rig.exchange(t, http.MethodGet, "/tracks", "", nil, http.StatusOK)

	jsoncontract.AssertWire(t, "tracks_list", out)
}

func TestWire_ListTracksFeaturingAnswersTheSameEnvelopeAsTheListing(t *testing.T) {
	rig := newWireRig(t)
	rig.createWireTrack(t)

	out := rig.exchange(t, http.MethodGet, "/tracks/featuring?name=Guest&mbid=5b11f4ce-a62d-471e-81fc-a69a8278c7da&deezer_id=42", "", nil, http.StatusOK)

	var listed struct {
		Items []json.RawMessage `json:"items"`
	}
	if err := json.Unmarshal(out, &listed); err != nil || len(listed.Items) != 1 {
		t.Fatalf("featuring listing = %s (%v), want one track so the fixture carries an item", out, err)
	}
	jsoncontract.AssertWire(t, "tracks_featuring", out)
}

func TestWire_FeaturedBackfillAnswersTheProgressTheClientDecodes(t *testing.T) {
	rig := newWireRig(t)
	rig.createWireTrack(t)

	out := rig.exchange(t, http.MethodPost, "/tracks/featured-backfill", "", nil, http.StatusOK)

	jsoncontract.AssertWire(t, "tracks_featured_backfill", out)
}

func TestWire_AudioURLsAnswersTheResolvedUrlsWithTheirExpiry(t *testing.T) {
	rig := newWireRig(t)
	track := librarytest.SeedReadyTrack(t, rig.tracks, librarytest.UserId(), "Ready", "Artist", "Album", "audio/ok.opus")

	out := rig.exchange(t, http.MethodPost, "/audio-urls", "audio_urls", map[string]any{"track_ids": []string{track.ID.UUID().String()}}, http.StatusOK)

	jsoncontract.AssertWire(t, "audio_urls", out)
}

func TestWire_LibraryAlbumsAnswersTheGroupedListEnvelope(t *testing.T) {
	rig := newWireRig(t)
	rig.createWireTrack(t)

	out := rig.exchange(t, http.MethodGet, "/library/albums", "", nil, http.StatusOK)

	jsoncontract.AssertWire(t, "library_albums", out)
}

func TestWire_LibraryArtistsAnswersTheGroupedListEnvelope(t *testing.T) {
	rig := newWireRig(t)
	rig.createWireTrack(t)

	out := rig.exchange(t, http.MethodGet, "/library/artists", "", nil, http.StatusOK)

	jsoncontract.AssertWire(t, "library_artists", out)
}

func TestWire_CreatePlaylistAnswersThePlaylistResponse(t *testing.T) {
	rig := newWireRig(t)

	out := rig.exchange(t, http.MethodPost, "/playlists", "playlists_create", map[string]any{"name": "Road trip"}, http.StatusCreated)

	jsoncontract.AssertWire(t, "playlists_create", out)
}

func TestWire_RenamePlaylistAnswersThePlaylistResponse(t *testing.T) {
	rig := newWireRig(t)
	playlistID := rig.createWirePlaylist(t)

	out := rig.exchange(t, http.MethodPatch, "/playlists/"+playlistID, "playlists_rename", map[string]any{"name": "Night drive"}, http.StatusOK)

	jsoncontract.AssertWire(t, "playlists_rename", out)
}

func TestWire_AddTracksToPlaylistAnswersTheAddedAndSkippedCounts(t *testing.T) {
	rig := newWireRig(t)
	playlistID := rig.createWirePlaylist(t)
	trackID := rig.createWireTrack(t)

	out := rig.exchange(t, http.MethodPost, "/playlists/"+playlistID+"/tracks/batch", "playlists_add_tracks", map[string]any{"track_ids": []string{trackID}}, http.StatusOK)

	jsoncontract.AssertWire(t, "playlists_add_tracks", out)
}

func TestWire_RemoveTracksFromPlaylistAnswersTheRemovedCount(t *testing.T) {
	rig := newWireRig(t)
	playlistID := rig.createWirePlaylist(t)
	trackID := rig.createWireTrack(t)
	rig.exchange(t, http.MethodPost, "/playlists/"+playlistID+"/tracks/batch", "playlists_add_tracks", map[string]any{"track_ids": []string{trackID}}, http.StatusOK)

	out := rig.exchange(t, http.MethodDelete, "/playlists/"+playlistID+"/tracks", "playlists_remove_tracks", map[string]any{"track_ids": []string{trackID}}, http.StatusOK)

	jsoncontract.AssertWire(t, "playlists_remove_tracks", out)
}

func TestWire_ListPlaylistsAnswersTheListEnvelopeWithPreviewArtwork(t *testing.T) {
	rig := newWireRig(t)
	playlistID := rig.createWirePlaylist(t)
	trackID := rig.createWireTrack(t)
	rig.exchange(t, http.MethodPost, "/playlists/"+playlistID+"/tracks/batch", "playlists_add_tracks", map[string]any{"track_ids": []string{trackID}}, http.StatusOK)

	out := rig.exchange(t, http.MethodGet, "/playlists", "", nil, http.StatusOK)

	jsoncontract.AssertWire(t, "playlists_list", out)
}

func TestWire_GetPlaylistAnswersTheDetailWithItsTracks(t *testing.T) {
	rig := newWireRig(t)
	playlistID := rig.createWirePlaylist(t)
	trackID := rig.createWireTrack(t)
	rig.exchange(t, http.MethodPost, "/playlists/"+playlistID+"/tracks/batch", "playlists_add_tracks", map[string]any{"track_ids": []string{trackID}}, http.StatusOK)

	out := rig.exchange(t, http.MethodGet, "/playlists/"+playlistID, "", nil, http.StatusOK)

	jsoncontract.AssertWire(t, "playlists_detail", out)
}
