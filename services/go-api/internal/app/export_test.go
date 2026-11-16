package app

import (
	"altune/go-api/internal/discovery"
	"altune/go-api/internal/shared/config"
)

func BuildArtworkChain(cfg *config.Config) discovery.TaggingArtworkResolver {
	return buildArtworkChain(newClientFactory(nil), cfg)
}
