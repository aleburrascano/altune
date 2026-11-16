package persistence

import (
	"altune/go-api/internal/discovery/domain"
	"reflect"
	"testing"
)

func TestUnreleasedSongCodec(t *testing.T) {
	hasMedia := true
	cases := map[string]domain.UnreleasedSong{
		"every field": {
			GeniusID:     42,
			Title:        "Lost Song",
			CoverURL:     "https://img.example/c.jpg",
			GeniusURL:    "https://genius.com/lost-song",
			ReleaseDate:  "2024-05-01",
			HasMedia:     &hasMedia,
			Quality:      "snippet",
			TrackerKey:   "tracker-1",
			AudioLinks:   []domain.AudioLink{{Provider: domain.AudioProviderSoundCloud, ExternalID: "sc1", URL: "https://soundcloud.com/a/b"}},
			OpenURL:      "https://open.example/x",
			OpenHost:     "open.example",
			OffStreaming: true,
		},
		"unknown media": {GeniusID: 1, Title: "Bare", GeniusURL: "https://genius.com/bare"},
	}
	for name, want := range cases {
		t.Run(name, func(t *testing.T) {
			raw, err := encodeUnreleasedSong(want)
			if err != nil {
				t.Fatalf("encode: %v", err)
			}
			got, err := decodeUnreleasedSong(raw)
			if err != nil {
				t.Fatalf("decode: %v", err)
			}
			if !reflect.DeepEqual(got, want) {
				t.Errorf("round trip = %+v, want %+v", got, want)
			}
		})
	}
}

func TestKnownIDsCodec(t *testing.T) {
	want := map[domain.ProviderName]string{domain.ProviderDeezer: "d1", domain.ProviderMusicBrainz: "mb1"}
	raw, err := encodeKnownIDs(want)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	got, err := decodeKnownIDs(raw)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("round trip = %v, want %v", got, want)
	}
}
