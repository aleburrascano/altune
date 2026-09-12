package providers

import (
	"context"
	"time"

	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/discovery/ports"
)

// defaultArtworkChainTimeout caps the total wall time the resolver chain may
// spend walking its providers. It sits comfortably below the sum of the
// per-provider client timeouts (~10 providers * 10s) so a full miss fails fast
// instead of holding the request goroutine for ~100s.
const defaultArtworkChainTimeout = 20 * time.Second

type ChainedArtworkResolver struct {
	resolvers []ports.ArtworkResolver
	timeout   time.Duration
}

func NewChainedArtworkResolver(resolvers ...ports.ArtworkResolver) *ChainedArtworkResolver {
	return &ChainedArtworkResolver{resolvers: resolvers, timeout: defaultArtworkChainTimeout}
}

func (c *ChainedArtworkResolver) ResolveTagged(ctx context.Context, kind domain.ResultKind, title, subtitle, mbid string) (string, string, error) {
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	for _, resolver := range c.resolvers {
		if ctx.Err() != nil {
			break
		}
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

func artworkSourceOf(r ports.ArtworkResolver) string {
	if s, ok := r.(ports.SourcedArtworkResolver); ok {
		return s.ArtworkSource()
	}
	return ""
}

func (c *ChainedArtworkResolver) ResolveWithIdentityTagged(ctx context.Context, kind domain.ResultKind, title, subtitle string, id ports.ArtworkIdentity) (string, string, error) {
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	for _, resolver := range c.resolvers {
		if ctx.Err() != nil {
			break
		}
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
	return "", "", nil
}
