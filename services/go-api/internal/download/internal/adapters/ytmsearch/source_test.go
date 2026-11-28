package ytmsearch_test

import (
	"altune/go-api/internal/download/downloadtest"
	"altune/go-api/internal/download/internal/adapters/ytmsearch"
	"altune/go-api/internal/download/internal/core/domain"
	"altune/go-api/internal/download/internal/core/ports"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

type fakeSongSearch struct {
	songs   []ytmsearch.Song
	err     error
	queries []string
}

func (f *fakeSongSearch) SearchSongs(_ context.Context, query string) ([]ytmsearch.Song, error) {
	f.queries = append(f.queries, query)
	return f.songs, f.err
}

type recordingDownloader struct{ gotURL string }

func (d *recordingDownloader) Download(_ context.Context, url, outDir string) (ports.FetchedAudio, error) {
	d.gotURL = url
	return ports.FetchedAudio{Path: outDir + "/a.m4a"}, nil
}

func atv(id, title string, artists []string, seconds int, explicit bool) ytmsearch.Song {
	return ytmsearch.Song{VideoID: id, Title: title, Artists: artists, DurationSeconds: seconds, Type: ytmsearch.VideoTypeAudioOnly, Explicit: explicit}
}

var vibesRequest = ports.FindRequest{Title: "2021 Vibes", Artist: "LUCKI", Duration: 125}

func find(t *testing.T, songs []ytmsearch.Song, req ports.FindRequest) []ports.AudioCandidate {
	t.Helper()
	got, err := ytmsearch.NewSource(&fakeSongSearch{songs: songs}, &recordingDownloader{}).Find(context.Background(), req)
	if err != nil {
		t.Fatalf("Find: %v", err)
	}
	return got
}

func TestSource_AStrongArtTrackHitIsOneResolvedCandidate(t *testing.T) {
	search := &fakeSongSearch{songs: []ytmsearch.Song{atv("LMDLNLM6Ji8", "2021 Vibes", []string{"LUCKI"}, 125, true)}}
	got, err := ytmsearch.NewSource(search, &recordingDownloader{}).Find(context.Background(), vibesRequest)
	if err != nil {
		t.Fatalf("Find: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("candidates = %v, want 1", got)
	}
	if got[0].URL != "https://music.youtube.com/watch?v=LMDLNLM6Ji8" || !got[0].IsResolved() || got[0].Duration != 125 || !got[0].Explicit {
		t.Errorf("candidate = %+v, want a resolved, explicit music.youtube.com candidate of 125 s", got[0])
	}
	if len(search.queries) != 1 || search.queries[0] != "2021 Vibes LUCKI" {
		t.Errorf("queries = %q, want one title + artist query", search.queries)
	}
}

func TestSource_AHitWithoutAValidVideoIDIsNeverTheCandidate(t *testing.T) {
	songs := []ytmsearch.Song{
		atv("short", "2021 Vibes", []string{"LUCKI"}, 125, true),
		atv("LMDLNLM6Ji8", "2021 Vibes", []string{"LUCKI"}, 125, true),
	}

	got := find(t, songs, vibesRequest)

	if len(got) != 1 || got[0].URL != "https://music.youtube.com/watch?v=LMDLNLM6Ji8" {
		t.Errorf("candidates = %+v, want only the hit with a valid video id", got)
	}
}

func TestSource_NoCandidateUnlessEveryCheckPasses(t *testing.T) {
	cases := map[string]ytmsearch.Song{
		"music video":        {VideoID: "u4GAeAYGAqE", Title: "2021 Vibes", Artists: []string{"LUCKI"}, DurationSeconds: 125, Type: ytmsearch.VideoTypeOfficialVideo},
		"user upload":        {VideoID: "abcdefghijk", Title: "2021 Vibes", Artists: []string{"LUCKI"}, DurationSeconds: 125, Type: ytmsearch.VideoTypeUserUpload},
		"other title":        atv("aaaaaaaaaaa", "Karma A Bitch", []string{"LUCKI"}, 125, true),
		"remix of the title": atv("bbbbbbbbbbb", "2021 Vibes (Remix)", []string{"LUCKI"}, 125, true),
		"other artist":       atv("ccccccccccc", "2021 Vibes", []string{"Someone Else"}, 125, true),
	}
	for name, song := range cases {
		t.Run(name, func(t *testing.T) {
			if got := find(t, []ytmsearch.Song{song}, vibesRequest); len(got) != 0 {
				t.Errorf("candidates = %+v, want none", got)
			}
		})
	}
}

func TestSource_ExplicitBeatsCleanWhenBothMatch(t *testing.T) {
	got := find(t, []ytmsearch.Song{
		atv("88oFlVnMEsc", "2021 Vibes", []string{"LUCKI"}, 125, false),
		atv("LMDLNLM6Ji8", "2021 Vibes", []string{"LUCKI"}, 125, true),
	}, vibesRequest)
	if len(got) != 1 || got[0].URL != "https://music.youtube.com/watch?v=LMDLNLM6Ji8" {
		t.Errorf("candidates = %+v, want only the explicit LMDLNLM6Ji8", got)
	}
}

func TestSource_TheCatalogsCleanFlagPicksTheCleanTwin(t *testing.T) {
	clean := false
	req := vibesRequest
	req.CatalogExplicit = &clean
	got := find(t, []ytmsearch.Song{
		atv("88oFlVnMEsc", "2021 Vibes", []string{"LUCKI"}, 125, false),
		atv("LMDLNLM6Ji8", "2021 Vibes", []string{"LUCKI"}, 125, true),
	}, req)
	if len(got) != 1 || got[0].URL != "https://music.youtube.com/watch?v=88oFlVnMEsc" {
		t.Errorf("candidates = %+v, want only the clean 88oFlVnMEsc", got)
	}
}

func TestSource_CreditAndTitleQuirksStillMatch(t *testing.T) {
	cases := map[string]struct {
		req  ports.FindRequest
		song ytmsearch.Song
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
			if got := find(t, []ytmsearch.Song{tc.song}, tc.req); len(got) != 1 {
				t.Errorf("candidates = %+v, want the one match", got)
			}
		})
	}
}

