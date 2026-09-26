package commands

import (
	"altune/go-api/internal/acquisition/adapters/ytdlp"
	"altune/go-api/internal/discovery/domain"
	discoveryService "altune/go-api/internal/discovery/service"
	"altune/go-api/internal/shared"
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
	results  int
	err      error
	gotUser  shared.UserId
	gotSaved bool
}

func (f *fakeJourneySearcher) Execute(_ context.Context, userId shared.UserId, _ *domain.SearchQuery, saveHistory bool) (*discoveryService.SearchOutput, error) {
	f.gotUser, f.gotSaved = userId, saveHistory
	if f.err != nil {
		return nil, f.err
	}
	return &discoveryService.SearchOutput{Results: make([]domain.SearchResult, f.results)}, nil
}

type fakeJourneyDownloader struct {
	payload   []byte
	err       error
	called    bool
	gotURL    string
	gotOutDir string
}

func (f *fakeJourneyDownloader) Download(_ context.Context, url string, outDir string) (string, error) {
	f.called, f.gotURL, f.gotOutDir = true, url, outDir
	if f.err != nil {
		return "", f.err
	}
	path := filepath.Join(outDir, "canary.mp3")
	if err := os.WriteFile(path, f.payload, 0o600); err != nil {
		return "", err
	}
	return path, nil
}

func TestRunJourneyCheck(t *testing.T) {
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
			wantErr:    "journey-check: search failed: deezer 503",
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
			wantErr:    "journey-check: download failed: HTTP Error 403",
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

			err := runJourneyCheck(context.Background(), tc.searcher, tc.downloader, &out)

			gotErr := ""
			if err != nil {
				gotErr = err.Error()
			}
			if gotErr != tc.wantErr {
				t.Errorf("runJourneyCheck error = %q, want %q", gotErr, tc.wantErr)
			}
			if out.String() != tc.wantOut {
				t.Errorf("runJourneyCheck output = %q, want %q", out.String(), tc.wantOut)
			}
		})
	}
}

func TestRunJourneyCheckSearchesAsTheSystemUserWithoutSavingHistory(t *testing.T) {
	searcher := &fakeJourneySearcher{results: 1, gotSaved: true}

	_ = runJourneyCheck(context.Background(), searcher, &fakeJourneyDownloader{payload: []byte("x")}, &bytes.Buffer{})

	if searcher.gotUser != shared.SystemUserId() || searcher.gotSaved {
		t.Errorf("Execute(user=%v, saveHistory=%v), want user=%v, saveHistory=false", searcher.gotUser, searcher.gotSaved, shared.SystemUserId())
	}
}

type deadlineRecordingSearcher struct {
	fakeJourneySearcher
	remaining   time.Duration
	hasDeadline bool
}

func (d *deadlineRecordingSearcher) Execute(ctx context.Context, userId shared.UserId, query *domain.SearchQuery, saveHistory bool) (*discoveryService.SearchOutput, error) {
	deadline, ok := ctx.Deadline()
	d.hasDeadline, d.remaining = ok, time.Until(deadline)
	return d.fakeJourneySearcher.Execute(ctx, userId, query, saveHistory)
}

func TestRunJourneyCheckBoundsTheSearchWithAMinuteDeadline(t *testing.T) {
	searcher := &deadlineRecordingSearcher{fakeJourneySearcher: fakeJourneySearcher{results: 1}}

	_ = runJourneyCheck(context.Background(), searcher, &fakeJourneyDownloader{payload: []byte("x")}, &bytes.Buffer{})

	if !searcher.hasDeadline || searcher.remaining < 50*time.Second || searcher.remaining > time.Minute {
		t.Errorf("search deadline set=%v remaining=%v, want about %v", searcher.hasDeadline, searcher.remaining, time.Minute)
	}
}

func TestRunJourneyCheckSkipsTheDownloadWhenTheSearchFails(t *testing.T) {
	downloader := &fakeJourneyDownloader{payload: []byte("x")}

	_ = runJourneyCheck(context.Background(), &fakeJourneySearcher{results: 0}, downloader, &bytes.Buffer{})

	if downloader.called {
		t.Error("Download was attempted after the search failed")
	}
}

