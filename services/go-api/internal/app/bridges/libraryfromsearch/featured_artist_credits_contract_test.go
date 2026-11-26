package libraryfromsearch_test

import (
	"altune/go-api/internal/library/librarytest"
	"testing"
)

func TestFeaturedArtistCredits_ResolverContractOverDiscovery(t *testing.T) {
	librarytest.FeaturedResolverContract(t, resolverOverCatalogCredits)
}
