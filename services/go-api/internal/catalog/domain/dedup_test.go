package domain

import (
	"testing"
)

func TestComputeDedupKey(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		title  string
		artist string
		album  string
		want   string
	}{
		{
			name:   "basic case",
			title:  "Hello",
			artist: "Adele",
			album:  "25",
			want:   "hello|adele|25",
		},
		{
			name:   "case insensitive",
			title:  "HELLO",
			artist: "ADELE",
			album:  "TWENTY FIVE",
			want:   "hello|adele|twenty five",
		},
		{
			name:   "whitespace normalized",
			title:  "  Hello   World  ",
			artist: "  Some   Artist  ",
			album:  "  The   Album  ",
			want:   "hello world|some artist|the album",
		},
		{
			name:   "special chars stripped",
			title:  "Hello! (World) [Remix]",
			artist: "Artist & Co.",
			album:  "Album #1",
			want:   "hello world remix|artist co|album 1",
		},
		{
			name:   "all fields empty",
			title:  "",
			artist: "",
			album:  "",
			want:   "||",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := computeDedupKey(tt.title, tt.artist, tt.album)
			if got != tt.want {
				t.Errorf("computeDedupKey(%q, %q, %q) = %q, want %q",
					tt.title, tt.artist, tt.album, got, tt.want)
			}
		})
	}
}
