package providers

import (
	"strings"
	"testing"
	"time"

	"altune/go-api/internal/discovery/domain"
)

// Pins the trivially-stable adapter contract surface the fan-out wires on:
// provider names, per-provider search budgets, supported kinds, and the
// artwork-source tags the coverage telemetry keys by.

func TestAdapterNames(t *testing.T) {
	tests := []struct {
		got, want domain.ProviderName
	}{
		{(&DeezerAdapter{}).Name(), domain.ProviderDeezer},
		{(&LastFmAdapter{}).Name(), domain.ProviderLastFM},
		{(&MusicBrainzAdapter{}).Name(), domain.ProviderMusicBrainz},
		{(&ITunesAdapter{}).Name(), domain.ProviderITunes},
	}
	for _, tt := range tests {
		if tt.got != tt.want {
			t.Errorf("Name() = %v, want %v", tt.got, tt.want)
		}
	}
}

func TestAdapterSearchTimeouts(t *testing.T) {
	tests := []struct {
		name string
		got  time.Duration
		want time.Duration
	}{
		{"musicbrainz", (&MusicBrainzAdapter{}).SearchTimeout(), 5 * time.Second},
		{"lastfm", (&LastFmAdapter{}).SearchTimeout(), 4 * time.Second},
		{"itunes", (&ITunesAdapter{}).SearchTimeout(), 4 * time.Second},
		{"spotify", (&SpotifyAdapter{}).SearchTimeout(), spotifySearchTimeout},
		{"applemusic", (&AppleMusicAdapter{}).SearchTimeout(), appleMusicSearchTimeout},
	}
	for _, tt := range tests {
		if tt.got != tt.want {
			t.Errorf("%s SearchTimeout = %v, want %v", tt.name, tt.got, tt.want)
		}
	}
}

func TestAdapterSupportedKinds_allThree(t *testing.T) {
	for name, kinds := range map[string]map[domain.ResultKind]bool{
		"musicbrainz": (&MusicBrainzAdapter{}).SupportedKinds(),
		"spotify":     (&SpotifyAdapter{}).SupportedKinds(),
		"applemusic":  (&AppleMusicAdapter{}).SupportedKinds(),
		"deezer":      (&DeezerAdapter{}).SupportedKinds(),
	} {
		if !kinds[domain.ResultKindTrack] || !kinds[domain.ResultKindAlbum] || !kinds[domain.ResultKindArtist] {
			t.Errorf("%s SupportedKinds = %v, want track+album+artist", name, kinds)
		}
	}
}

func TestArtworkSourceTags(t *testing.T) {
	tests := []struct {
		got, want string
	}{
		{(&DeezerAdapter{}).ArtworkSource(), "deezer"},
		{(&ITunesAdapter{}).ArtworkSource(), "itunes"},
		{(&SpotifyAdapter{}).ArtworkSource(), "spotify"},
		{(&SpotifyArtworkResolver{}).ArtworkSource(), "spotify"},
		{(&AppleMusicAdapter{}).ArtworkSource(), "applemusic"},
		{(&FanartTvArtworkResolver{}).ArtworkSource(), "fanart"},
		{(&GeniusArtworkResolver{}).ArtworkSource(), "genius"},
		{(&SoundCloudAPIAdapter{}).ArtworkSource(), "soundcloud"},
	}
	for _, tt := range tests {
		if tt.got != tt.want {
			t.Errorf("ArtworkSource = %q, want %q", tt.got, tt.want)
		}
	}
}

func TestLastFmAPIError_Error(t *testing.T) {
	err := &lastfmAPIError{Code: 29, Message: "Rate limit exceeded"}
	if !strings.Contains(err.Error(), "29") || !strings.Contains(err.Error(), "Rate limit exceeded") {
		t.Errorf("Error() = %q, want code and message surfaced", err.Error())
	}
}
