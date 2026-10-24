package catalogpin

import (
	"altune/go-api/internal/acquisition/ports"
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
)

type recordingFetcher struct {
	gotURL string
	gotDir string
	reason string
}

func (f *recordingFetcher) MarkUnplayable(_ context.Context, candidates []ports.AudioCandidate) {
	for i := range candidates {
		candidates[i].Unplayable = f.reason
	}
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
		Kind: ports.KindCatalogPin,
	}
	if got[0].URL != want.URL || got[0].Title != want.Title || !got[0].IsResolved() || got[0].Kind != want.Kind ||
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

func TestFind_EmitsTheKnownSource(t *testing.T) {
	const known = "https://music.youtube.com/watch?v=NxTPVbk-FZQ"
	src := NewSource(&recordingFetcher{})
	req := requestPinning()
	req.KnownSourceURL = known

	got, err := src.Find(context.Background(), req)

	if err != nil || len(got) != 1 {
		t.Fatalf("Find = %+v, %v; want the known source", got, err)
	}
	if got[0].URL != known || !got[0].IsResolved() || got[0].Kind != ports.KindCatalogPin {
		t.Errorf("candidate = %+v, want the known source as a resolved catalog pin", got[0])
	}

	picked := requestPinning(ports.CatalogSource{Provider: "soundcloud", ExternalID: "1", URL: pickedUpload})
	picked.KnownSourceURL = known
	withPick, err := src.Find(context.Background(), picked)
	if err != nil || len(withPick) != 1 || withPick[0].URL != pickedUpload {
		t.Errorf("with a picked upload Find = %+v, %v; want only the user's pick", withPick, err)
	}

	for _, unusable := range []string{"", "file:///etc/passwd", "not a url"} {
		req.KnownSourceURL = unusable
		none, err := src.Find(context.Background(), req)
		if err != nil || len(none) != 0 {
			t.Errorf("known source %q: Find = %+v, %v; want nothing", unusable, none, err)
		}
	}
}

type inspectingFetcher struct {
	recordingFetcher
	formatURL string
	err       error
}

func (f *inspectingFetcher) MarkUnplayable(_ context.Context, candidates []ports.AudioCandidate) {
	if f.err != nil {
		return
	}
	if !strings.Contains(f.formatURL, "preview") {
		return
	}
	for i := range candidates {
		candidates[i].Unplayable = ports.UnplayablePreview
	}
}

func findPinned(t *testing.T, f audioFetcher) []ports.AudioCandidate {
	t.Helper()
	got, err := NewSource(f).Find(context.Background(), requestPinning(
		ports.CatalogSource{Provider: "soundcloud", URL: pickedUpload}))
	if err != nil || len(got) != 1 {
		t.Fatalf("Find = %v, %v; want one candidate", got, err)
	}
	return got
}

func TestFind_MarksAPreviewPinUnplayable(t *testing.T) {
	got := findPinned(t, &inspectingFetcher{formatURL: "https://cf-media.sndcdn.com/x_preview.mp3"})

	if got[0].Unplayable != ports.UnplayablePreview {
		t.Fatalf("Unplayable = %q, want %q", got[0].Unplayable, ports.UnplayablePreview)
	}
}

func TestFind_LeavesAFullLengthPinPlayable(t *testing.T) {
	got := findPinned(t, &inspectingFetcher{formatURL: "https://cf-media.sndcdn.com/x_full.mp3"})

	if got[0].Unplayable != "" {
		t.Fatalf("Unplayable = %q, want empty", got[0].Unplayable)
	}
}

func TestFind_KeepsAPinPlayableWhenInspectionFails(t *testing.T) {
	got := findPinned(t, &inspectingFetcher{formatURL: "preview", err: errors.New("yt-dlp timed out")})

	if got[0].Unplayable != "" {
		t.Fatalf("Unplayable = %q, want empty", got[0].Unplayable)
	}
}

func TestFind_RebuildsAPixeldrainPinFromItsFileID(t *testing.T) {
	src := NewSource(&recordingFetcher{})

	got, err := src.Find(context.Background(), requestPinning(
		ports.CatalogSource{Provider: "pixeldrain", ExternalID: "ozSpSjn7", URL: "https://evil.example/x"},
	))

	want := "https://pixeldrain.com/api/file/ozSpSjn7?download"
	if err != nil || len(got) != 1 || got[0].URL != want || got[0].Kind != ports.KindCatalogPin {
		t.Fatalf("Find = %+v, %v; want one pin at %s", got, err, want)
	}
}

func TestFind_PrefersThePixeldrainFileOverASoundCloudPin(t *testing.T) {
	src := NewSource(&recordingFetcher{})

	got, _ := src.Find(context.Background(), requestPinning(
		ports.CatalogSource{Provider: "soundcloud", ExternalID: "1", URL: pickedUpload},
		ports.CatalogSource{Provider: "pixeldrain", ExternalID: "ozSpSjn7"},
	))

	if len(got) != 1 || got[0].URL != "https://pixeldrain.com/api/file/ozSpSjn7?download" {
		t.Fatalf("Find = %+v, want only the pixeldrain file", got)
	}
}

func TestFind_DropsAMalformedPixeldrainID(t *testing.T) {
	src := NewSource(&recordingFetcher{})

	got, _ := src.Find(context.Background(), requestPinning(
		ports.CatalogSource{Provider: "pixeldrain", ExternalID: "../etc"},
	))

	if len(got) != 0 {
		t.Fatalf("Find = %+v, want no candidate for a malformed id", got)
	}
}

func TestFind_RebuildsEveryFileHostPinFromItsID(t *testing.T) {
	cases := map[string]ports.CatalogSource{
		"https://api.pillows.su/api/download/84310b5d2c21df85e4570aec299d1ead": {Provider: "pillows", ExternalID: "84310b5d2c21df85e4570aec299d1ead", URL: "https://evil.example/x"},
	}
	for want, pin := range cases {
		got, err := NewSource(&recordingFetcher{}).Find(context.Background(), requestPinning(pin))
		if err != nil || len(got) != 1 || got[0].URL != want {
			t.Errorf("Find(%s) = %+v, %v; want one pin at %s", pin.Provider, got, err, want)
		}
	}
}
