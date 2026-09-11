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
			got := ParseQueueSource(tt.source.Format())

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

func TestQueueSource_FormatZero(t *testing.T) {
	if got := (QueueSource{}).Format(); got != "" {
		t.Errorf("Format() = %q, want empty", got)
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

func TestPackSourceId_UnknownKindIsValidationError(t *testing.T) {
	_, err := PackSourceId(QueueSource{Kind: "album", PlaylistId: "xyz"}, "album:xyz")
	if err == nil {
		t.Fatal("expected an unknown non-empty kind to be rejected, got nil error")
	}
	var ve *ValidationError
	if !errors.As(err, &ve) {
		t.Fatalf("expected *ValidationError, got %T: %v", err, err)
	}
}

func TestPackSourceId_KnownKindFormats(t *testing.T) {
	got, err := PackSourceId(QueueSource{Kind: SourceKindLibrary}, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "library" {
		t.Errorf("PackSourceId(library) = %q, want %q", got, "library")
	}
}

func TestPackSourceId_ZeroSourceFallsBackToSourceId(t *testing.T) {
	got, err := PackSourceId(QueueSource{}, "search:legacy")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "search:legacy" {
		t.Errorf("PackSourceId(zero) = %q, want fallback %q", got, "search:legacy")
	}
}
