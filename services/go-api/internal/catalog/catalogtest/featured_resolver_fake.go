package catalogtest

import (
	"altune/go-api/internal/catalog/domain"
	"context"
)

type FeaturedResolver struct {
	ByTitle map[string][]domain.FeaturedArtist
	Err     error
}

func (f FeaturedResolver) Resolve(_ context.Context, _, title string) ([]domain.FeaturedArtist, error) {
	if f.Err != nil {
		return nil, f.Err
	}
	return f.ByTitle[title], nil
}
