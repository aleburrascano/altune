package memory_test

import (
	"altune/go-api/internal/catalog"
	"altune/go-api/internal/catalog/internal/adapters/contract"
	"testing"
)

func TestFeaturedResolverFake_Contract(t *testing.T) {
	contract.FeaturedResolverContract(t, func(_ *testing.T, feats []catalog.FeaturedArtist, err error) catalog.FeaturedArtistResolver {
		return catalog.InMemoryFeaturedResolver{ByTitle: map[string][]catalog.FeaturedArtist{"Song": feats}, Err: err}
	})
}
