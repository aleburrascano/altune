package providers

import (
	"context"
	"fmt"
	"testing"
	"time"

	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/discovery/ports"
)

type fakeArtworkResolver struct {
	url string
	err error
}

func (f *fakeArtworkResolver) Resolve(_ context.Context, _ domain.ResultKind, _, _, _ string) (string, error) {
	return f.url, f.err
}

// slowArtworkResolver blocks for perCallDelay unless the context fires first,
// simulating a provider that sits at its per-provider timeout on every call.
type slowArtworkResolver struct {
	perCallDelay time.Duration
}

func (s *slowArtworkResolver) Resolve(ctx context.Context, _ domain.ResultKind, _, _, _ string) (string, error) {
	select {
	case <-time.After(s.perCallDelay):
		return "", nil
	case <-ctx.Done():
		return "", ctx.Err()
	}
}

func TestChainedArtworkResolver_AggregateTimeoutCapsFullMiss(t *testing.T) {
	const perCall = 200 * time.Millisecond
	const resolverCount = 10

	resolvers := make([]ports.ArtworkResolver, resolverCount)
	for i := range resolvers {
		resolvers[i] = &slowArtworkResolver{perCallDelay: perCall}
	}
	chain := NewChainedArtworkResolver(resolvers...)
	chain.timeout = 500 * time.Millisecond

	start := time.Now()
	url, _, err := chain.ResolveTagged(context.Background(), domain.ResultKindTrack, "Song", "Artist", "mbid")
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("ResolveTagged returned error: %v", err)
	}
	if url != "" {
		t.Fatalf("expected empty URL on full miss, got %q", url)
	}

	sumOfPerResolver := perCall * resolverCount
	if elapsed >= sumOfPerResolver {
		t.Fatalf("chain walked the full per-resolver sum (%v >= %v); no aggregate deadline", elapsed, sumOfPerResolver)
	}
	if elapsed > chain.timeout+perCall {
		t.Fatalf("chain exceeded aggregate bound: elapsed %v, bound ~%v", elapsed, chain.timeout)
	}
}

func TestChainedArtworkResolver_ShorterCallerDeadlineWins(t *testing.T) {
	resolvers := []ports.ArtworkResolver{
		&slowArtworkResolver{perCallDelay: time.Second},
		&slowArtworkResolver{perCallDelay: time.Second},
	}
	chain := NewChainedArtworkResolver(resolvers...)
	chain.timeout = 10 * time.Second

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()

	start := time.Now()
	_, _, _ = chain.ResolveTagged(ctx, domain.ResultKindTrack, "Song", "Artist", "mbid")
	elapsed := time.Since(start)

	if elapsed > time.Second {
		t.Fatalf("shorter caller deadline did not win: elapsed %v", elapsed)
	}
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
