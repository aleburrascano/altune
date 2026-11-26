package soundcloud_test

import (
	"altune/go-api/internal/search"
	"altune/go-api/internal/search/internal/adapters/providers/soundcloud"
	"altune/go-api/internal/shared/dbtest"
	"context"
	"os/exec"
	"testing"
	"time"
)

func TestSoundCloudAdapter_Search_Integration(t *testing.T) {
	dbtest.RequireIntegration(t)
	if _, err := exec.LookPath("yt-dlp"); err != nil {
		t.Skip("yt-dlp not installed, skipping integration test")
	}

	adapter := soundcloud.NewSoundCloudAdapter()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	results, err := adapter.Search(ctx, "Daft Punk Around The World", map[search.ResultKind]bool{
		search.ResultKindTrack: true,
	})
	if err != nil {
		t.Fatalf("Search returned error: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("expected at least one result, got 0")
	}

	first := results[0]
	if first.Kind != search.ResultKindTrack {
		t.Errorf("kind: got %v, want %v", first.Kind, search.ResultKindTrack)
	}
	if first.Title == "" {
		t.Error("first result has empty title")
	}
	if len(first.Sources) != 1 {
		t.Fatalf("expected 1 source, got %d", len(first.Sources))
	}
	if first.Sources[0].Provider != search.ProviderSoundCloud {
		t.Errorf("provider: got %v, want %v", first.Sources[0].Provider, search.ProviderSoundCloud)
	}
	if pc, ok := first.Extras["playback_count"]; ok {
		if _, isInt := pc.(int64); !isInt {
			t.Errorf("extras.playback_count: got %T, want int64", pc)
		}
	}
}
