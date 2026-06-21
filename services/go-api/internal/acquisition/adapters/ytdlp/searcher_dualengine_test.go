package ytdlp

import (
	"context"
	"errors"
	"testing"

	"altune/go-api/internal/catalog/ports"
)

// withRunner swaps the subprocess seam for a fake, so the dual-engine fan-out is
// exercised without invoking yt-dlp.
func withRunner(r searchRunner) *YtDlpAudioSearcher {
	s := NewYtDlpAudioSearcher("", "", "")
	s.runSearch = r
	return s
}

func TestYtDlpAudioSearcher_Search_QueriesBothEngines(t *testing.T) {
	var specs []string
	s := withRunner(func(_ context.Context, spec string) ([]ports.AudioCandidate, error) {
		specs = append(specs, spec)
		switch spec {
		case "ytsearch5:song artist":
			return []ports.AudioCandidate{{Title: "YT", URL: "https://youtube.com/watch?v=1"}}, nil
		case "scsearch5:song artist":
			return []ports.AudioCandidate{{Title: "SC", URL: "https://soundcloud.com/x/leak"}}, nil
		}
		return nil, nil
	})

	got, err := s.Search(context.Background(), "song artist")
	if err != nil {
		t.Fatalf("Search error: %v", err)
	}

	// AC#1: both engines issued, YouTube first.
	if len(specs) != 2 || specs[0] != "ytsearch5:song artist" || specs[1] != "scsearch5:song artist" {
		t.Fatalf("engine specs = %v, want yt then sc", specs)
	}
	// AC#2: union returned.
	if len(got) != 2 {
		t.Fatalf("expected 2 merged candidates, got %d: %+v", len(got), got)
	}
}

func TestYtDlpAudioSearcher_Search_DedupsByURL(t *testing.T) {
	dup := ports.AudioCandidate{Title: "Dup", URL: "https://soundcloud.com/x/same"}
	s := withRunner(func(_ context.Context, _ string) ([]ports.AudioCandidate, error) {
		// Both engines surface the same URL.
		return []ports.AudioCandidate{dup}, nil
	})

	got, err := s.Search(context.Background(), "q")
	if err != nil {
		t.Fatalf("Search error: %v", err)
	}
	// AC#3: duplicate URL collapsed.
	if len(got) != 1 {
		t.Fatalf("expected duplicate URL collapsed to 1, got %d", len(got))
	}
}

func TestYtDlpAudioSearcher_Search_OneEngineFails(t *testing.T) {
	s := withRunner(func(_ context.Context, spec string) ([]ports.AudioCandidate, error) {
		if spec == "ytsearch5:q" {
			return nil, errors.New("youtube blew up")
		}
		return []ports.AudioCandidate{{Title: "SC", URL: "https://soundcloud.com/x/leak"}}, nil
	})

	got, err := s.Search(context.Background(), "q")
	// AC#4: a single engine failure must not fail the search.
	if err != nil {
		t.Fatalf("a single engine failure must not fail the search, got: %v", err)
	}
	if len(got) != 1 || got[0].Title != "SC" {
		t.Fatalf("expected the surviving engine's candidate, got %+v", got)
	}
}

func TestYtDlpAudioSearcher_Search_BothEnginesFail(t *testing.T) {
	s := withRunner(func(_ context.Context, _ string) ([]ports.AudioCandidate, error) {
		return nil, errors.New("down")
	})

	// AC#5: every engine failing returns an error.
	if _, err := s.Search(context.Background(), "q"); err == nil {
		t.Fatal("expected an error when every engine fails")
	}
}
