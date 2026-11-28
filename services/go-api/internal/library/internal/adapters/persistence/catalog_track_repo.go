package persistence

import (
	"altune/go-api/internal/library/internal/ports"

	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	_ ports.TrackAdder               = (*PostgresCatalogTrackRepository)(nil)
	_ ports.TrackGetter              = (*PostgresCatalogTrackRepository)(nil)
	_ ports.TrackBatchGetter         = (*PostgresCatalogTrackRepository)(nil)
	_ ports.TrackLister              = (*PostgresCatalogTrackRepository)(nil)
	_ ports.TrackUpdater             = (*PostgresCatalogTrackRepository)(nil)
	_ ports.TrackNumberSetter        = (*PostgresCatalogTrackRepository)(nil)
	_ ports.TrackDeleter             = (*PostgresCatalogTrackRepository)(nil)
	_ ports.TrackAudioDeleter        = (*PostgresCatalogTrackRepository)(nil)
	_ ports.TrackReadWriter          = (*PostgresCatalogTrackRepository)(nil)
	_ ports.TrackLookup              = (*PostgresCatalogTrackRepository)(nil)
	_ ports.LibraryViewRepository    = (*PostgresCatalogTrackRepository)(nil)
	_ ports.FeaturedArtistRepository = (*PostgresCatalogTrackRepository)(nil)
)

type PostgresCatalogTrackRepository struct {
	*PostgresTrackRepository
	*PostgresLibraryLensRepository
	*PostgresFeaturedArtistRepository
}

func NewPostgresCatalogTrackRepository(pool *pgxpool.Pool, metrics ports.DBCallMetrics) *PostgresCatalogTrackRepository {
	return &PostgresCatalogTrackRepository{
		PostgresTrackRepository:          NewPostgresTrackRepository(pool, metrics),
		PostgresLibraryLensRepository:    NewPostgresLibraryLensRepository(pool, metrics),
		PostgresFeaturedArtistRepository: NewPostgresFeaturedArtistRepository(pool, metrics),
	}
}
