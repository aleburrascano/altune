//go:build integration

package providers

import (
	"context"
	"os/exec"
	"testing"
	"time"

	"altune/go-api/internal/discovery/domain"
)

// TestSoundCloudAdapter_Search_Integration requires the real yt-dlp binary and
// live SoundCloud network access. It is gated behind the `integration` build
// tag so it never runs during a plain `go test ./...` (or the CI gate), where
// it would otherwise fail offline. Opt in with `go test -tags integration`.
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
	if pc, ok := first.Extras["playback_count"]; ok {
		if _, isInt := pc.(int64); !isInt {
			t.Errorf("extras.playback_count: got %T, want int64", pc)
		}
	}
}
