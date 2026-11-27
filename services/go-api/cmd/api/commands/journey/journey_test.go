package journey_test

import (
	"altune/go-api/cmd/api/commands/journey"
	"altune/go-api/internal/download"
	"altune/go-api/internal/search"
	"altune/go-api/internal/shared/background"
	"altune/go-api/internal/shared/config"
	"altune/go-api/internal/shared/ids"
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type fakeJourneySearcher struct {
	results int
	err     error
	gotUser ids.UserId
}

func (f *fakeJourneySearcher) Execute(_ context.Context, userId ids.UserId, _ *search.SearchQuery) (*search.SearchOutput, error) {
	f.gotUser = userId
	if f.err != nil {
		return nil, f.err
	}
	return &search.SearchOutput{Results: make([]search.SearchResult, f.results)}, nil
}

type fakeJourneyDownloader struct {
	payload   []byte
	err       error
	called    bool
	gotURL    string
	gotOutDir string
}

func (f *fakeJourneyDownloader) Download(_ context.Context, url string, outDir string) (download.FetchedAudio, error) {
	f.called, f.gotURL, f.gotOutDir = true, url, outDir
	if f.err != nil {
		return download.FetchedAudio{}, f.err
	}
	path := filepath.Join(outDir, "canary.mp3")
	if err := os.WriteFile(path, f.payload, 0o600); err != nil {
		return download.FetchedAudio{}, err
	}
	return download.FetchedAudio{Path: path}, nil
}

func TestRun(t *testing.T) {
	cases := []struct {
		name       string
		searcher   *fakeJourneySearcher
		downloader *fakeJourneyDownloader
		wantErr    string
		wantOut    string
	}{
		{
			name:       "passes when the search has results and the download is non-empty",
			searcher:   &fakeJourneySearcher{results: 3},
			downloader: &fakeJourneyDownloader{payload: []byte("ID3audio")},
			wantOut:    "journey-check: search ok (3 results)\njourney-check: download ok (8 bytes)\n",
		},
		{
			name:       "fails when the search errors",
			searcher:   &fakeJourneySearcher{err: errors.New("deezer 503")},
			downloader: &fakeJourneyDownloader{payload: []byte("x")},
			wantErr:    "journey-check: search failed: execute: deezer 503",
		},
		{
			name:       "fails when the search returns zero results",
			searcher:   &fakeJourneySearcher{results: 0},
			downloader: &fakeJourneyDownloader{payload: []byte("x")},
			wantErr:    "journey-check: search failed: no results",
		},
		{
			name:       "fails when the download errors",
			searcher:   &fakeJourneySearcher{results: 1},
			downloader: &fakeJourneyDownloader{err: errors.New("HTTP Error 403")},
			wantErr:    "journey-check: download failed: download: HTTP Error 403",
			wantOut:    "journey-check: search ok (1 results)\n",
		},
		{
			name:       "fails when the download produces an empty file",
			searcher:   &fakeJourneySearcher{results: 1},
			downloader: &fakeJourneyDownloader{payload: nil},
			wantErr:    "journey-check: download failed: downloaded file is empty",
			wantOut:    "journey-check: search ok (1 results)\n",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var out bytes.Buffer

			err := journey.Run(context.Background(), tc.searcher, tc.downloader, &out)

			gotErr := ""
			if err != nil {
				gotErr = err.Error()
			}
			if gotErr != tc.wantErr {
				t.Errorf("Run error = %q, want %q", gotErr, tc.wantErr)
			}
			if out.String() != tc.wantOut {
				t.Errorf("Run output = %q, want %q", out.String(), tc.wantOut)
			}
		})
	}
}

func TestRunSearchesAsTheSystemUser(t *testing.T) {
	searcher := &fakeJourneySearcher{results: 1}

	_ = journey.Run(context.Background(), searcher, &fakeJourneyDownloader{payload: []byte("x")}, &bytes.Buffer{})

	if searcher.gotUser != ids.SystemUserId() {
		t.Errorf("Execute(user=%v), want user=%v", searcher.gotUser, ids.SystemUserId())
	}
}

type deadlineRecordingSearcher struct {
	fakeJourneySearcher
	remaining   time.Duration
	hasDeadline bool
}

func (d *deadlineRecordingSearcher) Execute(ctx context.Context, userId ids.UserId, query *search.SearchQuery) (*search.SearchOutput, error) {
	deadline, ok := ctx.Deadline()
	d.hasDeadline, d.remaining = ok, time.Until(deadline)
	return d.fakeJourneySearcher.Execute(ctx, userId, query)
}