func TestSource_OnlyTheTopFiveResultsAreConsidered(t *testing.T) {
	filler := atv("iiiiiiiiiii", "Other", []string{"LUCKI"}, 100, true)
	songs := []ytmsearch.Song{filler, filler, filler, filler, filler, atv("LMDLNLM6Ji8", "2021 Vibes", []string{"LUCKI"}, 125, true)}
	if got := find(t, songs, vibesRequest); len(got) != 0 {
		t.Errorf("candidates = %+v, want none past the top five", got)
	}
}

func TestSource_TheSavedLengthDoesNotGateTheMatch(t *testing.T) {
	cases := map[string]float64{
		"saved length from an old bad download": 122,
		"no saved length":                       0,
	}
	for name, saved := range cases {
		t.Run(name, func(t *testing.T) {
			req := ports.FindRequest{Title: "Demigod", Artist: "SLAYR", Duration: saved}
			if got := find(t, []ytmsearch.Song{atv("jjjjjjjjjjj", "Demigod", []string{"slayr"}, 168, true)}, req); len(got) != 1 {
				t.Errorf("candidates = %+v, want the official track", got)
			}
		})
	}
}

func TestSource_TheClosestLengthBreaksATie(t *testing.T) {
	got := find(t, []ytmsearch.Song{
		atv("kkkkkkkkkkk", "Human", []string{"The Human League"}, 300, false),
		atv("lllllllllll", "Human", []string{"The Human League"}, 266, false),
	}, ports.FindRequest{Title: "Human", Artist: "The Human League", Duration: 264})
	if len(got) != 1 || got[0].URL != "https://music.youtube.com/watch?v=lllllllllll" {
		t.Errorf("candidates = %+v, want the 266 s track closest to the saved length", got)
	}
}

func TestSource_TheCatalogAlbumBreaksATie(t *testing.T) {
	compilation := atv("mmmmmmmmmmm", "Human", []string{"The Human League"}, 264, false)
	compilation.Album = "Greatest Hits"
	original := atv("nnnnnnnnnnn", "Human", []string{"The Human League"}, 300, false)
	original.Album = "Crash"
	req := ports.FindRequest{Title: "Human", Artist: "The Human League", Album: "crash", Duration: 264}

	got := find(t, []ytmsearch.Song{compilation, original}, req)
	if len(got) != 1 || got[0].URL != "https://music.youtube.com/watch?v=nnnnnnnnnnn" {
		t.Errorf("candidates = %+v, want the hit on the catalog album over the closer length", got)
	}
}

func TestSource_SearchErrorsAreReturned(t *testing.T) {
	src := ytmsearch.NewSource(&fakeSongSearch{err: errors.New("innertube 503")}, &recordingDownloader{})
	if _, err := src.Find(context.Background(), vibesRequest); err == nil {
		t.Fatal("Find error = nil, want the search error so the fallback runs and the outage is recorded")
	}
}

