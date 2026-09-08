package persistence

import (
	"github.com/jackc/pgx/v5/pgxpool"
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
