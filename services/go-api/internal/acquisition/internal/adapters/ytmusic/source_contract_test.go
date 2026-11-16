package ytmusic_test

import (
	"altune/go-api/internal/acquisition"
	"altune/go-api/internal/acquisition/internal/adapters/ytmsearch"
	"altune/go-api/internal/acquisition/internal/adapters/ytmusic"
	"context"
	"errors"
	"os"
	"slices"
	"testing"
)

type stubLookup struct {
	watch ytmsearch.Watch
	err   error
}

func (l stubLookup) Watch(context.Context, string) (ytmsearch.Watch, error) {
	return l.watch, l.err
}

func observedFixture(t *testing.T, videoID string) stubLookup {
	t.Helper()
	raw, err := os.ReadFile("../ytmsearch/testdata/next_" + videoID + ".json")
	if err != nil {
		t.Fatal(err)
	}
	watch, err := ytmsearch.ParseWatch(raw)
	if err != nil {
		t.Fatal(err)
	}
	return stubLookup{watch: watch}
}

func pinnedTo(title, videoID string) acquisition.FindRequest {
	return acquisition.FindRequest{
		Title:    title,
		Artist:   "slayr",
		Identity: acquisition.RecordingIdentity{Duration: 151, Sources: []acquisition.ProviderRef{{Provider: "youtube", ExternalID: videoID}}},
	}
}

func TestFind_TheCandidateCarriesWhatThePinnedVideoIs(t *testing.T) {
	src := ytmusic.NewSource(&recordingFetcher{}, observedFixture(t, "88oFlVnMEsc"))
	got, err := src.Find(context.Background(), pinnedTo("remember them days", "88oFlVnMEsc"))
	if err != nil || len(got) != 1 {
		t.Fatalf("Find = %+v, %v", got, err)
	}
	if got[0].Title != "2021 Vibes" || got[0].Duration != 0 || !slices.Contains(got[0].Artists, "LUCKI") {
		t.Errorf("candidate = %+v, want the pinned video's own title and no copied length", got[0])
	}
}

func TestFind_AFailedLookupIsAnUnresolvedUpload(t *testing.T) {
	src := ytmusic.NewSource(&recordingFetcher{}, stubLookup{err: errors.New("ytm down")})
	got, err := src.Find(context.Background(), pinnedTo("remember them days", "88oFlVnMEsc"))
	if err != nil || len(got) != 1 {
		t.Fatalf("Find = %+v, %v", got, err)
	}
	if got[0].Kind != acquisition.KindUpload || got[0].IsResolved() {
		t.Errorf("candidate = %+v, want an unresolved upload", got[0])
	}
}

func TestFind_AFailedLookupLeavesTheFactsUnknown(t *testing.T) {
	src := ytmusic.NewSource(&recordingFetcher{}, stubLookup{err: errors.New("ytm down")})
	got, err := src.Find(context.Background(), pinnedTo("remember them days", "88oFlVnMEsc"))
	if err != nil || len(got) != 1 || got[0].Title != "" || got[0].Duration != 0 {
		t.Fatalf("Find = %+v, %v", got, err)
	}
}

func TestCandidateForWatch_OnlyAnObservedOfficialVideoIsMarkedOfficial(t *testing.T) {
	cases := map[ytmsearch.VideoType]bool{
		ytmsearch.VideoTypeOMV: true,
		ytmsearch.VideoTypeATV: false,
		ytmsearch.VideoTypeUGC: false,
		"":                     false,
	}
	for videoType, want := range cases {
		got := ytmusic.CandidateForWatch("88oFlVnMEsc", ytmsearch.Watch{Type: videoType}, false)
		if got.ObservedOMV != want {
			t.Errorf("type %q: ObservedOMV = %v, want %v", videoType, got.ObservedOMV, want)
		}
	}
}

type recordingFetcher struct {
	gotURL string
	gotDir string
}

func (f *recordingFetcher) Download(_ context.Context, url, outDir string) (acquisition.FetchedAudio, error) {
	f.gotURL, f.gotDir = url, outDir
	return acquisition.FetchedAudio{Path: outDir + "/track.mp3"}, nil
}

