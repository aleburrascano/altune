package service

import (
	"encoding/json"
	"testing"

	"github.com/google/uuid"

	"altune/go-api/internal/catalog/domain"
	"altune/go-api/internal/shared"
)

func TestTrackAddedPayload(t *testing.T) {
	userId := shared.NewUserId(uuid.New())
	track, err := domain.NewTrack(userId, "Midnight City", "M83", "Hurry Up, We're Dreaming")
	if err != nil {
		t.Fatalf("new track: %v", err)
	}

	p := trackAddedPayload(track)

	if p["id"] != track.ID.String() {
		t.Errorf("id = %v, want %s", p["id"], track.ID.String())
	}
	if p["track_id"] != track.ID.String() {
		t.Errorf("track_id (legacy) = %v, want %s", p["track_id"], track.ID.String())
	}
	if p["title"] != "Midnight City" || p["artist"] != "M83" {
		t.Errorf("title/artist = %v/%v", p["title"], p["artist"])
	}
	if album, ok := p["album"].(string); !ok || album != "Hurry Up, We're Dreaming" {
		t.Errorf("album = %v, want the album string", p["album"])
	}
	if p["acquisition_status"] != track.AcquisitionStatus.String() {
		t.Errorf("acquisition_status = %v", p["acquisition_status"])
	}
}

func TestTrackAddedPayload_EmptyAlbumIsNil(t *testing.T) {
	track := &domain.Track{
		ID:     domain.NewTrackId(),
		UserId: shared.NewUserId(uuid.New()),
		Title:  "Single",
		Artist: "Artist",
		Album:  "",
	}

	if album := trackAddedPayload(track)["album"]; album != nil {
		t.Errorf("empty album = %v, want JSON null", album)
	}
}

func TestTrackAddedPayload_SingleCarriesTheTitleAsAlbum(t *testing.T) {
	userId := shared.NewUserId(uuid.New())
	track, err := domain.NewTrack(userId, "Single", "Artist", "")
	if err != nil {
		t.Fatalf("new track: %v", err)
	}

	if album := trackAddedPayload(track)["album"]; album != "Single" {
		t.Errorf("album = %v, want the title", album)
	}
}

func TestTrackAddedPayload_MatchesRESTDTOByteForByte(t *testing.T) {
	userId := shared.NewUserId(uuid.New())
	track, err := domain.NewTrack(userId, "Midnight City", "M83", "Hurry Up, We're Dreaming")
	if err != nil {
		t.Fatalf("new track: %v", err)
	}
	dur := 240.5
	artwork := "https://cdn.example/art.jpg"
	year := 2011
	genre := "electronic"
	trackNo := 4
	albumArtist := "M83"
	isrc := "USUM71100001"
	audioRef := "obj/abc123"
	reason := "no_source"
	track.DurationSeconds = &dur
	track.ArtworkURL = &artwork
	track.Year = &year
	track.Genre = &genre
	track.TrackNumber = &trackNo
	track.AlbumArtist = &albumArtist
	track.ISRC = &isrc
	track.AudioRef = &audioRef
	track.FailureReason = &reason
	track.AcquisitionStatus = domain.AcquisitionFailed
	track.FeaturedArtists = []domain.FeaturedArtist{
		domain.NewFeaturedArtistIdentityOnly("Susanne Sundfor", "11111111-2222-3333-4444-555555555555", 4567),
	}

	restJSON := canonicalJSON(t, mustMarshal(t, TrackToDTO(track)))

	payload := trackAddedPayload(track)
	if payload["track_id"] != track.ID.String() {
		t.Errorf("track_id = %v, want %s", payload["track_id"], track.ID.String())
	}
	delete(payload, "track_id")
	sseJSON := canonicalJSON(t, mustMarshal(t, payload))

	if restJSON != sseJSON {
		t.Errorf("SSE payload diverged from REST DTO\n REST: %s\n  SSE: %s", restJSON, sseJSON)
	}
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
		t.Fatalf("unmarshal: %v", err)
	}
	out, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return string(out)
}
