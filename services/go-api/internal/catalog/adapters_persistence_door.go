package catalog

import persistence "altune/go-api/internal/catalog/internal/adapters/persistence"

type (
	PgxCatalogTrackRepository  = persistence.PgxCatalogTrackRepository
	PgxOrphanedAudioRepository = persistence.PgxOrphanedAudioRepository
	PgxTrackRepository         = persistence.PgxTrackRepository
)

var (
	NewPgxCatalogTrackRepository  = persistence.NewPgxCatalogTrackRepository
	NewPgxOrphanedAudioRepository = persistence.NewPgxOrphanedAudioRepository
	NewPgxPlaylistRepository      = persistence.NewPgxPlaylistRepository
	NewPgxTrackRepository         = persistence.NewPgxTrackRepository
	SetDBCallMetrics              = persistence.SetDBCallMetrics
)
