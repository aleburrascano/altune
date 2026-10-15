package domain

const (
	ExtraArtworkPath      = "artwork_path"
	ExtraCollapsedArtists = "collapsed_artists"
	ExtraAlbum            = "album"
	ExtraDuration         = "duration"
	ExtraGenre            = "genre"
	ExtraExplicit         = "explicit"
	ExtraFeaturedArtists  = "featured_artists"
	ExtraPreviewURL       = "preview_url"
	ExtraListeners        = "listeners"
	ExtraPlaycount        = "playcount"
	ExtraPlaybackCount    = "playback_count"
	ExtraTrackNumber      = "track_number"
	ExtraDiscNumber       = "disc_number"
	ExtraCopyright        = "copyright"
	ExtraUPC              = "upc"
	ExtraMBID             = "mbid"
	ExtraISRC             = "isrc"
	ExtraDisambiguation   = "disambiguation"
	ExtraOwnedTrackID     = "owned_track_id"
	ExtraTrackPosition    = "track_position"
	ExtraRecordType       = "record_type"
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
