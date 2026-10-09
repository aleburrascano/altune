package ports

import (
	"altune/go-api/internal/catalog/domain"
	"altune/go-api/internal/shared"
	"context"
)

type FeaturedArtistResolver interface {
	Resolve(ctx context.Context, artist, title string) ([]domain.FeaturedArtist, error)
}

type FeaturedArtistRepository interface {
	ListTracksFeaturing(ctx context.Context, userId shared.UserId, fa domain.FeaturedArtist) ([]*domain.Track, error)
	ReplaceFeaturedArtists(ctx context.Context, id domain.TrackId, userId shared.UserId, feats []domain.FeaturedArtist) error
}
