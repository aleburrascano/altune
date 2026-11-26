package domain_test

import (
	"altune/go-api/internal/search/domain"
	"testing"
)

func TestParseResultKind(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    domain.ResultKind
		wantErr bool
	}{
		{name: "artist", input: "artist", want: domain.ResultKindArtist},
		{name: "album", input: "album", want: domain.ResultKindAlbum},
		{name: "track", input: "track", want: domain.ResultKindTrack},
		{name: "playlist", input: "playlist", want: domain.ResultKindPlaylist},
		{name: "invalid", input: "podcast", wantErr: true},
		{name: "empty", input: "", wantErr: true},
		{name: "uppercase rejected", input: "Artist", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := domain.ParseResultKind(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("ParseResultKind(%q) expected error, got %v", tt.input, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseResultKind(%q) unexpected error: %v", tt.input, err)
			}
			if got != tt.want {
				t.Errorf("ParseResultKind(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestResultKind_String(t *testing.T) {
	tests := []struct {
		kind domain.ResultKind
		want string
	}{
		{domain.ResultKindUnknown, "unknown"},
		{domain.ResultKindArtist, "artist"},
		{domain.ResultKindAlbum, "album"},
		{domain.ResultKindTrack, "track"},
		{domain.ResultKindPlaylist, "playlist"},
		{domain.ResultKind(999), "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			got := tt.kind.String()
			if got != tt.want {
				t.Errorf("ResultKind(%d).String() = %q, want %q", tt.kind, got, tt.want)
			}
		})
	}
}

func TestConfidence_String(t *testing.T) {
	tests := []struct {
		conf domain.Confidence
		want string
	}{
		{domain.ConfidenceHigh, "high"},
		{domain.ConfidenceMedium, "medium"},
		{domain.ConfidenceLow, "low"},
		{domain.Confidence(999), "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			got := tt.conf.String()
			if got != tt.want {
				t.Errorf("Confidence(%d).String() = %q, want %q", tt.conf, got, tt.want)
			}
		})
	}
}

func TestEntityResolutionTier_String(t *testing.T) {
	tests := []struct {
		tier domain.EntityResolutionTier
		want string
	}{
		{domain.EntityResolutionMBID, "mbid"},
		{domain.EntityResolutionISRC, "isrc"},
		{domain.EntityResolutionUPC, "upc"},
		{domain.EntityResolutionBridge, "bridge"},
		{domain.EntityResolutionNone, "none"},
		{domain.EntityResolutionTier(999), "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			got := tt.tier.String()
			if got != tt.want {
				t.Errorf("EntityResolutionTier(%d).String() = %q, want %q", tt.tier, got, tt.want)
			}
		})
	}
}

func TestIsCanonicalContentProvider(t *testing.T) {
	t.Parallel()
	if domain.CanonicalContentProvider != domain.ProviderDeezer {
		t.Fatalf("CanonicalContentProvider = %v, want %v", domain.CanonicalContentProvider, domain.ProviderDeezer)
	}
	for p := domain.ProviderUnknown; p <= domain.ProviderSpotify; p++ {
		want := p == domain.ProviderDeezer
		if got := domain.IsCanonicalContentProvider(p); got != want {
			t.Errorf("IsCanonicalContentProvider(%v) = %v, want %v", p, got, want)
		}
	}
}

func TestProviderName_String(t *testing.T) {
	tests := []struct {
		provider domain.ProviderName
		want     string
	}{
		{domain.ProviderUnknown, "unknown"},
		{domain.ProviderDeezer, "deezer"},
		{domain.ProviderMusicBrainz, "musicbrainz"},
		{domain.ProviderSoundCloud, "soundcloud"},
		{domain.ProviderITunes, "itunes"},
		{domain.ProviderTheAudioDB, "theaudiodb"},
		{domain.ProviderDiscogs, "discogs"},
		{domain.ProviderYouTube, "youtube"},
		{domain.ProviderAppleMusic, "applemusic"},
		{domain.ProviderSpotify, "spotify"},
		{domain.ProviderName(999), "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			got := tt.provider.String()
			if got != tt.want {
				t.Errorf("ProviderName(%d).String() = %q, want %q", tt.provider, got, tt.want)
			}
		})
	}
}

