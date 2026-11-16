package discovery

import ytmusic "altune/go-api/internal/discovery/internal/adapters/providers/ytmusic"

type YouTubeMusicAdapter = ytmusic.YouTubeMusicAdapter

var (
	NewYouTubeMusicAdapter         = ytmusic.NewYouTubeMusicAdapter
	NewYouTubeMusicArtworkResolver = ytmusic.NewYouTubeMusicArtworkResolver
)
