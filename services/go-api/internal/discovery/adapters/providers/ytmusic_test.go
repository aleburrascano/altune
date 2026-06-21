package providers

import (
	"testing"

	"altune/go-api/internal/discovery/domain"

	"github.com/raitonoberu/ytmusic"
)

func TestMapYTMusicVideo(t *testing.T) {
	v := &ytmusic.VideoItem{
		VideoID:  "abc123",
		Title:    "Obscure Track",
		Artists:  []ytmusic.Artist{{Name: "Underground Artist"}},
		Duration: 200,
		Thumbnails: []ytmusic.Thumbnail{
			{URL: "https://img/small"},
			{URL: "https://img/large"},
		},
	}

	got := mapYTMusicVideo(v)

	if got.Kind != domain.ResultKindTrack {
		t.Errorf("Kind = %v, want track", got.Kind)
	}
	if got.Title != "Obscure Track" {
		t.Errorf("Title = %q, want %q", got.Title, "Obscure Track")
	}
	if got.Subtitle != "Underground Artist" {
		t.Errorf("Subtitle = %q, want %q", got.Subtitle, "Underground Artist")
	}
	if got.ImageURL != "https://img/large" {
		t.Errorf("ImageURL = %q, want the largest thumbnail", got.ImageURL)
	}
	if len(got.Sources) != 1 {
		t.Fatalf("Sources = %d, want 1", len(got.Sources))
	}
	src := got.Sources[0]
	if src.Provider != domain.ProviderYouTube || src.ExternalID != "abc123" {
		t.Errorf("source = %+v, want youtube/abc123", src)
	}
	if src.URL != "https://music.youtube.com/watch?v=abc123" {
		t.Errorf("source URL = %q", src.URL)
	}
	if got.Extras["duration"] != 200 {
		t.Errorf("duration extra = %v, want 200", got.Extras["duration"])
	}
}
