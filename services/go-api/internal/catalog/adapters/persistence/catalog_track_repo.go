package persistence

import (
	"altune/go-api/internal/catalog/ports"

	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	_ ports.TrackAdder               = (*PgxCatalogTrackRepository)(nil)
	_ ports.TrackGetter              = (*PgxCatalogTrackRepository)(nil)
	_ ports.TrackBatchGetter         = (*PgxCatalogTrackRepository)(nil)
	_ ports.TrackLister              = (*PgxCatalogTrackRepository)(nil)
	_ ports.TrackUpdater             = (*PgxCatalogTrackRepository)(nil)
	_ ports.TrackNumberSetter        = (*PgxCatalogTrackRepository)(nil)
	_ ports.TrackDeleter             = (*PgxCatalogTrackRepository)(nil)
	_ ports.TrackAudioDeleter        = (*PgxCatalogTrackRepository)(nil)
	_ ports.TrackReadWriter          = (*PgxCatalogTrackRepository)(nil)
	_ ports.TrackLookup              = (*PgxCatalogTrackRepository)(nil)
	_ ports.LibraryLensRepository    = (*PgxCatalogTrackRepository)(nil)
	_ ports.FeaturedArtistRepository = (*PgxCatalogTrackRepository)(nil)
)

type PgxCatalogTrackRepository struct {
	*PgxTrackRepository
	*PgxLibraryLensRepository
	*PgxFeaturedArtistRepository
}

func NewPgxCatalogTrackRepository(pool *pgxpool.Pool) *PgxCatalogTrackRepository {
	return &PgxCatalogTrackRepository{
		PgxTrackRepository:          NewPgxTrackRepository(pool),
		PgxLibraryLensRepository:    NewPgxLibraryLensRepository(pool),
		PgxFeaturedArtistRepository: NewPgxFeaturedArtistRepository(pool),
	}
}
