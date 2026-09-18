package domain

import (
	"errors"
	"testing"
)

func TestQueueSource_RoundTrip(t *testing.T) {
	tests := []struct {
		name   string
		source QueueSource
	}{
		{"library", QueueSource{Kind: SourceKindLibrary}},
		{"playlist", QueueSource{Kind: SourceKindPlaylist, PlaylistId: "abc", Name: "Road trip"}},
		{"playlist name with colon", QueueSource{Kind: SourceKindPlaylist, PlaylistId: "abc", Name: "Best of: 2019"}},
		{"playlist id with colon", QueueSource{Kind: SourceKindPlaylist, PlaylistId: "a:b", Name: "x"}},
		{"playlist id with colon no name", QueueSource{Kind: SourceKindPlaylist, PlaylistId: "a:b:c"}},
		{"playlist id with percent", QueueSource{Kind: SourceKindPlaylist, PlaylistId: "a%b", Name: "y"}},
		{"search", QueueSource{Kind: SourceKindSearch, Query: "boards of canada"}},
		{"search without query", QueueSource{Kind: SourceKindSearch}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ParseQueueSource(tt.source.String())

			if got != tt.source {
				t.Errorf("round trip = %+v, want %+v", got, tt.source)
			}
		})
	}
}

func TestParseQueueSource_Empty(t *testing.T) {
	if got := ParseQueueSource(""); !got.IsZero() {
		t.Errorf("ParseQueueSource(\"\") = %+v, want zero", got)
	}
}

func TestParseQueueSource_Unknown(t *testing.T) {
	if got := ParseQueueSource("mixtape:7"); !got.IsZero() {
		t.Errorf("ParseQueueSource(unknown) = %+v, want zero", got)
	}
}

func TestQueueSource_StringZero(t *testing.T) {
	if got := (QueueSource{}).String(); got != "" {
		t.Errorf("String() = %q, want empty", got)
	}
}

func TestParseQueueSource_MalformedSearchYieldsZeroNotEmptySearch(t *testing.T) {
	got := ParseQueueSource("search:%ZZ")
	if !got.IsZero() {
		t.Errorf("malformed source_id must not decode to a partial source, got %+v", got)
	}
}

func TestParseQueueSource_MalformedPlaylistYieldsZero(t *testing.T) {
	got := ParseQueueSource("playlist:%ZZ:name")
	if !got.IsZero() {
		t.Errorf("malformed playlist id must not decode to a partial source, got %+v", got)
	}
}

func TestParseQueueSource_MalformedPlaylistNameYieldsZero(t *testing.T) {
	got := ParseQueueSource("playlist:abc:%ZZ")
	if !got.IsZero() {
		t.Errorf("malformed playlist name must not decode to a partial source, got %+v", got)
	}
}

func TestFormatQueueSource_UnknownKindIsValidationError(t *testing.T) {
	_, err := FormatQueueSource(QueueSource{Kind: "album", PlaylistId: "xyz"}, "album:xyz")
	if err == nil {
		t.Fatal("expected an unknown non-empty kind to be rejected, got nil error")
	}
	var ve *ValidationError
	if !errors.As(err, &ve) {
		t.Fatalf("expected *ValidationError, got %T: %v", err, err)
	}
}

func TestFormatQueueSource_IdlessPlaylistStoresNoSourceInsteadOfFailing(t *testing.T) {
	// #1569: {"kind":"playlist","playlist_id":""} formatted to "playlist::", a
	// non-empty token stored as if it named a playlist. #1577: rejecting it
	// instead 400s every save a client sends after resuming such a source, so
	// the meaningless label is dropped and the queue still saves.
	for name, source := range map[string]QueueSource{
		"no id":       {Kind: SourceKindPlaylist},
		"no id, name": {Kind: SourceKindPlaylist, Name: "Road trip"},
	} {
		t.Run(name, func(t *testing.T) {
			got, err := FormatQueueSource(source, "")
			if err != nil {
				t.Fatalf("a playlist source naming no playlist must not fail the save: %v", err)
			}
			if got != "" {
				t.Errorf("FormatQueueSource(%+v) = %q, want no stored source", source, got)
			}
		})
	}
}