func TestFind_ResolvesFromTheIdentityVideoID(t *testing.T) {
	src := ytmusic.NewSource(&recordingFetcher{}, stubLookup{})
	req := acquisition.FindRequest{
		Title:  "Blinding Lights",
		Artist: "The Weeknd",
		Identity: acquisition.RecordingIdentity{
			Duration: 200,
			Sources:  []acquisition.ProviderRef{{Provider: "youtube", ExternalID: "dQw4w9WgXcQ"}},
		},
	}

	got, err := src.Find(context.Background(), req)
	if err != nil {
		t.Fatalf("Find: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("candidates = %d, want 1", len(got))
	}
	if got[0].URL != "https://music.youtube.com/watch?v=dQw4w9WgXcQ" {
		t.Errorf("URL = %q", got[0].URL)
	}
	if got[0].IsResolved() {
		t.Error("a resolver video whose lookup observed no type must stay unresolved so it faces the resemblance gate")
	}
	if got[0].Duration != 0 || got[0].Title != "" {
		t.Errorf("candidate = %+v, want no facts copied from the request", got[0])
	}
}

func TestFind_RejectsAVideoIDThatIsNotAWatchID(t *testing.T) {
	hostile := []string{
		"abc&list=1",
		"abc123",
		"dQw4w9WgXcQextra",
		"dQw4w9WgXc/",
		"dQw4w9WgXcQ\n",
		"../../../etc/pw",
		"dQw4w9WgX Q",
	}

	for _, videoID := range hostile {
		t.Run(videoID, func(t *testing.T) {
			src := ytmusic.NewSource(&recordingFetcher{}, stubLookup{})
			got, err := src.Find(context.Background(), acquisition.FindRequest{
				Title: "Song",
				Identity: acquisition.RecordingIdentity{
					Sources: []acquisition.ProviderRef{{Provider: "youtube", ExternalID: videoID}},
				},
			})
			if err != nil {
				t.Fatalf("an unusable video id is not an error, got %v", err)
			}
			if len(got) != 0 {
				t.Errorf("id %q must not become a fetch target; got %+v", videoID, got)
			}
		})
	}
}

func TestFind_NoIdentityMeansNoCandidates(t *testing.T) {
	src := ytmusic.NewSource(&recordingFetcher{}, stubLookup{})

	got, err := src.Find(context.Background(), acquisition.FindRequest{Title: "T", Artist: "A"})
	if err != nil {
		t.Fatalf("an unresolvable track is not an error, got %v", err)
	}
	if len(got) != 0 {
		t.Errorf("candidates = %d, want none", len(got))
	}
}

func TestFind_IgnoresNonYouTubeSources(t *testing.T) {
	src := ytmusic.NewSource(&recordingFetcher{}, stubLookup{})
	req := acquisition.FindRequest{
		Title:  "T",
		Artist: "A",
		Identity: acquisition.RecordingIdentity{
			Sources: []acquisition.ProviderRef{{Provider: "deezer", ExternalID: "999"}},
		},
	}

	got, _ := src.Find(context.Background(), req)
	if len(got) != 0 {
		t.Errorf("a deezer id is not downloadable here; want no candidates, got %d", len(got))
	}
}

func TestFetch_DelegatesToTheDownloader(t *testing.T) {
	fetcher := &recordingFetcher{}
	src := ytmusic.NewSource(fetcher, stubLookup{})

	fetched, err := src.Fetch(context.Background(),
		acquisition.AudioCandidate{URL: "https://music.youtube.com/watch?v=xyz"}, "/tmp/out")
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if fetcher.gotURL != "https://music.youtube.com/watch?v=xyz" {
		t.Errorf("downloaded %q", fetcher.gotURL)
	}
	if fetched.Path != "/tmp/out/track.mp3" {
		t.Errorf("path = %q", fetched.Path)
	}
}

func TestName(t *testing.T) {
	if got := ytmusic.NewSource(nil, nil).Name(); got != ytmusic.SourceName {
		t.Errorf("Name() = %q, want %q", got, ytmusic.SourceName)
	}
}

func TestFind_TheCatalogLinkIsThePinnedVideo(t *testing.T) {
	req := acquisition.FindRequest{
		Title:          "Finesse",
		CatalogSources: []acquisition.CatalogSource{{Provider: "youtube", ExternalID: "dQw4w9WgXcQ"}},
	}

	got, err := ytmusic.NewSource(&recordingFetcher{}, stubLookup{}).Find(context.Background(), req)
	if err != nil || len(got) != 1 {
		t.Fatalf("Find = %v, %v", got, err)
	}
	if got[0].Kind != acquisition.KindPinnedVideo {
		t.Errorf("Kind = %q, want the pinned video", got[0].Kind)
	}
}

func resolverVideoOnly(videoID string) acquisition.RecordingIdentity {
	return acquisition.RecordingIdentity{Sources: []acquisition.ProviderRef{{Provider: "youtube", ExternalID: videoID}}}
}

func findURLs(t *testing.T, req acquisition.FindRequest) []string {
	t.Helper()
	got, err := ytmusic.NewSource(&recordingFetcher{}, stubLookup{}).Find(context.Background(), req)
	if err != nil {
		t.Fatalf("Find: %v", err)
	}
	urls := make([]string, len(got))
	for i, c := range got {
		urls[i] = c.URL
	}
	return urls
}

func TestFind_TheCatalogPinBeatsTheResolverVideo(t *testing.T) {
	req := acquisition.FindRequest{
		Title:          "Mubu",
		Identity:       resolverVideoOnly("dQw4w9WgXcQ"),
		CatalogSources: []acquisition.CatalogSource{{Provider: "deezer", ExternalID: "12345"}, {Provider: "youtube", ExternalID: "OkjmSTJOAL4"}},
	}

	got := findURLs(t, req)

	if want := []string{"https://music.youtube.com/watch?v=OkjmSTJOAL4"}; !slices.Equal(got, want) {
		t.Errorf("candidates = %v, want only the catalog-pinned video %v", got, want)
	}
}

func TestFind_TheCatalogPinStandsWhenTheResolverFoundNothing(t *testing.T) {
	req := acquisition.FindRequest{
		Title:          "Mubu",
		CatalogSources: []acquisition.CatalogSource{{Provider: "youtube", ExternalID: "OkjmSTJOAL4"}},
	}

	got := findURLs(t, req)

	if want := []string{"https://music.youtube.com/watch?v=OkjmSTJOAL4"}; !slices.Equal(got, want) {
		t.Errorf("candidates = %v, want the pinned video %v", got, want)
	}
}

func TestFind_WithoutAYouTubePinTheResolverVideoIsUsed(t *testing.T) {
	req := acquisition.FindRequest{
		Title:          "Mubu",
		Identity:       resolverVideoOnly("dQw4w9WgXcQ"),
		CatalogSources: []acquisition.CatalogSource{{Provider: "deezer", ExternalID: "12345"}},
	}

	got := findURLs(t, req)

	if want := []string{"https://music.youtube.com/watch?v=dQw4w9WgXcQ"}; !slices.Equal(got, want) {
		t.Errorf("candidates = %v, want the resolver's video %v", got, want)
	}
}

func TestFind_AMalformedCatalogPinIsNotReplacedByTheResolverVideo(t *testing.T) {
	req := acquisition.FindRequest{
		Title:          "Mubu",
		Identity:       resolverVideoOnly("dQw4w9WgXcQ"),
		CatalogSources: []acquisition.CatalogSource{{Provider: "youtube", ExternalID: "abc&list=1"}},
	}

	if got := findURLs(t, req); len(got) != 0 {
		t.Errorf("candidates = %v, want none for a malformed pin", got)
	}
}

func TestFind_TheCatalogPinCarriesWhatThatVideoIs(t *testing.T) {
	src := ytmusic.NewSource(&recordingFetcher{}, observedFixture(t, "88oFlVnMEsc"))
	req := acquisition.FindRequest{
		Title:          "remember them days",
		Identity:       resolverVideoOnly("dQw4w9WgXcQ"),
		CatalogSources: []acquisition.CatalogSource{{Provider: "youtube", ExternalID: "88oFlVnMEsc"}},
	}

	got, err := src.Find(context.Background(), req)
	if err != nil || len(got) != 1 {
		t.Fatalf("Find = %+v, %v", got, err)
	}
	if got[0].Kind != acquisition.KindArtTrack || !got[0].IsResolved() || got[0].Title != "2021 Vibes" {
		t.Errorf("candidate = %+v, want the art track, resolved video with its own title", got[0])
	}
}

func TestFind_TheObservedTypeIsTheKind(t *testing.T) {
	resolverOnly := func(videoID string) acquisition.FindRequest {
		return acquisition.FindRequest{Title: "t", Identity: resolverVideoOnly(videoID)}
	}
	catalogPinned := func(videoID string) acquisition.FindRequest {
		return acquisition.FindRequest{Title: "t", CatalogSources: []acquisition.CatalogSource{{Provider: "youtube", ExternalID: videoID}}}
	}
	tests := []struct {
		name         string
		lookup       stubLookup
		req          acquisition.FindRequest
		wantKind     acquisition.CandidateKind
		wantResolved bool
		wantArtist   string
	}{
		{"art track is the art track", observedFixture(t, "88oFlVnMEsc"), resolverOnly("88oFlVnMEsc"), acquisition.KindArtTrack, true, "LUCKI"},
		{"official video is the pinned video", observedFixture(t, "u4GAeAYGAqE"), resolverOnly("u4GAeAYGAqE"), acquisition.KindPinnedVideo, true, "slayr"},
		{"user upload is an unresolved upload", stubLookup{watch: ytmsearch.Watch{Type: ytmsearch.VideoTypeUGC}}, resolverOnly("dQw4w9WgXcQ"), acquisition.KindUpload, false, ""},
		{"user upload the catalog pinned stays resolved", stubLookup{watch: ytmsearch.Watch{Type: ytmsearch.VideoTypeUGC}}, catalogPinned("dQw4w9WgXcQ"), acquisition.KindPinnedVideo, true, ""},
		{"failed lookup of a resolver video is unresolved", stubLookup{err: errors.New("ytm down")}, resolverOnly("dQw4w9WgXcQ"), acquisition.KindUpload, false, ""},
		{"failed lookup of a catalog pin stays resolved", stubLookup{err: errors.New("ytm down")}, catalogPinned("dQw4w9WgXcQ"), acquisition.KindPinnedVideo, true, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ytmusic.NewSource(&recordingFetcher{}, tt.lookup).Find(context.Background(), tt.req)
			if err != nil || len(got) != 1 {
				t.Fatalf("Find = %+v, %v", got, err)
			}
			c := got[0]
			if c.Kind != tt.wantKind || c.IsResolved() != tt.wantResolved || c.Channel != "" {
				t.Errorf("candidate = %+v, want kind %q resolved %v and no channel", c, tt.wantKind, tt.wantResolved)
			}
			if tt.wantArtist != "" && !slices.Contains(c.Artists, tt.wantArtist) {
				t.Errorf("artists = %v, want %q credited", c.Artists, tt.wantArtist)
			}
		})
	}
}