func TestRunJourneyCheckDownloadsTheYouTubeCanaryIntoARemovedTempDir(t *testing.T) {
	for _, downloader := range []*fakeJourneyDownloader{
		{payload: []byte("audio")},
		{err: errors.New("boom")},
	} {
		_ = runJourneyCheck(context.Background(), &fakeJourneySearcher{results: 1}, downloader, &bytes.Buffer{})

		if downloader.gotURL != ytdlp.YouTubeCanary.URL {
			t.Errorf("Download url = %q, want %q", downloader.gotURL, ytdlp.YouTubeCanary.URL)
		}
		if _, err := os.Stat(downloader.gotOutDir); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("temp dir %q still present after run (download err=%v): stat err = %v", downloader.gotOutDir, downloader.err, err)
		}
	}
}

func TestRunJourneyCheckRedactsCookiePathsInTheFailure(t *testing.T) {
	downloader := &fakeJourneyDownloader{err: errors.New("yt-dlp download: exit 1 (stderr: --cookies /data/cookies.txt rejected)")}

	err := runJourneyCheck(context.Background(), &fakeJourneySearcher{results: 1}, downloader, &bytes.Buffer{})

	if err == nil || strings.Contains(err.Error(), "/data/cookies.txt") {
		t.Errorf("runJourneyCheck error = %v, want the cookie path redacted", err)
	}
}

func TestThrowawayCookieJarCopiesTheLiveJarAndRemovesTheCopy(t *testing.T) {
	livePath := filepath.Join(t.TempDir(), "cookies.txt")
	if err := os.WriteFile(livePath, []byte("# Netscape HTTP Cookie File\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	path, remove, err := throwawayCookieJar(livePath)
	if err != nil {
		t.Fatalf("throwawayCookieJar(%q) error = %v", livePath, err)
	}
	copied, readErr := os.ReadFile(path)
	remove()

	if path == livePath || readErr != nil || string(copied) != "# Netscape HTTP Cookie File\n" {
		t.Errorf("throwawayCookieJar(%q) = %q holding %q (read err %v), want a distinct copy of the live jar", livePath, path, copied, readErr)
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("copy %q still present after remove: stat err = %v", path, err)
	}
}

func TestThrowawayCookieJarRunsWithoutCookiesWhenNoJarIsConfigured(t *testing.T) {
	path, remove, err := throwawayCookieJar("")
	remove()

	if path != "" || err != nil {
		t.Errorf("throwawayCookieJar(\"\") = %q, %v, want \"\", nil", path, err)
	}
}

func TestThrowawayCookieJarFailsWhenTheLiveJarIsMissing(t *testing.T) {
	_, remove, err := throwawayCookieJar(filepath.Join(t.TempDir(), "absent.txt"))
	remove()

	if !errors.Is(err, os.ErrNotExist) {
		t.Errorf("throwawayCookieJar(absent) error = %v, want not-exist", err)
	}
}

func TestRunJourneyCheckRedactsSecretsInASearchFailure(t *testing.T) {
	searcher := &fakeJourneySearcher{err: errors.New("GET https://api.example/search?q=x&access_token=sk_live_abc123: 401")}

	err := runJourneyCheck(context.Background(), searcher, &fakeJourneyDownloader{payload: []byte("x")}, &bytes.Buffer{})

	if err == nil || !strings.HasPrefix(err.Error(), "journey-check: search failed: ") || strings.Contains(err.Error(), "sk_live_abc123") {
		t.Errorf("runJourneyCheck error = %v, want a search failure with the token redacted", err)
	}
}

type missingFileDownloader struct{}

func (missingFileDownloader) Download(_ context.Context, _ string, outDir string) (string, error) {
	return filepath.Join(outDir, "never-written.mp3"), nil
}

func TestRunJourneyCheckFailsWhenTheDownloadReportsAFileThatIsNotThere(t *testing.T) {
	err := runJourneyCheck(context.Background(), &fakeJourneySearcher{results: 1}, missingFileDownloader{}, &bytes.Buffer{})

	if err == nil || !strings.HasPrefix(err.Error(), "journey-check: download failed: ") {
		t.Errorf("runJourneyCheck error = %v, want a download failure", err)
	}
}

func TestRunJourneyCheckPassesAOneByteDownload(t *testing.T) {
	var out bytes.Buffer

	err := runJourneyCheck(context.Background(), &fakeJourneySearcher{results: 1}, &fakeJourneyDownloader{payload: []byte("x")}, &out)

	if err != nil || out.String() != "journey-check: search ok (1 results)\njourney-check: download ok (1 bytes)\n" {
		t.Errorf("runJourneyCheck = %v, output %q; want pass with 1 result and 1 byte", err, out.String())
	}
}
