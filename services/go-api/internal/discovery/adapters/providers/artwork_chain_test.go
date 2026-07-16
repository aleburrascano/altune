package providers

import (
	"context"
	"fmt"
	"testing"

	"altune/go-api/internal/discovery/domain"
)

// fakeArtworkResolver is a fake implementing ports.ArtworkResolver for chain tests.
type fakeArtworkResolver struct {
	url string
	err error
}

func (f *fakeArtworkResolver) Resolve(_ context.Context, _ domain.ResultKind, _, _, _ string) (string, error) {
	return f.url, f.err
}

func TestChainedArtworkResolver_ReturnsFirstNonEmpty(t *testing.T) {
	chain := NewChainedArtworkResolver(
		&fakeArtworkResolver{url: "", err: nil},
		&fakeArtworkResolver{url: "https://images.example.com/cover.jpg", err: nil},
		&fakeArtworkResolver{url: "https://images.example.com/other.jpg", err: nil},
	)

	url, _, err := chain.ResolveTagged(context.Background(), domain.ResultKindTrack, "Song", "Artist", "mbid")
	if err != nil {
		t.Fatalf("ResolveTagged returned error: %v", err)
	}
	if url != "https://images.example.com/cover.jpg" {
		t.Errorf("expected second resolver's URL, got %q", url)
	}
}

func TestChainedArtworkResolver_SkipsErrors(t *testing.T) {
	chain := NewChainedArtworkResolver(
		&fakeArtworkResolver{url: "", err: fmt.Errorf("network error")},
		&fakeArtworkResolver{url: "https://images.example.com/fallback.jpg", err: nil},
	)

	url, _, err := chain.ResolveTagged(context.Background(), domain.ResultKindArtist, "Artist", "", "mbid")
	if err != nil {
		t.Fatalf("ResolveTagged returned error: %v", err)
	}
	if url != "https://images.example.com/fallback.jpg" {
		t.Errorf("expected fallback URL after error, got %q", url)
	}
}

func TestChainedArtworkResolver_AllEmpty(t *testing.T) {
	chain := NewChainedArtworkResolver(
		&fakeArtworkResolver{url: "", err: nil},
		&fakeArtworkResolver{url: "", err: nil},
	)

	url, _, err := chain.ResolveTagged(context.Background(), domain.ResultKindTrack, "Song", "Artist", "mbid")
	if err != nil {
		t.Fatalf("ResolveTagged returned error: %v", err)
	}
	if url != "" {
		t.Errorf("expected empty URL when all resolvers return empty, got %q", url)
	}
}

func TestChainedArtworkResolver_NoResolvers(t *testing.T) {
	chain := NewChainedArtworkResolver()

	url, _, err := chain.ResolveTagged(context.Background(), domain.ResultKindTrack, "Song", "Artist", "mbid")
	if err != nil {
		t.Fatalf("ResolveTagged returned error: %v", err)
	}
	if url != "" {
		t.Errorf("expected empty URL with no resolvers, got %q", url)
	}
}

func TestChainedArtworkResolver_SkipsDeezerPlaceholder(t *testing.T) {
	// IsDeezerPlaceholder filters URLs that are Deezer's placeholder images.
	// The chain should skip those and try the next resolver.
	chain := NewChainedArtworkResolver(
		&fakeArtworkResolver{url: "https://e-cdns-images.dzcdn.net/images/artist//500x500-000000-80-0-0.jpg", err: nil},
		&fakeArtworkResolver{url: "https://images.example.com/real.jpg", err: nil},
	)

	url, _, err := chain.ResolveTagged(context.Background(), domain.ResultKindArtist, "Artist", "", "mbid")
	if err != nil {
		t.Fatalf("ResolveTagged returned error: %v", err)
	}
	if url != "https://images.example.com/real.jpg" {
		t.Errorf("expected real URL after skipping Deezer placeholder, got %q", url)
	}
}
