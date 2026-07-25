package domain

import "testing"

func TestQueueSource_RoundTrip(t *testing.T) {
	tests := []struct {
		name   string
		source QueueSource
	}{
		{"library", QueueSource{Kind: SourceKindLibrary}},
		{"playlist", QueueSource{Kind: SourceKindPlaylist, PlaylistId: "abc", Name: "Road trip"}},
		{"playlist name with colon", QueueSource{Kind: SourceKindPlaylist, PlaylistId: "abc", Name: "Best of: 2019"}},
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
