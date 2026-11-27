package download

import ytdlp "altune/go-api/internal/download/internal/adapters/ytdlp"

type (
	CanarySource         = ytdlp.CanarySource
	YouTubeAudioSearcher = ytdlp.YouTubeAudioSearcher
)

var (
	MustNewFfprobeProber      = ytdlp.MustNewFfprobeProber
	NewYtDlpSource            = ytdlp.NewSource
	MustNewYtDlpAudioSearcher = ytdlp.MustNewYtDlpAudioSearcher

	WithYtDlpRemoteComponents = ytdlp.WithRemoteComponents
	WithYtDlpPOTokenURL       = ytdlp.WithPOTokenURL
	WithYtDlpEnvironment      = ytdlp.WithEnvironment
	WithYtDlpDownloadTimeout  = ytdlp.WithDownloadTimeout
	WithYtDlpCanaryTimeout    = ytdlp.WithCanaryTimeout
)

var (
	SoundCloudCanary = ytdlp.SoundCloudCanary
	YTMusicCanary    = ytdlp.YTMusicCanary
	YouTubeCanary    = ytdlp.YouTubeCanary
)

const YtDlpSourceName = ytdlp.SourceName
