package service

import "testing"

func TestCleanQuery(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want string
	}{
		{
			name: "strips official video",
			raw:  "Megaman Tay-K official video",
			want: "Megaman Tay-K",
		},
		{
			name: "strips official music video",
			raw:  "Blinding Lights The Weeknd official music video",
			want: "Blinding Lights The Weeknd",
		},
		{
			name: "strips lyrics",
			raw:  "Drake Hotline Bling lyrics",
			want: "Drake Hotline Bling",
		},
		{
			name: "strips lyric video",
			raw:  "Song Name lyric video",
			want: "Song Name",
		},
		{
			name: "strips audio",
			raw:  "Track Name audio",
			want: "Track Name",
		},
		{
			name: "strips hq hd 4k",
			raw:  "Song HQ HD 4K",
			want: "Song",
		},
		{
			name: "strips full album",
			raw:  "Artist Name full album",
			want: "Artist Name",
		},
		{
			name: "strips visualizer",
			raw:  "Song visualizer",
			want: "Song",
		},
		{
			name: "strips topic",
			raw:  "Artist - Topic",
			want: "Artist -",
		},
		{
			name: "case insensitive",
			raw:  "Song OFFICIAL VIDEO",
			want: "Song",
		},
		{
			name: "no noise returns as-is",
			raw:  "Megaman Tay-K",
			want: "Megaman Tay-K",
		},
		{
			name: "all noise returns original",
			raw:  "official video",
			want: "official video",
		},
		{
			name: "empty string returns empty",
			raw:  "",
			want: "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := CleanQuery(tt.raw)
			if got != tt.want {
				t.Errorf("CleanQuery(%q) = %q, want %q", tt.raw, got, tt.want)
			}
		})
	}
}
