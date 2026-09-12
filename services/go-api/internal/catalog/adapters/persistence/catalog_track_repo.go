package persistence

import (
	"altune/go-api/internal/catalog/ports"

	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	_ ports.TrackRepository          = (*PgxCatalogTrackRepository)(nil)
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
