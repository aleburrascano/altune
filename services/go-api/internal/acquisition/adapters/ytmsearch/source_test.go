package ytmsearch

import (
	"altune/go-api/internal/acquisition/ports"
	"context"
	"errors"
	"testing"
)

type fakeSongSearch struct {
	songs   []Song
	err     error
	queries []string
}

func (f *fakeSongSearch) SearchSongs(_ context.Context, query string) ([]Song, error) {
	f.queries = append(f.queries, query)
	return f.songs, f.err
}

type recordingDownloader struct{ gotURL string }

func (d *recordingDownloader) Download(_ context.Context, url, outDir string) (ports.FetchedAudio, error) {
	d.gotURL = url
	return ports.FetchedAudio{Path: outDir + "/a.m4a"}, nil
}

func atv(id, title string, artists []string, seconds int, explicit bool) Song {
	return Song{VideoID: id, Title: title, Artists: artists, DurationSeconds: seconds, Type: VideoTypeATV, Explicit: explicit}
}

var vibesRequest = ports.FindRequest{Title: "2021 Vibes", Artist: "LUCKI", Duration: 125}

func find(t *testing.T, songs []Song, req ports.FindRequest) []ports.AudioCandidate {
	t.Helper()
	got, err := NewSource(&fakeSongSearch{songs: songs}, &recordingDownloader{}).Find(context.Background(), req)
	if err != nil {
		t.Fatalf("Find: %v", err)
	}
	return got
}

func TestSource_AStrongArtTrackHitIsOneResolvedCandidate(t *testing.T) {
	search := &fakeSongSearch{songs: []Song{atv("LMDLNLM6Ji8", "2021 Vibes", []string{"LUCKI"}, 125, true)}}
	got, err := NewSource(search, &recordingDownloader{}).Find(context.Background(), vibesRequest)
	if err != nil {
		t.Fatalf("Find: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("candidates = %v, want 1", got)
	}
	if got[0].URL != "https://music.youtube.com/watch?v=LMDLNLM6Ji8" || !got[0].Resolved || got[0].Duration != 125 {
		t.Errorf("candidate = %+v, want a resolved music.youtube.com candidate of 125 s", got[0])
	}
	if len(search.queries) != 1 || search.queries[0] != "2021 Vibes LUCKI" {
		t.Errorf("queries = %q, want one title + artist query", search.queries)
	}
}

func TestSource_NoCandidateUnlessEveryCheckPasses(t *testing.T) {
	cases := map[string]Song{
		"music video":         {VideoID: "u4GAeAYGAqE", Title: "2021 Vibes", Artists: []string{"LUCKI"}, DurationSeconds: 125, Type: VideoTypeOMV},
		"user upload":         {VideoID: "abcdefghijk", Title: "2021 Vibes", Artists: []string{"LUCKI"}, DurationSeconds: 125, Type: VideoTypeUGC},
		"other title":         atv("aaaaaaaaaaa", "Karma A Bitch", []string{"LUCKI"}, 125, true),
		"remix of the title":  atv("bbbbbbbbbbb", "2021 Vibes (Remix)", []string{"LUCKI"}, 125, true),
		"other artist":        atv("ccccccccccc", "2021 Vibes", []string{"Someone Else"}, 125, true),
		"duration 4 s longer": atv("ddddddddddd", "2021 Vibes", []string{"LUCKI"}, 129, true),
	}
	for name, song := range cases {
		t.Run(name, func(t *testing.T) {
			if got := find(t, []Song{song}, vibesRequest); len(got) != 0 {
				t.Errorf("candidates = %+v, want none", got)
			}
		})
	}
}

func TestSource_ExplicitBeatsCleanWhenBothMatch(t *testing.T) {
	got := find(t, []Song{
		atv("88oFlVnMEsc", "2021 Vibes", []string{"LUCKI"}, 125, false),
		atv("LMDLNLM6Ji8", "2021 Vibes", []string{"LUCKI"}, 125, true),
	}, vibesRequest)
	if len(got) != 1 || got[0].URL != "https://music.youtube.com/watch?v=LMDLNLM6Ji8" {
		t.Errorf("candidates = %+v, want only the explicit LMDLNLM6Ji8", got)
	}
}

func TestSource_CreditAndTitleQuirksStillMatch(t *testing.T) {
	cases := map[string]struct {
		req  ports.FindRequest
		song Song
	}{
		"dollar sign in the credit": {
			req:  ports.FindRequest{Title: "Teen Bean", Artist: "Ken Carson", Duration: 119},
			song: atv("eeeeeeeeeee", "Teen Bean", []string{"Ken Car$on"}, 120, true),
		},
		"bonus tag on the title": {
			req:  ports.FindRequest{Title: "Y NOT?", Artist: "LUCKI", Duration: 148},
			song: atv("fffffffffff", "Y NOT? (Bonus)", []string{"LUCKI"}, 149, true),
		},
		"featured artist in the title": {
			req:  ports.FindRequest{Title: "Syrup Sandwiches", Artist: "Cordae", Duration: 173},
			song: atv("ggggggggggg", "Syrup Sandwiches (feat. Joey Bada$$)", []string{"Cordae"}, 173, true),
		},
		"second credited artist": {
			req:  ports.FindRequest{Title: "Our 25th Birthday", Artist: "Central Cee", Duration: 308},
			song: atv("hhhhhhhhhhh", "Our 25th Birthday", []string{"Dave", "Central Cee"}, 308, true),
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if got := find(t, []Song{tc.song}, tc.req); len(got) != 1 {
				t.Errorf("candidates = %+v, want the one match", got)
			}
		})
	}
}

func TestSource_OnlyTheTopFiveResultsAreConsidered(t *testing.T) {
	filler := atv("iiiiiiiiiii", "Other", []string{"LUCKI"}, 100, true)
	songs := []Song{filler, filler, filler, filler, filler, atv("LMDLNLM6Ji8", "2021 Vibes", []string{"LUCKI"}, 125, true)}
	if got := find(t, songs, vibesRequest); len(got) != 0 {
		t.Errorf("candidates = %+v, want none past the top five", got)
	}
}

func TestSource_AnUnknownTrackDurationYieldsNothing(t *testing.T) {
	req := ports.FindRequest{Title: "2021 Vibes", Artist: "LUCKI"}
	if got := find(t, []Song{atv("LMDLNLM6Ji8", "2021 Vibes", []string{"LUCKI"}, 125, true)}, req); len(got) != 0 {
		t.Errorf("candidates = %+v, want none without a duration to check", got)
	}
}

func TestSource_SearchErrorsAreReturned(t *testing.T) {
	src := NewSource(&fakeSongSearch{err: errors.New("innertube 503")}, &recordingDownloader{})
	if _, err := src.Find(context.Background(), vibesRequest); err == nil {
		t.Fatal("Find error = nil, want the search error so the fallback runs and the outage is recorded")
	}
}

func TestSource_FetchDownloadsTheCandidateURL(t *testing.T) {
	downloader := &recordingDownloader{}
	src := NewSource(&fakeSongSearch{}, downloader)
	if _, err := src.Fetch(context.Background(), ports.AudioCandidate{URL: "https://music.youtube.com/watch?v=LMDLNLM6Ji8"}, t.TempDir()); err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if downloader.gotURL != "https://music.youtube.com/watch?v=LMDLNLM6Ji8" {
		t.Errorf("downloaded %q, want the candidate URL", downloader.gotURL)
	}
}
