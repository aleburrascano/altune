package main

import (
	"altune/go-api/internal/acquisition"
	"testing"
)

func theTrack() target {
	return target{TrackID: "t1", UserID: "u1", Artist: "Cutting Crew", Title: "(I Just) Died in Your Arms", Album: "Broadcast", ISRC: "GBAAA8600001", CatalogSeconds: 280, Version: 3}
}

func artTrack() acquisition.YtmSong {
	return acquisition.YtmSong{
		VideoID: "AbCdEfGhIj1", Title: "(I Just) Died In Your Arms", Artists: []string{"Cutting Crew"},
		Album: "Broadcast", DurationSeconds: 282, Type: acquisition.VideoTypeATV,
	}
}

func TestJudge(t *testing.T) {
	cases := []struct {
		name   string
		track  func(*target)
		song   func(*acquisition.YtmSong)
		expect rejection
	}{
		{"the label's art track is accepted", nil, nil, accepted},
		{"a music video is not an art track", nil, func(s *acquisition.YtmSong) { s.Type = acquisition.VideoTypeOMV }, rejectNotArtTrack},
		{"a fan upload is not an art track", nil, func(s *acquisition.YtmSong) { s.Type = acquisition.VideoTypeUGC }, rejectNotArtTrack},
		{"an artist that is not credited", nil, func(s *acquisition.YtmSong) { s.Artists = []string{"Tribute Band"} }, rejectArtist},
		{"a credit among several artists is enough", nil, func(s *acquisition.YtmSong) { s.Artists = []string{"Guest", "Cutting Crew"} }, accepted},
		{"another song", nil, func(s *acquisition.YtmSong) { s.Title = "One for the Mockingbird" }, rejectTitle},
		{"a length beyond the authoritative tolerance", nil, func(s *acquisition.YtmSong) { s.DurationSeconds = 330 }, rejectLength},
		{"a length at the tolerance edge", nil, func(s *acquisition.YtmSong) { s.DurationSeconds = 288 }, accepted},
		{"a song with no length", nil, func(s *acquisition.YtmSong) { s.DurationSeconds = 0 }, rejectLength},
		{"a song with no length against a catalog length inside the five second floor", func(tr *target) { tr.CatalogSeconds = 4 }, func(s *acquisition.YtmSong) { s.DurationSeconds = 0 }, rejectLength},
		{"a track with no catalog length", func(tr *target) { tr.CatalogSeconds = 0 }, nil, rejectNoCatalogLen},
		{"another album", nil, func(s *acquisition.YtmSong) { s.Album = "The Greatest Hits" }, rejectAlbum},
		{"album spelled with other case", func(tr *target) { tr.Album = "BROADCAST" }, nil, accepted},
		{"an unknown album is not held against the song", func(tr *target) { tr.Album = "" }, func(s *acquisition.YtmSong) { s.Album = "Anything" }, accepted},
		{"an unreleased album is not held against the song", func(tr *target) { tr.Album = "UNRELEASED" }, func(s *acquisition.YtmSong) { s.Album = "Anything" }, accepted},
		{"a video id that is not a video id", nil, func(s *acquisition.YtmSong) { s.VideoID = "short" }, rejectVideoID},
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
