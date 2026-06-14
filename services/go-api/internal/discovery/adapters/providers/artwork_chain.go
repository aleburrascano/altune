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
	for _, resolver := range c.resolvers {
		url, err := resolver.Resolve(ctx, kind, title, subtitle, mbid)
		if err != nil {
			continue
		}
		if url != "" && !IsDeezerPlaceholder(url) {
			return url, nil
		}
	}
	return "", nil
}
