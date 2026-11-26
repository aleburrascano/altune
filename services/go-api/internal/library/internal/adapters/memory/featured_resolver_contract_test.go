package memory_test

import (
	"altune/go-api/internal/library"
	"altune/go-api/internal/library/internal/adapters/contracttest"
	"testing"
)

func TestFeaturedResolverFake_Contract(t *testing.T) {
	contracttest.FeaturedResolverContract(t, func(_ *testing.T, feats []library.FeaturedArtist, err error) library.FeaturedArtistResolver {
		return library.InMemoryFeaturedResolver{ByTitle: map[string][]library.FeaturedArtist{"Song": feats}, Err: err}
	})
}
