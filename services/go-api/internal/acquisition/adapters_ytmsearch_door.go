package acquisition

import ytmsearch "altune/go-api/internal/acquisition/internal/adapters/ytmsearch"

type (
	YtmSearchClient = ytmsearch.Client
	YtmSearchConfig = ytmsearch.Config
	YtmOEmbed       = ytmsearch.OEmbed
	YtmSong         = ytmsearch.Song
	YtmVideoType    = ytmsearch.VideoType
	YtmWatch        = ytmsearch.Watch
)

var (
	CreditsArtist      = ytmsearch.CreditsArtist
	NewYtmSearch       = ytmsearch.New
	NewYtmSearchSource = ytmsearch.NewSource
	ParseYtmOEmbed     = ytmsearch.ParseOEmbed
	ParseYtmSongs      = ytmsearch.ParseSongs
	SameTitle          = ytmsearch.SameTitle
)

var ErrNoWatchTrack = ytmsearch.ErrNoWatchTrack

const (
	YtmSearchSourceName = ytmsearch.SourceName
	VideoTypeATV        = ytmsearch.VideoTypeATV
	VideoTypeOMV        = ytmsearch.VideoTypeOMV
	VideoTypeUGC        = ytmsearch.VideoTypeUGC
)
