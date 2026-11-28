package downloadtest

import "altune/go-api/internal/download/internal/adapters/ytmsearch"

const VideoTypeAudioOnly = ytmsearch.VideoTypeAudioOnly

var (
	NewYtmSearch  = ytmsearch.New
	ParseYtmSongs = ytmsearch.ParseSongs
)
