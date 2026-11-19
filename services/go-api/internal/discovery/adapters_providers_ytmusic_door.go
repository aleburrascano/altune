package discovery

import ytmusic "altune/go-api/internal/discovery/internal/adapters/providers/ytmusic"

var (
	NewYouTubeMusicAdapter         = ytmusic.NewYouTubeMusicAdapter
	NewYouTubeMusicArtworkResolver = ytmusic.NewYouTubeMusicArtworkResolver
)
