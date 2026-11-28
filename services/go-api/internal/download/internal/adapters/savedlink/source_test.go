package savedlink_test

import (
	"altune/go-api/internal/download/downloadtest"
	"altune/go-api/internal/download/internal/adapters/savedlink"
	"altune/go-api/internal/download/internal/domain"
	"altune/go-api/internal/shared/logging"
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

func (f *recordingFetcher) MarkUnplayable(_ context.Context, candidates []domain.AudioCandidate) {
	for i := range candidates {
		candidates[i].Unplayable = f.reason
	}
}

func (f *recordingFetcher) Download(_ context.Context, url, outDir string) (domain.FetchedAudio, error) {
	f.gotURL, f.gotDir = url, outDir
	return domain.FetchedAudio{Path: outDir + "/track.mp3"}, nil
}

const unplayablePreview = "preview"

const pickedUpload = "https://soundcloud.com/vvhhhhkk/sophie-ellis-bextor-murder-on-the-dancefloor-tmj"

func requestPinning(sources ...domain.CatalogSource) domain.FindRequest {
	return domain.FindRequest{Title: "Murder On The Dancefloor", Artist: "Sophie Ellis-Bextor", CatalogSources: sources}
}

func TestFind_EmitsThePickedSoundCloudUpload(t *testing.T) {
	src := newSource(t, &recordingFetcher{}, savedlink.NoPageReader())

	got, err := src.Find(context.Background(), requestPinning(
		domain.CatalogSource{Provider: "youtube", ExternalID: "hAx6mYeC6pY", URL: "https://music.youtube.com/watch?v=hAx6mYeC6pY"},
		domain.CatalogSource{Provider: "soundcloud", ExternalID: "1", URL: pickedUpload},
	))
	if err != nil || len(got) != 1 {
		t.Fatalf("Find = %+v, %v; want the one picked upload", got, err)
	}
	want := downloadtest.Candidate(
		pickedUpload,
		downloadtest.WithTitle("Murder On The Dancefloor"),
		downloadtest.WithArtists([]string{"Sophie Ellis-Bextor"}),
		downloadtest.WithKind(domain.KindCatalogPin),
	)
	if got[0].URL != want.URL || got[0].Title != want.Title || !got[0].IsResolved() || got[0].Kind != want.Kind ||
		len(got[0].Artists) != 1 || got[0].Artists[0] != want.Artists[0] {
		t.Errorf("candidate = %+v, want %+v", got[0], want)
	}

	none, err := src.Find(context.Background(), requestPinning(
		domain.CatalogSource{Provider: "youtube", ExternalID: "hAx6mYeC6pY"},
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
			logs := logging.CaptureText(t, slog.LevelDebug)

			got, err := newSource(t, &recordingFetcher{}, savedlink.NoPageReader()).Find(context.Background(),
				requestPinning(domain.CatalogSource{Provider: "soundcloud", ExternalID: "1", URL: tt.url}))

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

	fetched, err := newSource(t, fetcher, savedlink.NoPageReader()).Fetch(context.Background(), downloadtest.Candidate(pickedUpload), "/tmp/out")

	if err != nil || fetcher.gotURL != pickedUpload || fetched.Path != "/tmp/out/track.mp3" {
		t.Errorf("Fetch = %+v, %v; downloaded %q", fetched, err, fetcher.gotURL)
	}
}

func TestName(t *testing.T) {
	if got := newSource(t, &recordingFetcher{}, savedlink.NoPageReader()).Name(); got != savedlink.SourceName {
		t.Errorf("Name() = %q, want %q", got, savedlink.SourceName)
	}
}

func TestNewSource_RefusesAMissingFetcherOrPageReader(t *testing.T) {
	if src, err := savedlink.NewSource(nil, savedlink.NoPageReader()); !errors.Is(err, savedlink.ErrFetcherRequired) || src != nil {
		t.Errorf("nil fetcher: got (%v, %v), want ErrSavedLinkFetcherRequired", src, err)
	}
	if src, err := savedlink.NewSource(&recordingFetcher{}, nil); !errors.Is(err, savedlink.ErrPageReaderRequired) || src != nil {
		t.Errorf("nil page reader: got (%v, %v), want ErrSavedLinkPageReaderRequired", src, err)
	}
}

type fetchesAudio interface {
	Download(ctx context.Context, url, outDir string) (domain.FetchedAudio, error)
	MarkUnplayable(ctx context.Context, candidates []domain.AudioCandidate)
}

type readsPages interface {
	ReadPage(ctx context.Context, pageURL string) ([]byte, error)
}

func newSource(t testing.TB, fetcher fetchesAudio, pages readsPages) *savedlink.Source {
	t.Helper()
	src, err := savedlink.NewSource(fetcher, pages)
	if err != nil {
		t.Fatalf("NewSource: %v", err)
	}
	return src
}

func TestFind_EmitsTheKnownSource(t *testing.T) {
	const known = "https://music.youtube.com/watch?v=NxTPVbk-FZQ"
	src := newSource(t, &recordingFetcher{}, savedlink.NoPageReader())
	req := requestPinning()
	req.KnownSourceURL = known

	got, err := src.Find(context.Background(), req)

	if err != nil || len(got) != 1 {
		t.Fatalf("Find = %+v, %v; want the known source", got, err)
	}
	if got[0].URL != known || !got[0].IsResolved() || got[0].Kind != domain.KindCatalogPin {
		t.Errorf("candidate = %+v, want the known source as a resolved catalog pin", got[0])
	}

	picked := requestPinning(domain.CatalogSource{Provider: "soundcloud", ExternalID: "1", URL: pickedUpload})
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

func (f *inspectingFetcher) MarkUnplayable(_ context.Context, candidates []domain.AudioCandidate) {
	if f.err != nil {
		return
	}
	if !strings.Contains(f.formatURL, "preview") {
		return
	}
	for i := range candidates {
		candidates[i].Unplayable = unplayablePreview
	}
}

func findPinned(t *testing.T, f *inspectingFetcher) []domain.AudioCandidate {
	t.Helper()
	got, err := newSource(t, f, savedlink.NoPageReader()).Find(context.Background(), requestPinning(
		domain.CatalogSource{Provider: "soundcloud", URL: pickedUpload}))
	if err != nil || len(got) != 1 {
		t.Fatalf("Find = %v, %v; want one candidate", got, err)
	}
	return got
}

func TestFind_MarksAPreviewPinUnplayable(t *testing.T) {
	got := findPinned(t, &inspectingFetcher{formatURL: "https://cf-media.sndcdn.com/x_preview.mp3"})

	if got[0].Unplayable != unplayablePreview {
		t.Fatalf("Unplayable = %q, want %q", got[0].Unplayable, unplayablePreview)
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
	src := newSource(t, &recordingFetcher{}, savedlink.NoPageReader())

	got, err := src.Find(context.Background(), requestPinning(
		domain.CatalogSource{Provider: "pixeldrain", ExternalID: "ozSpSjn7", URL: "https://evil.example/x"},
	))

	want := "https://pixeldrain.com/api/file/ozSpSjn7?download"
	if err != nil || len(got) != 1 || got[0].URL != want || got[0].Kind != domain.KindCatalogPin {
		t.Fatalf("Find = %+v, %v; want one pin at %s", got, err, want)
	}
}

func TestFind_PrefersThePixeldrainFileOverASoundCloudPin(t *testing.T) {
	src := newSource(t, &recordingFetcher{}, savedlink.NoPageReader())

	got, _ := src.Find(context.Background(), requestPinning(
		domain.CatalogSource{Provider: "soundcloud", ExternalID: "1", URL: pickedUpload},
		domain.CatalogSource{Provider: "pixeldrain", ExternalID: "ozSpSjn7"},
	))

	if len(got) != 1 || got[0].URL != "https://pixeldrain.com/api/file/ozSpSjn7?download" {
		t.Fatalf("Find = %+v, want only the pixeldrain file", got)
	}
}

func TestFind_DropsAMalformedPixeldrainID(t *testing.T) {
	src := newSource(t, &recordingFetcher{}, savedlink.NoPageReader())

	got, _ := src.Find(context.Background(), requestPinning(
		domain.CatalogSource{Provider: "pixeldrain", ExternalID: "../etc"},
	))

	if len(got) != 0 {
		t.Fatalf("Find = %+v, want no candidate for a malformed id", got)
	}
}

func TestFind_RebuildsEveryFileHostPinFromItsID(t *testing.T) {
	cases := map[string]domain.CatalogSource{
		"https://api.pillows.su/api/download/84310b5d2c21df85e4570aec299d1ead": {Provider: "pillows", ExternalID: "84310b5d2c21df85e4570aec299d1ead", URL: "https://evil.example/x"},
	}
	for want, pin := range cases {
		got, err := newSource(t, &recordingFetcher{}, savedlink.NoPageReader()).Find(context.Background(), requestPinning(pin))
		if err != nil || len(got) != 1 || got[0].URL != want {
			t.Errorf("Find(%s) = %+v, %v; want one pin at %s", pin.Provider, got, err, want)
		}
	}
}

type fakePages struct {
	body string
	err  error
	got  string
}

func (f *fakePages) ReadPage(_ context.Context, pageURL string) ([]byte, error) {
	f.got = pageURL
	return []byte(f.body), f.err
}

func TestFind_ResolvesAnImgurggPinToItsCDNFile(t *testing.T) {
	pages := &fakePages{body: `<a href="https://evil.example/0Q3ydhk-a.mp3"></a><audio src="https://b.imgur.gg/0Q3ydhk-Got_A_New_One.mp3">`}
	got, err := newSource(t, &recordingFetcher{}, pages).Find(context.Background(), requestPinning(
		domain.CatalogSource{Provider: "imgurgg", ExternalID: "0Q3ydhk", URL: "https://evil.example/x"},
	))
	want := "https://b.imgur.gg/0Q3ydhk-Got_A_New_One.mp3"
	if err != nil || len(got) != 1 || got[0].URL != want || pages.got != "https://imgur.gg/f/0Q3ydhk" {
		t.Fatalf("Find = %+v, %v, page read %q; want one pin at %s", got, err, pages.got, want)
	}
}

func TestFind_DropsAnImgurggPinWhosePageHasNoCDNFile(t *testing.T) {
	for name, src := range map[string]*savedlink.Source{
		"foreign file": newSource(t, &recordingFetcher{}, &fakePages{body: `<audio src="https://evil.example/0Q3ydhk-a.mp3">`}),
		"page error":   newSource(t, &recordingFetcher{}, &fakePages{err: errors.New("down")}),
		"no reader":    newSource(t, &recordingFetcher{}, savedlink.NoPageReader()),
	} {
		got, _ := src.Find(context.Background(), requestPinning(domain.CatalogSource{Provider: "imgurgg", ExternalID: "0Q3ydhk"}))
		if len(got) != 0 {
			t.Errorf("%s: Find = %+v, want no candidate", name, got)
		}
	}
}
