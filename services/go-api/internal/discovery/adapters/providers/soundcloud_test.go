package providers

import (
	"context"
	"os/exec"
	"testing"
	"time"

	"altune/go-api/internal/discovery/domain"
)

func TestSoundCloudAdapter_Name(t *testing.T) {
	adapter := NewSoundCloudAdapter()
	if got := adapter.Name(); got != domain.ProviderSoundCloud {
		t.Errorf("Name() = %v, want %v", got, domain.ProviderSoundCloud)
	}
}

func TestSoundCloudAdapter_SupportedKinds(t *testing.T) {
	adapter := NewSoundCloudAdapter()
	kinds := adapter.SupportedKinds()

	if !kinds[domain.ResultKindTrack] {
		t.Error("expected track to be supported")
	}
	if kinds[domain.ResultKindAlbum] {
		t.Error("expected album to NOT be supported")
	}
	if kinds[domain.ResultKindArtist] {
		t.Error("expected artist to NOT be supported")
	}
}

func TestSoundCloudAdapter_SearchTimeout(t *testing.T) {
	adapter := NewSoundCloudAdapter()
	if got := adapter.SearchTimeout(); got != 5*time.Second {
		t.Errorf("SearchTimeout() = %v, want %v", got, 5*time.Second)
	}
}

func TestSoundCloudAdapter_Search_UnsupportedKinds(t *testing.T) {
	adapter := NewSoundCloudAdapter()
	results, err := adapter.Search(context.Background(), "test query", map[domain.ResultKind]bool{
		domain.ResultKindAlbum: true,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if results != nil {
		t.Errorf("expected nil results for unsupported kinds, got %d", len(results))
	}
}

func TestSoundCloudAdapter_Search_ContextCancelled(t *testing.T) {
	if _, err := exec.LookPath("yt-dlp"); err != nil {
		t.Skip("yt-dlp not installed")
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	adapter := NewSoundCloudAdapter()
	_, err := adapter.Search(ctx, "test", map[domain.ResultKind]bool{
		domain.ResultKindTrack: true,
	})
	if err == nil {
		t.Fatal("expected error for cancelled context")
	}
}

func TestSoundCloudAdapter_Search_Integration(t *testing.T) {
	if _, err := exec.LookPath("yt-dlp"); err != nil {
		t.Skip("yt-dlp not installed, skipping integration test")
	}

	adapter := NewSoundCloudAdapter()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	results, err := adapter.Search(ctx, "Daft Punk Around The World", map[domain.ResultKind]bool{
		domain.ResultKindTrack: true,
	})
	if err != nil {
		t.Fatalf("Search returned error: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("expected at least one result, got 0")
	}

	first := results[0]
	if first.Kind != domain.ResultKindTrack {
		t.Errorf("kind: got %v, want %v", first.Kind, domain.ResultKindTrack)
	}
	if first.Title == "" {
		t.Error("first result has empty title")
	}
	if len(first.Sources) != 1 {
		t.Fatalf("expected 1 source, got %d", len(first.Sources))
	}
	if first.Sources[0].Provider != domain.ProviderSoundCloud {
		t.Errorf("provider: got %v, want %v", first.Sources[0].Provider, domain.ProviderSoundCloud)
	}
}
