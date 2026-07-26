package ytmusic

import (
	"context"
	"testing"

	"altune/go-api/internal/acquisition/ports"
)

type recordingFetcher struct {
	gotURL string
	gotDir string
}

func (f *recordingFetcher) Download(_ context.Context, url, outDir string) (string, error) {
	f.gotURL, f.gotDir = url, outDir
	return outDir + "/track.mp3", nil
}

func TestFind_ResolvesFromTheIdentityVideoID(t *testing.T) {
	src := NewSource(&recordingFetcher{})
	req := ports.FindRequest{
		Title:  "Blinding Lights",
		Artist: "The Weeknd",
		Identity: ports.RecordingIdentity{
			Duration: 200,
			Sources:  []ports.RecordingSource{{Provider: "youtube", ExternalID: "abc123"}},
		},
	}

	got, err := src.Find(context.Background(), req)
	if err != nil {
		t.Fatalf("Find: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("candidates = %d, want 1", len(got))
	}
	if got[0].URL != "https://music.youtube.com/watch?v=abc123" {
		t.Errorf("URL = %q", got[0].URL)
	}
	if !got[0].Resolved {
		t.Error("a catalog-resolved candidate must be marked Resolved so it skips the resemblance gate")
	}
	if got[0].Duration != 200 {
		t.Errorf("Duration = %v, want the catalog duration", got[0].Duration)
	}
}

func TestFind_NoIdentityMeansNoCandidates(t *testing.T) {
	src := NewSource(&recordingFetcher{})

	got, err := src.Find(context.Background(), ports.FindRequest{Title: "T", Artist: "A"})
	if err != nil {
		t.Fatalf("an unresolvable track is not an error, got %v", err)
	}
	if len(got) != 0 {
		t.Errorf("candidates = %d, want none", len(got))
	}
}

func TestFind_IgnoresNonYouTubeSources(t *testing.T) {
	src := NewSource(&recordingFetcher{})
	req := ports.FindRequest{
		Title:  "T",
		Artist: "A",
		Identity: ports.RecordingIdentity{
			Sources: []ports.RecordingSource{{Provider: "deezer", ExternalID: "999"}},
		},
	}

	got, _ := src.Find(context.Background(), req)
	if len(got) != 0 {
		t.Errorf("a deezer id is not downloadable here; want no candidates, got %d", len(got))
	}
}

func TestFetch_DelegatesToTheDownloader(t *testing.T) {
	fetcher := &recordingFetcher{}
	src := NewSource(fetcher)

	path, err := src.Fetch(context.Background(),
		ports.AudioCandidate{URL: "https://music.youtube.com/watch?v=xyz"}, "/tmp/out")
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if fetcher.gotURL != "https://music.youtube.com/watch?v=xyz" {
		t.Errorf("downloaded %q", fetcher.gotURL)
	}
	if path != "/tmp/out/track.mp3" {
		t.Errorf("path = %q", path)
	}
}

func TestName(t *testing.T) {
	if got := NewSource(nil).Name(); got != SourceName {
		t.Errorf("Name() = %q, want %q", got, SourceName)
	}
}
