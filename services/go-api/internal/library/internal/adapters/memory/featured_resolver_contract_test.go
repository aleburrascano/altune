package memory_test

import (
	"altune/go-api/internal/library/internal/adapters/memory"
	"altune/go-api/internal/library/internal/contracttest"
	"altune/go-api/internal/library/internal/domain"
	"altune/go-api/internal/library/internal/ports"
	"testing"
)

func TestFeaturedResolverFake_Contract(t *testing.T) {
	contracttest.FeaturedResolverContract(t, func(_ *testing.T, feats []domain.FeaturedArtist, err error) ports.FeaturedArtistResolver {
		return memory.FeaturedResolver{ByTitle: map[string][]domain.FeaturedArtist{"Song": feats}, Err: err}
	})
}
