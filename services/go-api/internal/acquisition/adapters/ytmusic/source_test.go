package ytmusic

import (
	"altune/go-api/internal/acquisition/adapters/ytmsearch"
	"altune/go-api/internal/acquisition/ports"
	"context"
	"errors"
	"os"
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

func pinnedTo(title, videoID string) ports.FindRequest {
	return ports.FindRequest{
		Title:    title,
		Artist:   "slayr",
		Identity: ports.RecordingIdentity{Duration: 151, Sources: []ports.ProviderRef{{Provider: "youtube", ExternalID: videoID}}},
	}
}

func TestFind_TheCandidateCarriesWhatThePinnedVideoIs(t *testing.T) {
	src := NewSource(&recordingFetcher{}, observedFixture(t, "88oFlVnMEsc"))
	got, err := src.Find(context.Background(), pinnedTo("remember them days", "88oFlVnMEsc"))
	if err != nil || len(got) != 1 {
		t.Fatalf("Find = %+v, %v", got, err)
	}
	if got[0].Title != "2021 Vibes" || got[0].Duration != 0 {
		t.Errorf("candidate = %+v, want the pinned video's own title and no copied length", got[0])
	}
}

func TestFind_AFailedLookupLeavesTheFactsUnknown(t *testing.T) {
	src := NewSource(&recordingFetcher{}, stubLookup{err: errors.New("ytm down")})
	got, err := src.Find(context.Background(), pinnedTo("remember them days", "88oFlVnMEsc"))
	if err != nil || len(got) != 1 || got[0].Title != "" || got[0].Duration != 0 {
		t.Fatalf("Find = %+v, %v", got, err)
	}
}

type recordingFetcher struct {
	gotURL string
	gotDir string
}

func (f *recordingFetcher) Download(_ context.Context, url, outDir string) (ports.FetchedAudio, error) {
	f.gotURL, f.gotDir = url, outDir
	return ports.FetchedAudio{Path: outDir + "/track.mp3"}, nil
}

func TestFind_ResolvesFromTheIdentityVideoID(t *testing.T) {
	src := NewSource(&recordingFetcher{}, stubLookup{})
	req := ports.FindRequest{
		Title:  "Blinding Lights",
		Artist: "The Weeknd",
		Identity: ports.RecordingIdentity{
			Duration: 200,
			Sources:  []ports.ProviderRef{{Provider: "youtube", ExternalID: "dQw4w9WgXcQ"}},
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
	if !got[0].Resolved {
		t.Error("a catalog-resolved candidate must be marked Resolved so it skips the resemblance gate")
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
			src := NewSource(&recordingFetcher{}, stubLookup{})
			got, err := src.Find(context.Background(), ports.FindRequest{
				Title: "Song",
				Identity: ports.RecordingIdentity{
					Sources: []ports.ProviderRef{{Provider: "youtube", ExternalID: videoID}},
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
	src := NewSource(&recordingFetcher{}, stubLookup{})

	got, err := src.Find(context.Background(), ports.FindRequest{Title: "T", Artist: "A"})
	if err != nil {
		t.Fatalf("an unresolvable track is not an error, got %v", err)
	}
	if len(got) != 0 {
		t.Errorf("candidates = %d, want none", len(got))
	}
}

func TestFind_IgnoresNonYouTubeSources(t *testing.T) {
	src := NewSource(&recordingFetcher{}, stubLookup{})
	req := ports.FindRequest{
		Title:  "T",
		Artist: "A",
		Identity: ports.RecordingIdentity{
			Sources: []ports.ProviderRef{{Provider: "deezer", ExternalID: "999"}},
		},
	}

	got, _ := src.Find(context.Background(), req)
	if len(got) != 0 {
		t.Errorf("a deezer id is not downloadable here; want no candidates, got %d", len(got))
	}
}

func TestFetch_DelegatesToTheDownloader(t *testing.T) {
	fetcher := &recordingFetcher{}
	src := NewSource(fetcher, stubLookup{})

	fetched, err := src.Fetch(context.Background(),
		ports.AudioCandidate{URL: "https://music.youtube.com/watch?v=xyz"}, "/tmp/out")
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
	if got := NewSource(nil, nil).Name(); got != SourceName {
		t.Errorf("Name() = %q, want %q", got, SourceName)
	}
}
