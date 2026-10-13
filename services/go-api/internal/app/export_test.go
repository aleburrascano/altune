package app

import (
	discoveryPorts "altune/go-api/internal/discovery/ports"
	"altune/go-api/internal/shared/config"
)

func BuildArtworkChain(cfg *config.Config) discoveryPorts.TaggingArtworkResolver {
	return buildArtworkChain(newClientFactory(nil), cfg)
}
