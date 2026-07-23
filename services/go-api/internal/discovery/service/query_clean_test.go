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
		{
			// Lowercasing "İ" (U+0130) grows its byte length; the old
			// index-on-lowered / slice-on-original removal mangled the query.
			name: "multibyte case-fold query survives noise removal",
			raw:  "İstanbul lyrics",
			want: "İstanbul",
		},
		{
			name: "removes every occurrence of a noise phrase",
			raw:  "hello lyrics lyrics",
			want: "hello",
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

func TestCleanQueryTrailingFeat(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want string
	}{
		{
			name: "strips trailing feat",
			raw:  "Calvin Harris feat",
			want: "Calvin Harris",
		},
		{
			name: "strips trailing feat with period",
			raw:  "Calvin Harris feat.",
			want: "Calvin Harris",
		},
		{
			name: "strips trailing ft",
			raw:  "Drake ft",
			want: "Drake",
		},
		{
			name: "strips trailing featuring",
			raw:  "Metro Boomin featuring",
			want: "Metro Boomin",
		},
		{
			name: "case insensitive trailing FEAT",
			raw:  "Calvin Harris FEAT",
			want: "Calvin Harris",
		},
		{
			name: "keeps mid-query feat with featured artist",
			raw:  "Drake feat Rihanna",
			want: "Drake feat Rihanna",
		},
		{
			name: "keeps mid-query ft with featured artist",
			raw:  "Calvin Harris ft Rihanna",
			want: "Calvin Harris ft Rihanna",
		},
		{
			name: "does not strip word ending in ft",
			raw:  "Daft Punk",
			want: "Daft Punk",
		},
		{
			name: "does not strip word ending in feat",
			raw:  "great feat of strength",
			want: "great feat of strength",
		},
		{
			name: "lone feat returns original",
			raw:  "feat",
			want: "feat",
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
