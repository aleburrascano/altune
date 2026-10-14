package catalogtest_test

import (
	"altune/go-api/internal/catalog/catalogtest"
	"altune/go-api/internal/catalog/domain"
	"altune/go-api/internal/catalog/ports"
	"testing"
)

func TestFeaturedResolverFake_Contract(t *testing.T) {
	catalogtest.FeaturedResolverContract(t, func(_ *testing.T, feats []domain.FeaturedArtist, err error) ports.FeaturedArtistResolver {
		return catalogtest.FeaturedResolver{ByTitle: map[string][]domain.FeaturedArtist{"Song": feats}, Err: err}
	})
}
