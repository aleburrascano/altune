package providers

import (
	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/discovery/ports"
	"context"
	"errors"
	"fmt"
	"testing"
	"time"
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

	if !errors.Is(err, ports.ErrArtworkDegraded) {
		t.Fatalf("a miss cut short by the deadline must report ErrArtworkDegraded, got %v", err)
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

func TestChainedArtworkResolver_MissAfterAFailureIsDegraded(t *testing.T) {
	chain := NewChainedArtworkResolver(
		&fakeArtworkResolver{err: fmt.Errorf("coverartarchive 503")},
		&fakeArtworkResolver{url: ""},
	)

	url, _, err := chain.ResolveTagged(context.Background(), domain.ResultKindTrack, "Song", "Artist", "mbid")

	if !errors.Is(err, ports.ErrArtworkDegraded) {
		t.Fatalf("a miss with one provider down must report ErrArtworkDegraded, got %v", err)
	}
	if url != "" {
		t.Errorf("expected empty URL on a degraded miss, got %q", url)
	}
}

func TestChainedArtworkResolver_IdentityMissAfterAFailureIsDegraded(t *testing.T) {
	chain := NewChainedArtworkResolver(
		&fakeIdentityResolver{err: fmt.Errorf("discogs 503")},
		&fakeIdentityResolver{url: ""},
	)

	url, _, err := chain.ResolveWithIdentityTagged(
		context.Background(), domain.ResultKindAlbum, "DAMN.", "Kendrick Lamar", ports.ArtworkIdentity{MBID: "mbid"})

	if !errors.Is(err, ports.ErrArtworkDegraded) {
		t.Fatalf("an identity miss with one provider down must report ErrArtworkDegraded, got %v", err)
	}
	if url != "" {
		t.Errorf("expected empty URL on a degraded miss, got %q", url)
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

type fakeIdentityResolver struct {
	url        string
	err        error
	source     string
	nameCalled bool
	idCalled   bool
}

func (f *fakeIdentityResolver) Resolve(context.Context, domain.ResultKind, string, string, string) (string, error) {
	f.nameCalled = true
	return f.url, f.err
}

func (f *fakeIdentityResolver) ResolveByIdentity(context.Context, domain.ResultKind, ports.ArtworkIdentity) (string, error) {
	f.idCalled = true
	return f.url, f.err
}

func (f *fakeIdentityResolver) ArtworkSource() domain.ProviderKey {
	return domain.ProviderKey(f.source)
}

func TestChainedArtworkResolver_ResolveWithIdentityTagged(t *testing.T) {
	nameOnly := &fakeArtworkResolver{url: "https://img/name-guess.jpg"}
	failing := &fakeIdentityResolver{err: errors.New("boom"), source: "broken"}
	identity := &fakeIdentityResolver{url: "https://img/identity.jpg", source: "discogs"}

	chain := NewChainedArtworkResolver(nameOnly, failing, identity)
	url, source, err := chain.ResolveWithIdentityTagged(
		context.Background(), domain.ResultKindArtist, "Che", "",
		ports.ArtworkIdentity{ExternalIDs: map[string]string{"discogs": "38"}},
	)
	if err != nil {
		t.Fatalf("ResolveWithIdentityTagged: %v", err)
	}
	if url != "https://img/identity.jpg" {
		t.Errorf("url = %q, want the identity resolver's image (errors skipped, name resolvers never tried)", url)
	}
	if source != "discogs" {
		t.Errorf("source = %q, want the resolver's tag", source)
	}
	if !failing.idCalled || !identity.idCalled {
		t.Error("both identity resolvers must be tried in order")
	}
	if failing.nameCalled || identity.nameCalled {
		t.Error("the identity path must never fall back to a name search — that is the caller's labelled-provisional decision")
	}
}

func TestChainedArtworkResolver_ResolveWithIdentityTagged_skipsPlaceholder(t *testing.T) {
	placeholder := &fakeIdentityResolver{url: DeezerPlaceholderImage, source: "deezer"}
	real := &fakeIdentityResolver{url: "https://img/real.jpg", source: "discogs"}

	chain := NewChainedArtworkResolver(placeholder, real)
	url, source, err := chain.ResolveWithIdentityTagged(
		context.Background(), domain.ResultKindArtist, "Che", "", ports.ArtworkIdentity{})
	if err != nil {
		t.Fatalf("ResolveWithIdentityTagged: %v", err)
	}
	if url != "https://img/real.jpg" || source != "discogs" {
		t.Errorf("(%q, %q), want the placeholder skipped", url, source)
	}
}

func TestChainedArtworkResolver_ResolveWithIdentityTagged_noIdentitySourceIsEmpty(t *testing.T) {
	chain := NewChainedArtworkResolver(&fakeArtworkResolver{url: "https://img/name.jpg"})
	url, source, err := chain.ResolveWithIdentityTagged(
		context.Background(), domain.ResultKindArtist, "Che", "", ports.ArtworkIdentity{})
	if err != nil || url != "" || source != "" {
		t.Errorf("(%q, %q, %v), want empty — no identity-capable resolver in the chain", url, source, err)
	}
}

func TestChainedArtworkResolver_ResolveTagged_skipsIdentityResolvers(t *testing.T) {
	identity := &fakeIdentityResolver{url: "https://img/identity.jpg", source: "discogs"}
	name := &fakeArtworkResolver{url: "https://img/name.jpg"}

	chain := NewChainedArtworkResolver(identity, name)
	url, source, err := chain.ResolveTagged(context.Background(), domain.ResultKindArtist, "Che", "", "")
	if err != nil {
		t.Fatalf("ResolveTagged: %v", err)
	}
	if url != "https://img/name.jpg" {
		t.Errorf("url = %q, want the name resolver's image", url)
	}
	if identity.nameCalled || identity.idCalled {
		t.Error("identity-only resolver must be skipped entirely on the name path")
	}
	if source != "" {
		t.Errorf("source = %q, want empty for an unsourced resolver", source)
	}
}
