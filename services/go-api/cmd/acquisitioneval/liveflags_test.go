package main

import (
	"altune/go-api/internal/acquisition/adapters/ytmsearch"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

const ytmsearchTestdata = "../../internal/acquisition/adapters/ytmsearch/testdata"

func readTestdata(t *testing.T, name string) []byte {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(ytmsearchTestdata, name))
	if err != nil {
		t.Fatalf("read testdata: %v", err)
	}
	return raw
}

func luckiSongs(t *testing.T) []ytmsearch.Song {
	t.Helper()
	songs, err := ytmsearch.ParseSongs(readTestdata(t, "search_lucki_2021_vibes.json"))
	if err != nil {
		t.Fatalf("ParseSongs: %v", err)
	}
	return songs
}

func TestYoutubeFlags_AppliesTheStudyRules(t *testing.T) {
	luckiTrack := liveTrack{Title: "2021 Vibes", Artist: "LUCKI"}
	tests := map[string]struct {
		track liveTrack
		src   youtubeSource
		want  []string
	}{
		"the official audio of the right upload raises nothing": {
			track: liveTrack{Title: "remember them days", Artist: "SLAYR"},
			src:   youtubeSource{VideoID: "XDC3uH1FSgw", Title: "remember them days", Author: "slayr - Topic", Type: ytmsearch.VideoTypeATV},
			want:  []string{},
		},
		"an official music video is a music_video and a video_title": {
			track: liveTrack{Title: "remember them days", Artist: "SLAYR"},
			src:   youtubeSource{VideoID: "u4GAeAYGAqE", Title: "slayr - remember them days (OFFICIAL MUSIC VIDEO)", Author: "slayr", Type: ytmsearch.VideoTypeOMV},
			want:  []string{"music_video(OMV)", "video_title"},
		},
		"a fan reupload of a user upload with a lyrics qualifier": {
			track: liveTrack{Title: "Rollacoasta", Artist: "prettifun", Evidence: &liveEvidence{DurationDeltaSeconds: 2.1567}},
			src:   youtubeSource{Title: "prettifun - Rollacoasta (lyrics in description)", Author: "music starz", Type: ytmsearch.VideoTypeUGC},
			want:  []string{"user_upload(UGC)", "fan_reupload_channel", "qualifier:lyrics"},
		},
		"a clean title on a fan channel is a clean_edit(title)": {
			track: liveTrack{Title: "Starstruck", Artist: "LUCKI"},
			src:   youtubeSource{Title: "LUCKI - Starstruck (Clean)", Author: "Rell", Type: ytmsearch.VideoTypeUGC},
			want:  []string{"user_upload(UGC)", "fan_reupload_channel", "qualifier:clean", "clean_edit(title)"},
		},
		"a VEVO channel is not a fan reupload": {
			track: liveTrack{Title: "Song", Artist: "Somebody"},
			src:   youtubeSource{Title: "Song", Author: "SomebodyVEVO", Type: ytmsearch.VideoTypeOMV},
			want:  []string{"music_video(OMV)"},
		},
		"an artist containing ft is not split inside the word": {
			track: liveTrack{Title: "Around the World", Artist: "Daft Punk"},
			src:   youtubeSource{Title: "Around the World", Author: "Dave Matthews Fan Uploads", Type: ytmsearch.VideoTypeUGC},
			want:  []string{"user_upload(UGC)", "fan_reupload_channel"},
		},
		"an artist containing with is not split inside the word": {
			track: liveTrack{Title: "Song", Artist: "Withers"},
			src:   youtubeSource{Title: "Song", Author: "Other Users", Type: ytmsearch.VideoTypeUGC},
			want:  []string{"user_upload(UGC)", "fan_reupload_channel"},
		},
		"an empty author never raises fan_reupload_channel": {
			track: liveTrack{Title: "Song", Artist: "Somebody"},
			src:   youtubeSource{Title: "Song", Author: "", Type: ytmsearch.VideoTypeATV},
			want:  []string{},
		},
		"a qualifier the track title asked for is not flagged": {
			track: liveTrack{Title: "Song (Live)", Artist: "Somebody"},
			src:   youtubeSource{Title: "Song (Live)", Author: "Somebody - Topic", Type: ytmsearch.VideoTypeATV},
			want:  []string{},
		},
		"the clean master is flagged when an explicit twin exists": {
			track: luckiTrack,
			src:   youtubeSource{VideoID: "88oFlVnMEsc", Title: "2021 Vibes", Author: "LUCKI - Topic", Type: ytmsearch.VideoTypeATV, Songs: luckiSongs(t)},
			want:  []string{"clean_edit(explicit exists)"},
		},
		"the explicit master is not flagged": {
			track: luckiTrack,
			src:   youtubeSource{VideoID: "LMDLNLM6Ji8", Title: "2021 Vibes", Author: "LUCKI - Topic", Type: ytmsearch.VideoTypeATV, Songs: luckiSongs(t)},
			want:  []string{},
		},
		"a stored id outside the search results is never a clean_edit": {
			track: luckiTrack,
			src:   youtubeSource{VideoID: "notInResult", Title: "2021 Vibes", Author: "LUCKI - Topic", Type: ytmsearch.VideoTypeATV, Songs: luckiSongs(t)},
			want:  []string{},
		},
		"only the top five search results count": {
			track: luckiTrack,
			src: youtubeSource{
				VideoID: "88oFlVnMEsc", Title: "2021 Vibes", Author: "LUCKI - Topic", Type: ytmsearch.VideoTypeATV,
				Songs: append(luckiSongs(t)[:1:1], luckiSongs(t)[2:]...),
			},
			want: []string{},
		},
		"a duration delta beyond eight seconds is flagged with its rounded sign": {
			track: liveTrack{Title: "Song", Artist: "Somebody", Evidence: &liveEvidence{DurationDeltaSeconds: -12.4}},
			src:   youtubeSource{Title: "Song", Author: "Somebody - Topic", Type: ytmsearch.VideoTypeATV},
			want:  []string{"duration_delta:-12s"},
		},
		"a duration delta of exactly eight seconds is not flagged": {
			track: liveTrack{Title: "Song", Artist: "Somebody", Evidence: &liveEvidence{DurationDeltaSeconds: 8}},
			src:   youtubeSource{Title: "Song", Author: "Somebody - Topic", Type: ytmsearch.VideoTypeATV},
			want:  []string{},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			got := youtubeFlags(tc.track, tc.src)
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("flags = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestMatching_NormalizesLikeTheStudy(t *testing.T) {
	tests := map[string]struct {
		got  string
		want string
	}{
		"accents drop to ascii":      {got: normText("Beyoncé’s  Café"), want: "beyonces cafe"},
		"ampersand and dollar words": {got: normText("A$AP & Co"), want: "asap and co"},
		"punctuation collapses":      {got: normText("  Hello,, World!! "), want: "hello world"},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			if tc.got != tc.want {
				t.Fatalf("got %q, want %q", tc.got, tc.want)
			}
		})
	}
}

func TestMatching_TitleAndArtist(t *testing.T) {
	if !titleMatches("Song (feat. Guest) [Explicit]", "Song") {
		t.Error("feat. and explicit suffixes should not defeat a title match")
	}
	if titleMatches("Another Song", "Song Two") {
		t.Error("unrelated titles must not match")
	}
	if titleMatches("anything", "") {
		t.Error("an empty track title matches nothing")
	}
	if !artistMatches("Lucki, Somebody", "LUCKI feat. Guest") {
		t.Error("the first artist token should be found in the candidate text")
	}
	if artistMatches("Other Person", "LUCKI") {
		t.Error("a different artist must not match")
	}
	if artistMatches("Other Person", "") {
		t.Error("an empty artist matches nothing")
	}
}