func TestFormatQueueSource_IdlessPlaylistFallbackStoresNoSource(t *testing.T) {
	// The raw source_id field is the second door to the same token, and it must
	// answer it the way the structured source does rather than the opposite way
	// (#1577): not stored, not rejected.
	for _, fallback := range []string{"playlist:", "playlist::", "playlist::Road+trip"} {
		got, err := FormatQueueSource(QueueSource{}, fallback)
		if err != nil {
			t.Errorf("legacy source_id %q naming no playlist must not fail the save: %v", fallback, err)
			continue
		}
		if got != "" {
			t.Errorf("FormatQueueSource(zero, %q) = %q, want no stored source", fallback, got)
		}
	}
}

func TestParseQueueSource_IdlessPlaylistTokenIsZero(t *testing.T) {
	// A stored token naming no playlist must not rehydrate into a source a
	// reader can echo back to a client as if a playlist were playing (#1577).
	for _, token := range []string{"playlist:", "playlist::", "playlist::Road+trip"} {
		if got := ParseQueueSource(token); !got.IsZero() {
			t.Errorf("ParseQueueSource(%q) = %+v, want zero", token, got)
		}
	}
}

func TestFormatQueueSource_PlaylistWithIdFormats(t *testing.T) {
	got, err := FormatQueueSource(QueueSource{Kind: SourceKindPlaylist, PlaylistId: "abc", Name: "Road trip"}, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "playlist:abc:Road+trip" {
		t.Errorf("FormatQueueSource(playlist) = %q, want %q", got, "playlist:abc:Road+trip")
	}
}

func TestFormatQueueSource_KnownKindFormats(t *testing.T) {
	got, err := FormatQueueSource(QueueSource{Kind: SourceKindLibrary}, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "library" {
		t.Errorf("FormatQueueSource(library) = %q, want %q", got, "library")
	}
}

func TestFormatQueueSource_ZeroSourceFallsBackToSourceId(t *testing.T) {
	got, err := FormatQueueSource(QueueSource{}, "search:legacy")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "search:legacy" {
		t.Errorf("FormatQueueSource(zero) = %q, want fallback %q", got, "search:legacy")
	}
}

func TestFormatQueueSource_EmptyFallbackStaysEmpty(t *testing.T) {
	got, err := FormatQueueSource(QueueSource{}, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "" {
		t.Errorf("FormatQueueSource(zero, \"\") = %q, want empty", got)
	}
}

func TestFormatQueueSource_GarbageFallbackIsValidationError(t *testing.T) {
	// Reproduces #620: a legacy source_id that cannot decode to a known-kind
	// source must be rejected, not silently persisted only to read back as
	// source: null on the next GET.
	_, err := FormatQueueSource(QueueSource{}, "mixtape:7")
	if err == nil {
		t.Fatal("expected garbage legacy source_id to be rejected, got nil error")
	}
	var ve *ValidationError
	if !errors.As(err, &ve) {
		t.Fatalf("expected *ValidationError, got %T: %v", err, err)
	}
}

// The two ways a source fails a save are a client error and a stored-token
// error, and a client fixes them differently, so they cannot share one code
// (#1596).
func TestFormatQueueSource_EachRejectionHasItsOwnCode(t *testing.T) {
	tests := []struct {
		name     string
		source   QueueSource
		fallback string
		wantCode string
	}{
		{
			name:     "unknown source kind",
			source:   QueueSource{Kind: "album", PlaylistId: "xyz"},
			fallback: "album:xyz",
			wantCode: "playback.unknown_source_kind",
		},
		{
			name:     "undecodable legacy source_id",
			fallback: "mixtape:7",
			wantCode: "playback.unrecognized_source_id",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := FormatQueueSource(tt.source, tt.fallback)

			if got := validationCode(t, err); got != tt.wantCode {
				t.Errorf("code = %q, want %q", got, tt.wantCode)
			}
		})
	}
}

func TestFormatQueueSource_LegitimateFallbacksAccepted(t *testing.T) {
	for _, fallback := range []string{
		"library",
		"search",
		"search:boards of canada",
		"playlist:abc:Road trip",
		"playlist:a%3Ab:x",
	} {
		got, err := FormatQueueSource(QueueSource{}, fallback)
		if err != nil {
			t.Errorf("legitimate fallback %q rejected: %v", fallback, err)
			continue
		}
		if got != fallback {
			t.Errorf("legitimate fallback %q rewritten to %q", fallback, got)
		}
	}
}
