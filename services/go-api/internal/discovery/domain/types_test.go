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

func TestParseConfidence(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    Confidence
		wantErr bool
	}{
		{name: "high", input: "high", want: ConfidenceHigh},
		{name: "medium", input: "medium", want: ConfidenceMedium},
		{name: "low", input: "low", want: ConfidenceLow},
		{name: "invalid", input: "very_high", wantErr: true},
		{name: "empty", input: "", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := ParseConfidence(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("ParseConfidence(%q) expected error, got %v", tt.input, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseConfidence(%q) unexpected error: %v", tt.input, err)
			}
			if got != tt.want {
				t.Errorf("ParseConfidence(%q) = %v, want %v", tt.input, got, tt.want)
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

func TestProviderName_String(t *testing.T) {
	tests := []struct {
		provider ProviderName
		want     string
	}{
		{ProviderDeezer, "deezer"},
		{ProviderMusicBrainz, "musicbrainz"},
		{ProviderSoundCloud, "soundcloud"},
		{ProviderLastFM, "lastfm"},
		{ProviderITunes, "itunes"},
		{ProviderTheAudioDB, "theaudiodb"},
		{ProviderDiscogs, "discogs"},
		{ProviderYouTube, "youtube"},
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
		{name: "invalid", input: "spotify", wantErr: true},
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

func TestContentValidationStatus_String(t *testing.T) {
	tests := []struct {
		status ContentValidationStatus
		want   string
	}{
		{ContentValidationFetchable, "fetchable"},
		{ContentValidationUnfetchable, "unfetchable"},
		{ContentValidationUnknown, "unknown"},
		{ContentValidationStatus(999), "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			got := tt.status.String()
			if got != tt.want {
				t.Errorf("ContentValidationStatus(%d).String() = %q, want %q", tt.status, got, tt.want)
			}
		})
	}
}

func TestNewSearchQuery_Valid(t *testing.T) {
	tests := []struct {
		name  string
		raw   string
		kinds map[ResultKind]bool
		limit int
	}{
		{
			name:  "typical query",
			raw:   "radiohead",
			kinds: map[ResultKind]bool{ResultKindTrack: true},
			limit: 25,
		},
		{
			name:  "limit lower bound",
			raw:   "query",
			kinds: map[ResultKind]bool{ResultKindArtist: true},
			limit: 1,
		},
		{
			name:  "limit upper bound",
			raw:   "query",
			kinds: map[ResultKind]bool{ResultKindAlbum: true},
			limit: 50,
		},
		{
			name: "multiple kinds",
			raw:  "beatles",
			kinds: map[ResultKind]bool{
				ResultKindTrack:  true,
				ResultKindAlbum:  true,
				ResultKindArtist: true,
			},
			limit: 10,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			q, err := NewSearchQuery(tt.raw, tt.kinds, tt.limit)
			if err != nil {
				t.Fatalf("NewSearchQuery() unexpected error: %v", err)
			}
			if q.Raw != tt.raw {
				t.Errorf("Raw = %q, want %q", q.Raw, tt.raw)
			}
			if q.Limit != tt.limit {
				t.Errorf("Limit = %d, want %d", q.Limit, tt.limit)
			}
			if len(q.Kinds) != len(tt.kinds) {
				t.Errorf("Kinds length = %d, want %d", len(q.Kinds), len(tt.kinds))
			}
		})
	}
}

func TestNewSearchQuery_Errors(t *testing.T) {
	validKinds := map[ResultKind]bool{ResultKindTrack: true}

	tests := []struct {
		name    string
		raw     string
		kinds   map[ResultKind]bool
		limit   int
		wantMsg string
	}{
		{
			name:    "empty raw",
			raw:     "",
			kinds:   validKinds,
			limit:   10,
			wantMsg: "raw query cannot be empty",
		},
		{
			name:    "empty kinds",
			raw:     "query",
			kinds:   map[ResultKind]bool{},
			limit:   10,
			wantMsg: "kinds cannot be empty",
		},
		{
			name:    "nil kinds",
			raw:     "query",
			kinds:   nil,
			limit:   10,
			wantMsg: "kinds cannot be empty",
		},
		{
			name:    "limit too low",
			raw:     "query",
			kinds:   validKinds,
			limit:   0,
			wantMsg: "limit must be between 1 and 50",
		},
		{
			name:    "limit negative",
			raw:     "query",
			kinds:   validKinds,
			limit:   -1,
			wantMsg: "limit must be between 1 and 50",
		},
		{
			name:    "limit too high",
			raw:     "query",
			kinds:   validKinds,
			limit:   51,
			wantMsg: "limit must be between 1 and 50",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			q, err := NewSearchQuery(tt.raw, tt.kinds, tt.limit)
			if err == nil {
				t.Fatalf("NewSearchQuery() expected error containing %q, got nil (result: %+v)", tt.wantMsg, q)
			}
			if got := err.Error(); got != tt.wantMsg {
				t.Errorf("error = %q, want %q", got, tt.wantMsg)
			}
		})
	}
}

func TestParseResultKind_RoundTrip(t *testing.T) {
	// Every valid ResultKind should survive String() -> Parse() round-trip
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

func TestParseConfidence_RoundTrip(t *testing.T) {
	confs := []Confidence{ConfidenceLow, ConfidenceMedium, ConfidenceHigh}
	for _, c := range confs {
		t.Run(c.String(), func(t *testing.T) {
			parsed, err := ParseConfidence(c.String())
			if err != nil {
				t.Fatalf("round-trip failed for %v: %v", c, err)
			}
			if parsed != c {
				t.Errorf("round-trip: got %v, want %v", parsed, c)
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
