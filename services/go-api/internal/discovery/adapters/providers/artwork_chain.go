package providers

import (
	"context"

	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/discovery/ports"
)

type ChainedArtworkResolver struct {
	resolvers []ports.ArtworkResolver
}

func NewChainedArtworkResolver(resolvers ...ports.ArtworkResolver) *ChainedArtworkResolver {
	return &ChainedArtworkResolver{resolvers: resolvers}
}

func (c *ChainedArtworkResolver) Resolve(ctx context.Context, kind domain.ResultKind, title, subtitle, mbid string) (string, error) {
	url, _, err := c.ResolveTagged(ctx, kind, title, subtitle, mbid)
	return url, err
}

// ResolveTagged is Resolve plus the source that supplied the URL (for
// per-provider coverage visibility). Source is "" when nothing resolved.
func (c *ChainedArtworkResolver) ResolveTagged(ctx context.Context, kind domain.ResultKind, title, subtitle, mbid string) (string, string, error) {
	for _, resolver := range c.resolvers {
		// Identity-only resolvers (fetch by bridged id) never name-search — skip
		// them on the name path so a missing identity can't trigger a wrong guess.
		if _, identityOnly := resolver.(ports.IdentityArtworkResolver); identityOnly {
			continue
		}
		url, err := resolver.Resolve(ctx, kind, title, subtitle, mbid)
		if err != nil {
			continue
		}
		if url != "" && !IsDeezerPlaceholder(url) {
			return url, artworkSourceOf(resolver), nil
		}
	}
	return "", "", nil
}

// artworkSourceOf names the resolver for tagging, or "" if it doesn't report one.
func artworkSourceOf(r ports.ArtworkResolver) string {
	if s, ok := r.(ports.SourcedArtworkResolver); ok {
		return s.ArtworkSource()
	}
	return ""
}

// ResolveWithIdentity resolves artwork identity-first: every resolver that can
// fetch by a proven bridged id (Discogs by its MB-asserted id, …) is tried
// before any name-based resolver. Only if no identity source has the image does
// it fall through to the name/MBID chain. This is what stops a same-name artist
// (the "Che" problem) getting another Che's face — the bridged id pins the exact
// entity. Returns "" when nothing resolves; the caller decides the fallback.
func (c *ChainedArtworkResolver) ResolveWithIdentity(ctx context.Context, kind domain.ResultKind, title, subtitle string, id ports.ArtworkIdentity) (string, error) {
	url, _, err := c.ResolveWithIdentityTagged(ctx, kind, title, subtitle, id)
	return url, err
}

// ResolveWithIdentityTagged is ResolveWithIdentity plus the source that supplied
// the URL.
func (c *ChainedArtworkResolver) ResolveWithIdentityTagged(ctx context.Context, kind domain.ResultKind, title, subtitle string, id ports.ArtworkIdentity) (string, string, error) {
	for _, resolver := range c.resolvers {
		ir, ok := resolver.(ports.IdentityArtworkResolver)
		if !ok {
			continue
		}
		url, err := ir.ResolveByIdentity(ctx, kind, id)
		if err != nil {
			continue
		}
		if url != "" && !IsDeezerPlaceholder(url) {
			return url, artworkSourceOf(resolver), nil
		}
	}
	return c.ResolveTagged(ctx, kind, title, subtitle, id.MBID)
}
