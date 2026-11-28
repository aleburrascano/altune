package ports

import (
	"altune/go-api/internal/library/internal/domain"
	"altune/go-api/internal/shared/ids"
	"context"
)

type FeaturedArtistResolver interface {
	Resolve(ctx context.Context, artist, title string) ([]domain.FeaturedArtist, error)
}

type FeaturedArtistRepository interface {
	ListTracksFeaturing(ctx context.Context, userId ids.UserId, fa domain.FeaturedArtist) ([]*domain.Track, error)
	ReplaceFeaturedArtists(ctx context.Context, id domain.TrackId, userId ids.UserId, feats []domain.FeaturedArtist) error
}

type FeaturedArtistCredit struct {
	Name     string
	MBID     string
	DeezerID int64
}

type FeaturedArtistCreditSource interface {
	FeaturedArtistCredits(ctx context.Context, artist, title string) ([]FeaturedArtistCredit, error)
}
