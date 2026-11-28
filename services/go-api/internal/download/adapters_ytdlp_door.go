package download

import ytdlp "altune/go-api/internal/download/internal/adapters/ytdlp"

type (
	CanarySource         = ytdlp.CanarySource
	YouTubeAudioSearcher = ytdlp.YouTubeAudioSearcher
)

var (
	MustNewFfprobeProber      = ytdlp.MustNewFfprobeProber
	MustNewYtDlpAudioSearcher = ytdlp.MustNewYtDlpAudioSearcher
)

var (
	SoundCloudCanary = ytdlp.SoundCloudCanary
	YTMusicCanary    = ytdlp.YTMusicCanary
	YouTubeCanary    = ytdlp.YouTubeCanary
)

const YtDlpSourceName = ytdlp.SourceName
