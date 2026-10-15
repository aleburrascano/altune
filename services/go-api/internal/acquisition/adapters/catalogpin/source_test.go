package catalogpin

import (
	"altune/go-api/internal/acquisition/ports"
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"
)

type recordingFetcher struct {
	gotURL string
	gotDir string
}

func (f *recordingFetcher) Download(_ context.Context, url, outDir string) (ports.FetchedAudio, error) {
	f.gotURL, f.gotDir = url, outDir
	return ports.FetchedAudio{Path: outDir + "/track.mp3"}, nil
}

const pickedUpload = "https://soundcloud.com/vvhhhhkk/sophie-ellis-bextor-murder-on-the-dancefloor-tmj"

func requestPinning(sources ...ports.CatalogSource) ports.FindRequest {
	return ports.FindRequest{Title: "Murder On The Dancefloor", Artist: "Sophie Ellis-Bextor", CatalogSources: sources}
}

func capturedLog(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return &buf
}

func TestFind_EmitsThePickedSoundCloudUpload(t *testing.T) {
	src := NewSource(&recordingFetcher{})

	got, err := src.Find(context.Background(), requestPinning(
		ports.CatalogSource{Provider: "youtube", ExternalID: "hAx6mYeC6pY", URL: "https://music.youtube.com/watch?v=hAx6mYeC6pY"},
		ports.CatalogSource{Provider: "soundcloud", ExternalID: "1", URL: pickedUpload},
	))
	if err != nil || len(got) != 1 {
		t.Fatalf("Find = %+v, %v; want the one picked upload", got, err)
	}
	want := ports.AudioCandidate{
		URL: pickedUpload, Title: "Murder On The Dancefloor", Artists: []string{"Sophie Ellis-Bextor"},
		Resolved: true, Kind: ports.KindCatalogPin,
	}
	if got[0].URL != want.URL || got[0].Title != want.Title || !got[0].Resolved || got[0].Kind != want.Kind ||
		len(got[0].Artists) != 1 || got[0].Artists[0] != want.Artists[0] {
		t.Errorf("candidate = %+v, want %+v", got[0], want)
	}

	none, err := src.Find(context.Background(), requestPinning(
		ports.CatalogSource{Provider: "youtube", ExternalID: "hAx6mYeC6pY"},
	))
	if err != nil || len(none) != 0 {
		t.Errorf("without a soundcloud pin Find = %+v, %v; want nothing", none, err)
	}
}

func TestFind_ASoundCloudPinWithoutAUsableURLIsSkippedAndLogged(t *testing.T) {
	tests := []struct {
		name    string
		url     string
		wantLog string
	}{
		{"no url", "", "acquisition.catalogpin_url_missing"},
		{"youtube host", "https://www.youtube.com/watch?v=hAx6mYeC6pY", "acquisition.catalogpin_url_rejected"},
		{"lookalike host", "https://soundcloud.com.evil.example/a/b", "acquisition.catalogpin_url_rejected"},
		{"plain http", "http://soundcloud.com/a/b", "acquisition.catalogpin_url_rejected"},
		{"not a url", "::not a url", "acquisition.catalogpin_url_rejected"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logs := capturedLog(t)

			got, err := NewSource(&recordingFetcher{}).Find(context.Background(),
				requestPinning(ports.CatalogSource{Provider: "soundcloud", ExternalID: "1", URL: tt.url}))

			if err != nil || len(got) != 0 {
				t.Fatalf("Find = %+v, %v; an unusable pin is skipped, not an error and not a candidate", got, err)
			}
			if !strings.Contains(logs.String(), tt.wantLog) {
				t.Errorf("log = %q, want %s", logs.String(), tt.wantLog)
			}
		})
	}
}

func TestFetch_DownloadsThePickedURL(t *testing.T) {
	fetcher := &recordingFetcher{}

	fetched, err := NewSource(fetcher).Fetch(context.Background(), ports.AudioCandidate{URL: pickedUpload}, "/tmp/out")

	if err != nil || fetcher.gotURL != pickedUpload || fetched.Path != "/tmp/out/track.mp3" {
		t.Errorf("Fetch = %+v, %v; downloaded %q", fetched, err, fetcher.gotURL)
	}
}

func TestName(t *testing.T) {
	if got := NewSource(nil).Name(); got != SourceName {
		t.Errorf("Name() = %q, want %q", got, SourceName)
	}
}
