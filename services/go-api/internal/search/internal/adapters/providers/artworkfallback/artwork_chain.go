package artworkfallback

import (
	"altune/go-api/internal/search/internal/core/ports"
	"altune/go-api/internal/search/internal/domain"
	"context"
	"errors"
	"fmt"
	"time"
)

const defaultArtworkChainTimeout = 20 * time.Second

type ChainedArtworkLookup struct {
	resolvers []ports.ArtworkLookup
	timeout   time.Duration
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
		if _, identityOnly := resolver.(ports.IdentityArtworkLookup); identityOnly {
			continue
		}
		url, err := resolver.Resolve(ctx, kind, title, subtitle, mbid)
		if err != nil {
			failures = errors.Join(failures, err)
			continue
		}
		if url != "" {
			return url, artworkSourceOf(resolver), nil
		}
	}
	return "", "", missVerdict(failures)
}

func missVerdict(failures error) error {
	if failures == nil {
		return nil
	}
	return fmt.Errorf("%w: %w", ports.ErrArtworkDegraded, failures)
}

func artworkSourceOf(r ports.ArtworkLookup) domain.ProviderKey {
	if s, ok := r.(ports.SourcedArtworkLookup); ok {
		return s.ArtworkSource()
	}
	return ""
}

func (c *ChainedArtworkLookup) ResolveWithIdentityTagged(ctx context.Context, kind domain.ResultKind, title, subtitle string, id ports.ArtworkIdentity) (string, domain.ProviderKey, error) {
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	var failures error
	for _, resolver := range c.resolvers {
		if ctx.Err() != nil {
			failures = errors.Join(failures, ctx.Err())
			break
		}
		ir, ok := resolver.(ports.IdentityArtworkLookup)
		if !ok {
			continue
		}
		url, err := ir.ResolveByIdentity(ctx, kind, id)
		if err != nil {
			failures = errors.Join(failures, err)
			continue
		}
		if url != "" {
			return url, artworkSourceOf(resolver), nil
		}
	}
	return "", "", missVerdict(failures)
}
