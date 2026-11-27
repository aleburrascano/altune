package download

import ytmsearch "altune/go-api/internal/download/internal/adapters/ytmsearch"

type (
	YouTubeMusicSearchClient = ytmsearch.Client
	YouTubeMusicSearchConfig = ytmsearch.Config
	YouTubeMusicSong         = ytmsearch.Song
	YouTubeMusicVideoType    = ytmsearch.VideoType
)

var (
	NewYtmSearch       = ytmsearch.New
	NewYtmSearchFor    = ytmsearch.ForClient
	NewYtmSearchSource = ytmsearch.NewSource
	ParseYtmSongs      = ytmsearch.ParseSongs
)

const (
	YouTubeMusicSearchSourceName = ytmsearch.SourceName
	VideoTypeAudioOnly           = ytmsearch.VideoTypeAudioOnly
	VideoTypeOfficialVideo       = ytmsearch.VideoTypeOfficialVideo
	VideoTypeUserUpload          = ytmsearch.VideoTypeUserUpload
)