func TestSource_FetchDownloadsTheCandidateURL(t *testing.T) {
	downloader := &recordingDownloader{}
	src := ytmsearch.NewSource(&fakeSongSearch{}, downloader)
	if _, err := src.Fetch(context.Background(), downloadtest.Candidate("https://music.youtube.com/watch?v=LMDLNLM6Ji8"), t.TempDir()); err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if downloader.gotURL != "https://music.youtube.com/watch?v=LMDLNLM6Ji8" {
		t.Errorf("downloaded %q, want the candidate URL", downloader.gotURL)
	}
}

func TestSource_TheArtTrackCarriesWhatTheSearchObserved(t *testing.T) {
	song := atv("abc12345678", "Finesse", []string{"Bruno Mars"}, 191, false)
	song.Album = "24K Magic"
	req := ports.FindRequest{Title: "Finesse", Artist: "Bruno Mars", Duration: 191}

	got := find(t, []ytmsearch.Song{song}, req)
	if len(got) != 1 {
		t.Fatalf("candidates = %v, want 1", got)
	}
	if got[0].Kind != ports.KindArtTrack || got[0].Album != "24K Magic" || len(got[0].Artists) != 1 || got[0].Artists[0] != "Bruno Mars" {
		t.Errorf("candidate = %+v, want the art track with its observed artists and album", got[0])
	}
}

func TestSource_AnOutageIsSourceUnavailable(t *testing.T) {
	down := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusServiceUnavailable) }))
	t.Cleanup(down.Close)
	hung := make(chan struct{})
	slow := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) { <-hung }))
	t.Cleanup(slow.Close)
	t.Cleanup(func() { close(hung) })

	for name, client := range map[string]*ytmsearch.Client{
		"a 503":                clientFor(down, time.Second),
		"the client timed out": clientFor(slow, 30*time.Millisecond),
	} {
		t.Run(name, func(t *testing.T) {
			_, err := ytmsearch.NewSource(client, &recordingDownloader{}).Find(context.Background(), vibesRequest)

			if !isSourceUnavailable(err) {
				t.Errorf("Find err = %v, want source unavailable", err)
			}
			if errors.Is(err, context.DeadlineExceeded) {
				t.Errorf("Find err = %v, must not satisfy DeadlineExceeded", err)
			}
		})
	}
}

func TestSource_ACancelledCallerIsNotAnOutage(t *testing.T) {
	hung := make(chan struct{})
	arrived := make(chan struct{}, 1)
	slow := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		select {
		case arrived <- struct{}{}:
		default:
		}
		<-hung
	}))
	t.Cleanup(slow.Close)
	t.Cleanup(func() { close(hung) })
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go func() {
		select {
		case <-arrived:
			cancel()
		case <-ctx.Done():
		}
	}()

	_, err := ytmsearch.NewSource(clientFor(slow, time.Minute), &recordingDownloader{}).Find(ctx, vibesRequest)

	if !errors.Is(err, context.Canceled) || isSourceUnavailable(err) {
		t.Errorf("Find err = %v, want the caller's cancellation, not an outage", err)
	}
}

func TestSource_AnOutageNamesYtmsearch(t *testing.T) {
	down := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusTooManyRequests) }))
	t.Cleanup(down.Close)

	_, err := ytmsearch.NewSource(clientFor(down, time.Second), &recordingDownloader{}).Find(context.Background(), vibesRequest)

	var unavailable *domain.SourceUnavailableError
	if !errors.As(err, &unavailable) || unavailable.Source != domain.SourceName("ytmsearch") {
		t.Errorf("Find err = %v, want SourceUnavailableError{Source: ytmsearch}", err)
	}
}

func TestSource_TheArtTrackCarriesNoPlaceholderText(t *testing.T) {
	got := find(t, []ytmsearch.Song{atv("LMDLNLM6Ji8", "2021 Vibes", []string{"LUCKI"}, 125, true)}, vibesRequest)
	if len(got) != 1 {
		t.Fatalf("candidates = %v, want 1", got)
	}
	if got[0].Channel != "" || len(got[0].Categories) != 0 {
		t.Errorf("art track has channel %q and categories %q, want neither: ytmsearch observes no channel", got[0].Channel, got[0].Categories)
	}
}

func TestSource_AFeaturingBracketIsACreditNotATitle(t *testing.T) {
	song := atv("abc12345678", "Finesse (featuring Cardi B)", []string{"Bruno Mars"}, 191, false)
	req := ports.FindRequest{Title: "Finesse", Artist: "Bruno Mars", Duration: 191}

	if got := find(t, []ytmsearch.Song{song}, req); len(got) != 1 {
		t.Fatalf("candidates = %v, want the featuring art track taken as a strong hit", got)
	}
}
