package providers

import (
	"context"
	"errors"
	"testing"

	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/discovery/ports"
)

// fakeIdentityResolver implements ports.ArtworkResolver + IdentityArtworkResolver
// + SourcedArtworkResolver for chain tests.
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

func (f *fakeIdentityResolver) ArtworkSource() string { return f.source }

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

// The name path must skip identity-only resolvers so a missing identity can't
// trigger a wrong same-name guess.
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
	// fakeArtworkResolver carries no ArtworkSource — tag must be empty, not invented.
	if source != "" {
		t.Errorf("source = %q, want empty for an unsourced resolver", source)
	}
}
