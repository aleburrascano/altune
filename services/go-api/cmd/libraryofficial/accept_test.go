package main

import (
	"altune/go-api/internal/acquisition/adapters/ytmsearch"
	"testing"
)

func theTrack() target {
	return target{TrackID: "t1", UserID: "u1", Artist: "Cutting Crew", Title: "(I Just) Died in Your Arms", Album: "Broadcast", ISRC: "GBAAA8600001", CatalogSeconds: 280, Version: 3}
}

func artTrack() ytmsearch.Song {
	return ytmsearch.Song{
		VideoID: "AbCdEfGhIj1", Title: "(I Just) Died In Your Arms", Artists: []string{"Cutting Crew"},
		Album: "Broadcast", DurationSeconds: 282, Type: ytmsearch.VideoTypeATV,
	}
}

func TestJudge(t *testing.T) {
	cases := []struct {
		name   string
		track  func(*target)
		song   func(*ytmsearch.Song)
		expect rejection
	}{
		{"the label's art track is accepted", nil, nil, accepted},
		{"a music video is not an art track", nil, func(s *ytmsearch.Song) { s.Type = ytmsearch.VideoTypeOMV }, rejectNotArtTrack},
		{"a fan upload is not an art track", nil, func(s *ytmsearch.Song) { s.Type = ytmsearch.VideoTypeUGC }, rejectNotArtTrack},
		{"an artist that is not credited", nil, func(s *ytmsearch.Song) { s.Artists = []string{"Tribute Band"} }, rejectArtist},
		{"a credit among several artists is enough", nil, func(s *ytmsearch.Song) { s.Artists = []string{"Guest", "Cutting Crew"} }, accepted},
		{"another song", nil, func(s *ytmsearch.Song) { s.Title = "One for the Mockingbird" }, rejectTitle},
		{"a length beyond the authoritative tolerance", nil, func(s *ytmsearch.Song) { s.DurationSeconds = 330 }, rejectLength},
		{"a length at the tolerance edge", nil, func(s *ytmsearch.Song) { s.DurationSeconds = 288 }, accepted},
		{"a song with no length", nil, func(s *ytmsearch.Song) { s.DurationSeconds = 0 }, rejectLength},
		{"a track with no catalog length", func(tr *target) { tr.CatalogSeconds = 0 }, nil, rejectNoCatalogLen},
		{"another album", nil, func(s *ytmsearch.Song) { s.Album = "The Greatest Hits" }, rejectAlbum},
		{"album spelled with other case", func(tr *target) { tr.Album = "BROADCAST" }, nil, accepted},
		{"an unknown album is not held against the song", func(tr *target) { tr.Album = "" }, func(s *ytmsearch.Song) { s.Album = "Anything" }, accepted},
		{"an unreleased album is not held against the song", func(tr *target) { tr.Album = "UNRELEASED" }, func(s *ytmsearch.Song) { s.Album = "Anything" }, accepted},
		{"a video id that is not a video id", nil, func(s *ytmsearch.Song) { s.VideoID = "short" }, rejectVideoID},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			track, song := theTrack(), artTrack()
			if tc.track != nil {
				tc.track(&track)
			}
			if tc.song != nil {
				tc.song(&song)
			}
			if got := track.judge(song); got != tc.expect {
				t.Fatalf("judge = %q, want %q", got, tc.expect)
			}
		})
	}
}
