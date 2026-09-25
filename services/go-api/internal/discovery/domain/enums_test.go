package domain

import (
	"testing"
)

func TestParseResultKind(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    ResultKind
		wantErr bool
	}{
		{name: "artist", input: "artist", want: ResultKindArtist},
		{name: "album", input: "album", want: ResultKindAlbum},
		{name: "track", input: "track", want: ResultKindTrack},
		{name: "playlist", input: "playlist", want: ResultKindPlaylist},
		{name: "invalid", input: "podcast", wantErr: true},
		{name: "empty", input: "", wantErr: true},
		{name: "uppercase rejected", input: "Artist", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := ParseResultKind(tt.input)
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
		kind ResultKind
		want string
	}{
		{ResultKindUnknown, "unknown"},
		{ResultKindArtist, "artist"},
		{ResultKindAlbum, "album"},
		{ResultKindTrack, "track"},
		{ResultKindPlaylist, "playlist"},
		{ResultKind(999), "unknown"},
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
		conf Confidence
		want string
	}{
		{ConfidenceHigh, "high"},
		{ConfidenceMedium, "medium"},
		{ConfidenceLow, "low"},
		{Confidence(999), "unknown"},
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
		tier EntityResolutionTier
		want string
	}{
		{EntityResolutionMBID, "mbid"},
		{EntityResolutionISRC, "isrc"},
		{EntityResolutionUPC, "upc"},
		{EntityResolutionBridge, "bridge"},
		{EntityResolutionNone, "none"},
		{EntityResolutionTier(999), "unknown"},
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
	if CanonicalContentProvider != ProviderDeezer {
		t.Fatalf("CanonicalContentProvider = %v, want %v", CanonicalContentProvider, ProviderDeezer)
	}
	for p := ProviderUnknown; p <= ProviderSpotify; p++ {
		want := p == ProviderDeezer
		if got := IsCanonicalContentProvider(p); got != want {
			t.Errorf("IsCanonicalContentProvider(%v) = %v, want %v", p, got, want)
		}
	}
}

func TestProviderName_String(t *testing.T) {
	tests := []struct {
		provider ProviderName
		want     string
	}{
		{ProviderUnknown, "unknown"},
		{ProviderDeezer, "deezer"},
		{ProviderMusicBrainz, "musicbrainz"},
		{ProviderSoundCloud, "soundcloud"},
		{ProviderLastFM, "lastfm"},
		{ProviderITunes, "itunes"},
		{ProviderTheAudioDB, "theaudiodb"},
		{ProviderDiscogs, "discogs"},
		{ProviderYouTube, "youtube"},
		{ProviderAmazonMusic, "amazonmusic"},
		{ProviderAppleMusic, "applemusic"},
		{ProviderSpotify, "spotify"},
		{ProviderName(999), "unknown"},
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
		want    ProviderName
		wantErr bool
	}{
		{name: "deezer", input: "deezer", want: ProviderDeezer},
		{name: "musicbrainz", input: "musicbrainz", want: ProviderMusicBrainz},
		{name: "soundcloud", input: "soundcloud", want: ProviderSoundCloud},
		{name: "lastfm", input: "lastfm", want: ProviderLastFM},
		{name: "itunes", input: "itunes", want: ProviderITunes},
		{name: "theaudiodb", input: "theaudiodb", want: ProviderTheAudioDB},
		{name: "discogs", input: "discogs", want: ProviderDiscogs},
		{name: "youtube", input: "youtube", want: ProviderYouTube},
		{name: "amazonmusic", input: "amazonmusic", want: ProviderAmazonMusic},
		{name: "applemusic", input: "applemusic", want: ProviderAppleMusic},
		{name: "spotify", input: "spotify", want: ProviderSpotify},
		{name: "invalid", input: "napster", wantErr: true},
		{name: "empty", input: "", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := ParseProviderName(tt.input)
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
		status ProviderStatus
		want   string
	}{
		{ProviderStatusOK, "ok"},
		{ProviderStatusTimeout, "timeout"},
		{ProviderStatusError, "error"},
		{ProviderStatusRateLimited, "rate_limited"},
		{ProviderStatusCircuitOpen, "circuit_open"},
		{ProviderStatus(999), "unknown"},
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
	kinds := []ResultKind{ResultKindArtist, ResultKindAlbum, ResultKindTrack, ResultKindPlaylist}
	for _, k := range kinds {
		t.Run(k.String(), func(t *testing.T) {
			parsed, err := ParseResultKind(k.String())
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
	providers := []ProviderName{
		ProviderDeezer, ProviderMusicBrainz, ProviderSoundCloud,
		ProviderLastFM, ProviderITunes, ProviderTheAudioDB,
		ProviderDiscogs, ProviderYouTube,
	}
	for _, p := range providers {
		t.Run(p.String(), func(t *testing.T) {
			parsed, err := ParseProviderName(p.String())
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
	var zero ResolutionTierStamp
	if zero.Stamped || zero.Tier != EntityResolutionNone {
		t.Errorf("zero stamp = %+v, want unstamped at none", zero)
	}
	got := StampResolutionTier(EntityResolutionNone)
	if !got.Stamped || got.Tier != EntityResolutionNone {
		t.Errorf("StampResolutionTier(none) = %+v, want stamped at none", got)
	}
}
