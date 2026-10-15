package domain

const (
	ExtraCollapsedArtists = "collapsed_artists"
	ExtraAlbum            = "album"
	ExtraGenre            = "genre"
	ExtraFeaturedArtists  = "featured_artists"
	ExtraPreviewURL       = "preview_url"
	ExtraListeners        = "listeners"
	ExtraPlaycount        = "playcount"
	ExtraPlaybackCount    = "playback_count"
	ExtraCopyright        = "copyright"
	ExtraDisambiguation   = "disambiguation"
	ExtraOwnedTrackID     = "owned_track_id"
	ExtraResolutionTier   = "resolution_tier"
	ExtraTopPick          = "top_pick"
)

func BoolExtra(extras map[string]any, key string) bool {
	switch v := extras[key].(type) {
	case bool:
		return v
	default:
		return false
	}
}

func StringExtra(extras map[string]any, key string) string {
	switch v := extras[key].(type) {
	case string:
		return v
	default:
		return ""
	}
}

func IntExtra(extras map[string]any, key string) int {
	switch v := extras[key].(type) {
	case int:
		return v
	case float64:
		return int(v)
	default:
		return 0
	}
}