func TestParseProviderName(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    domain.ProviderName
		wantErr bool
	}{
		{name: "deezer", input: "deezer", want: domain.ProviderDeezer},
		{name: "musicbrainz", input: "musicbrainz", want: domain.ProviderMusicBrainz},
		{name: "soundcloud", input: "soundcloud", want: domain.ProviderSoundCloud},
		{name: "itunes", input: "itunes", want: domain.ProviderITunes},
		{name: "theaudiodb", input: "theaudiodb", want: domain.ProviderTheAudioDB},
		{name: "discogs", input: "discogs", want: domain.ProviderDiscogs},
		{name: "youtube", input: "youtube", want: domain.ProviderYouTube},
		{name: "applemusic", input: "applemusic", want: domain.ProviderAppleMusic},
		{name: "spotify", input: "spotify", want: domain.ProviderSpotify},
		{name: "invalid", input: "napster", wantErr: true},
		{name: "empty", input: "", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := domain.ParseProviderName(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("ParseProviderName(%q) expected error, got %v", tt.input, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseProviderName(%q) unexpected error: %v", tt.input, err)
			}
			if got != tt.want {
				t.Errorf("ParseProviderName(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestProviderStatus_String(t *testing.T) {
	tests := []struct {
		status domain.ProviderStatus
		want   string
	}{
		{domain.ProviderStatusOK, "ok"},
		{domain.ProviderStatusTimeout, "timeout"},
		{domain.ProviderStatusError, "error"},
		{domain.ProviderStatusRateLimited, "rate_limited"},
		{domain.ProviderStatusCircuitOpen, "circuit_open"},
		{domain.ProviderStatus(999), "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			got := tt.status.String()
			if got != tt.want {
				t.Errorf("ProviderStatus(%d).String() = %q, want %q", tt.status, got, tt.want)
			}
		})
	}
}

func TestParseResultKind_RoundTrip(t *testing.T) {
	kinds := []domain.ResultKind{domain.ResultKindArtist, domain.ResultKindAlbum, domain.ResultKindTrack, domain.ResultKindPlaylist}
	for _, k := range kinds {
		t.Run(k.String(), func(t *testing.T) {
			parsed, err := domain.ParseResultKind(k.String())
			if err != nil {
				t.Fatalf("round-trip failed for %v: %v", k, err)
			}
			if parsed != k {
				t.Errorf("round-trip: got %v, want %v", parsed, k)
			}
		})
	}
}

func TestParseProviderName_RoundTrip(t *testing.T) {
	providers := []domain.ProviderName{
		domain.ProviderDeezer, domain.ProviderMusicBrainz, domain.ProviderSoundCloud,
		domain.ProviderITunes, domain.ProviderTheAudioDB,
		domain.ProviderDiscogs, domain.ProviderYouTube,
	}
	for _, p := range providers {
		t.Run(p.String(), func(t *testing.T) {
			parsed, err := domain.ParseProviderName(p.String())
			if err != nil {
				t.Fatalf("round-trip failed for %v: %v", p, err)
			}
			if parsed != p {
				t.Errorf("round-trip: got %v, want %v", parsed, p)
			}
		})
	}
}

func TestResolutionTierStamp_ZeroValueIsUnstamped(t *testing.T) {
	var zero domain.ResolutionTierStamp
	if zero.Stamped || zero.Tier != domain.EntityResolutionNone {
		t.Errorf("zero stamp = %+v, want unstamped at none", zero)
	}
	got := domain.StampResolutionTier(domain.EntityResolutionNone)
	if !got.Stamped || got.Tier != domain.EntityResolutionNone {
		t.Errorf("StampResolutionTier(none) = %+v, want stamped at none", got)
	}
}
