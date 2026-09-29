package providers

import (
	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/discovery/ports"
	"context"
	"errors"
	"fmt"
	"time"
)

const defaultArtworkChainTimeout = 20 * time.Second

type ChainedArtworkResolver struct {
	resolvers []ports.ArtworkResolver
	timeout   time.Duration
}

func NewChainedArtworkResolver(resolvers ...ports.ArtworkResolver) *ChainedArtworkResolver {
	return &ChainedArtworkResolver{resolvers: resolvers, timeout: defaultArtworkChainTimeout}
}

func (c *ChainedArtworkResolver) ResolveTagged(ctx context.Context, kind domain.ResultKind, title, subtitle, mbid string) (string, domain.ProviderKey, error) {
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	var failures error
	for _, resolver := range c.resolvers {
		if ctx.Err() != nil {
			failures = errors.Join(failures, ctx.Err())
			break
		}
		if _, identityOnly := resolver.(ports.IdentityArtworkResolver); identityOnly {
			continue
		}
		url, err := resolver.Resolve(ctx, kind, title, subtitle, mbid)
		if err != nil {
			failures = errors.Join(failures, err)
			continue
		}
		if url != "" && !IsDeezerPlaceholder(url) {
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

func artworkSourceOf(r ports.ArtworkResolver) domain.ProviderKey {
	if s, ok := r.(ports.SourcedArtworkResolver); ok {
		return s.ArtworkSource()
	}
	return ""
}

func (c *ChainedArtworkResolver) ResolveWithIdentityTagged(ctx context.Context, kind domain.ResultKind, title, subtitle string, id ports.ArtworkIdentity) (string, domain.ProviderKey, error) {
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	var failures error
	for _, resolver := range c.resolvers {
		if ctx.Err() != nil {
			failures = errors.Join(failures, ctx.Err())
			break
		}
		ir, ok := resolver.(ports.IdentityArtworkResolver)
		if !ok {
			continue
		}
		url, err := ir.ResolveByIdentity(ctx, kind, id)
		if err != nil {
			failures = errors.Join(failures, err)
			continue
		}
		if url != "" && !IsDeezerPlaceholder(url) {
			return url, artworkSourceOf(resolver), nil
		}
	}
	return "", "", missVerdict(failures)
}
