package search

import ytmusic "altune/go-api/internal/search/internal/adapters/providers/ytmusic"

var (
	NewYouTubeMusicAdapter           = ytmusic.NewYouTubeMusicAdapter
	NewYouTubeMusicArtworkLookup     = ytmusic.NewYouTubeMusicArtworkLookup
	MustNewYouTubeMusicAdapter       = ytmusic.MustNewYouTubeMusicAdapter
	MustNewYouTubeMusicArtworkLookup = ytmusic.MustNewYouTubeMusicArtworkLookup
	ErrYouTubeMusicSleeperRequired   = ytmusic.ErrSleeperRequired
	ErrYouTubeMusicJitterRequired    = ytmusic.ErrJitterRequired
)
