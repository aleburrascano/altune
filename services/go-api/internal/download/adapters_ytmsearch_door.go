package download

import ytmsearch "altune/go-api/internal/download/internal/adapters/ytmsearch"

type (
	YouTubeMusicSearchClient = ytmsearch.Client
	YouTubeMusicSearchConfig = ytmsearch.Config
	YouTubeMusicOEmbed       = ytmsearch.OEmbed
	YouTubeMusicSong         = ytmsearch.Song
	YouTubeMusicVideoType    = ytmsearch.VideoType
	YouTubeMusicWatch        = ytmsearch.Watch
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
	YouTubeMusicSearchSourceName = ytmsearch.SourceName
	VideoTypeAudioOnly           = ytmsearch.VideoTypeAudioOnly
	VideoTypeOfficialVideo       = ytmsearch.VideoTypeOfficialVideo
	VideoTypeUserUpload          = ytmsearch.VideoTypeUserUpload
)
