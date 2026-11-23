package catalogdiscovery_test

import (
	"altune/go-api/internal/catalog/catalogtest"
	"testing"
)

func TestFeaturedArtistCredits_ResolverContractOverDiscovery(t *testing.T) {
	catalogtest.FeaturedResolverContract(t, resolverOverCatalogCredits)
}