func TestRunBoundsTheSearchWithAMinuteDeadline(t *testing.T) {
	searcher := &deadlineRecordingSearcher{fakeJourneySearcher: fakeJourneySearcher{results: 1}}

	_ = journey.Run(context.Background(), searcher, &fakeJourneyDownloader{payload: []byte("x")}, &bytes.Buffer{})

	if !searcher.hasDeadline || searcher.remaining < 50*time.Second || searcher.remaining > time.Minute {
		t.Errorf("search deadline set=%v remaining=%v, want about %v", searcher.hasDeadline, searcher.remaining, time.Minute)
	}
}

func TestRunSkipsTheDownloadWhenTheSearchFails(t *testing.T) {
	downloader := &fakeJourneyDownloader{payload: []byte("x")}

	_ = journey.Run(context.Background(), &fakeJourneySearcher{results: 0}, downloader, &bytes.Buffer{})

	if downloader.called {
		t.Error("Download was attempted after the search failed")
	}
}

func TestRunDownloadsTheYouTubeCanaryIntoARemovedTempDir(t *testing.T) {
	for _, downloader := range []*fakeJourneyDownloader{
		{payload: []byte("audio")},
		{err: errors.New("boom")},
	} {
		_ = journey.Run(context.Background(), &fakeJourneySearcher{results: 1}, downloader, &bytes.Buffer{})

		if downloader.gotURL != download.YouTubeCanary.URL {
			t.Errorf("Download url = %q, want %q", downloader.gotURL, download.YouTubeCanary.URL)
		}
		if _, err := os.Stat(downloader.gotOutDir); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("temp dir %q still present after run (download err=%v): stat err = %v", downloader.gotOutDir, downloader.err, err)
		}
	}
}

func TestRunRedactsCookiePathsInTheFailure(t *testing.T) {
	downloader := &fakeJourneyDownloader{err: errors.New("yt-dlp download: exit 1 (stderr: --cookies /data/cookies.txt rejected)")}

	err := journey.Run(context.Background(), &fakeJourneySearcher{results: 1}, downloader, &bytes.Buffer{})

	if err == nil || strings.Contains(err.Error(), "/data/cookies.txt") {
		t.Errorf("Run error = %v, want the cookie path redacted", err)
	}
}

func TestRunRedactsSecretsInASearchFailure(t *testing.T) {
	searcher := &fakeJourneySearcher{err: errors.New("GET https://api.example/search?q=x&access_token=sk_live_abc123: 401")}

	err := journey.Run(context.Background(), searcher, &fakeJourneyDownloader{payload: []byte("x")}, &bytes.Buffer{})

	if err == nil || !strings.HasPrefix(err.Error(), "journey-check: search failed: ") || strings.Contains(err.Error(), "sk_live_abc123") {
		t.Errorf("Run error = %v, want a search failure with the token redacted", err)
	}
}

type missingFileDownloader struct{}

func (missingFileDownloader) Download(_ context.Context, _ string, outDir string) (download.FetchedAudio, error) {
	return download.FetchedAudio{Path: filepath.Join(outDir, "never-written.mp3")}, nil
}

func TestRunFailsWhenTheDownloadReportsAFileThatIsNotThere(t *testing.T) {
	err := journey.Run(context.Background(), &fakeJourneySearcher{results: 1}, missingFileDownloader{}, &bytes.Buffer{})

	if err == nil || !strings.HasPrefix(err.Error(), "journey-check: download failed: ") {
		t.Errorf("Run error = %v, want a download failure", err)
	}
}

func TestRunPassesAOneByteDownload(t *testing.T) {
	var out bytes.Buffer

	err := journey.Run(context.Background(), &fakeJourneySearcher{results: 1}, &fakeJourneyDownloader{payload: []byte("x")}, &out)

	if err != nil || out.String() != "journey-check: search ok (1 results)\njourney-check: download ok (1 bytes)\n" {
		t.Errorf("Run = %v, output %q; want pass with 1 result and 1 byte", err, out.String())
	}
}

func TestRankingOnlySearcherComesFromTheDiscoveryTable(t *testing.T) {
	searcher, err := journey.RankingOnlySearcher(t.Context(), &config.Config{BackgroundMaxInFlight: 64}, nil)
	if err != nil {
		t.Fatalf("RankingOnlySearcher: %v", err)
	}
	if _, ok := searcher.(*search.SearchService); !ok {
		t.Errorf("searcher = %T, want the table's ranking-only search service", searcher)
	}
}

func TestRankingOnlySearcherRefusesABackgroundCapBelowOne(t *testing.T) {
	_, err := journey.RankingOnlySearcher(t.Context(), &config.Config{}, nil)
	if !errors.Is(err, background.ErrTrackerLimit) {
		t.Fatalf("RankingOnlySearcher = %v, want runloop.ErrTrackerLimit", err)
	}
}
