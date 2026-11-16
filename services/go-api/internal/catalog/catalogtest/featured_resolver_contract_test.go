package catalogtest_test

import (
	"altune/go-api/internal/catalog"
	"altune/go-api/internal/catalog/catalogtest"
	"testing"
)

func TestFeaturedResolverFake_Contract(t *testing.T) {
	catalogtest.FeaturedResolverContract(t, func(_ *testing.T, feats []catalog.FeaturedArtist, err error) catalog.FeaturedArtistResolver {
		return catalogtest.FeaturedResolver{ByTitle: map[string][]catalog.FeaturedArtist{"Song": feats}, Err: err}
	})
}
