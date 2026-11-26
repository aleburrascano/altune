package library

import persistence "altune/go-api/internal/library/internal/adapters/persistence"

type (
	PostgresCatalogTrackRepository  = persistence.PostgresCatalogTrackRepository
	PostgresOrphanedAudioRepository = persistence.PostgresOrphanedAudioRepository
	PostgresPlaylistRepository      = persistence.PostgresPlaylistRepository
	PostgresTrackRepository         = persistence.PostgresTrackRepository
)

var (
	NewPostgresCatalogTrackRepository  = persistence.NewPostgresCatalogTrackRepository
	NewPostgresOrphanedAudioRepository = persistence.NewPostgresOrphanedAudioRepository
	NewPostgresPlaylistRepository      = persistence.NewPostgresPlaylistRepository
	NewPostgresTrackRepository         = persistence.NewPostgresTrackRepository
	SetDBCallMetrics                   = persistence.SetDBCallMetrics
	WithLibraryCap                     = persistence.WithLibraryCap
)
