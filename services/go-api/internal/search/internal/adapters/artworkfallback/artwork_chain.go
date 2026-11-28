package artworkfallback

import (
	"altune/go-api/internal/search/internal/domain"
	"altune/go-api/internal/search/internal/ports"
	"context"
	"errors"
	"fmt"
	"time"
)

const defaultArtworkChainTimeout = 20 * time.Second

type ChainedArtworkLookup struct {
	resolvers  []ports.ArtworkLookup
	identities []ports.IdentityArtworkLookup
	timeout    time.Duration
}

func (c *ChainedArtworkLookup) WithIdentityLookups(identities ...ports.IdentityArtworkLookup) *ChainedArtworkLookup {
	scoped := *c
	scoped.identities = identities
	return &scoped
}

func NewChainedArtworkLookup(resolvers ...ports.ArtworkLookup) *ChainedArtworkLookup {
	return NewChainedArtworkResolverWithin(defaultArtworkChainTimeout, resolvers...)
}

func NewChainedArtworkResolverWithin(timeout time.Duration, resolvers ...ports.ArtworkLookup) *ChainedArtworkLookup {
	return &ChainedArtworkLookup{resolvers: resolvers, timeout: timeout}
}

func (c *ChainedArtworkLookup) ResolveTagged(ctx context.Context, kind domain.ResultKind, title, subtitle, mbid string) (string, domain.ProviderKey, error) {
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	var failures error
	for _, resolver := range c.resolvers {
		if ctx.Err() != nil {
			failures = errors.Join(failures, ctx.Err())
			break
		}
		url, err := resolver.Resolve(ctx, kind, title, subtitle, mbid)
		if err != nil {
			failures = errors.Join(failures, err)
			continue
		}
		if url != "" {
			return url, resolver.ArtworkSource(), nil
		}
	}
	return "", "", missVerdict(failures)
}

func missVerdict(failures error) error {
	if failures == nil {
		return nil
	}
	return fmt.Errorf("%w: %w", domain.ErrArtworkDegraded, failures)
}

func (c *ChainedArtworkLookup) ResolveWithIdentityTagged(ctx context.Context, kind domain.ResultKind, title, subtitle string, id ports.ArtworkIdentity) (string, domain.ProviderKey, error) {
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	var failures error
	for _, resolver := range c.identities {
		if ctx.Err() != nil {
			failures = errors.Join(failures, ctx.Err())
			break
		}
		url, err := resolver.ResolveByIdentity(ctx, kind, id)
		if err != nil {
			failures = errors.Join(failures, err)
			continue
		}
		if url != "" {
			return url, resolver.ArtworkSource(), nil
		}
	}
	return "", "", missVerdict(failures)
}
