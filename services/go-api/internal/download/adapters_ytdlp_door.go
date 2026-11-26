package download

import ytdlp "altune/go-api/internal/download/internal/adapters/ytdlp"

type (
	CanarySource         = ytdlp.CanarySource
	YouTubeAudioSearcher = ytdlp.YouTubeAudioSearcher
)

var (
	NewFfprobeProber      = ytdlp.NewFfprobeProber
	NewYtDlpSource        = ytdlp.NewSource
	NewYtDlpAudioSearcher = ytdlp.NewYtDlpAudioSearcher
	WithDecodeTimeout     = ytdlp.WithDecodeTimeout
	WithSkipRecorder      = ytdlp.WithSkipRecorder
)

var (
	SoundCloudCanary = ytdlp.SoundCloudCanary
	YTMusicCanary    = ytdlp.YTMusicCanary
	YouTubeCanary    = ytdlp.YouTubeCanary
)

const YtDlpSourceName = ytdlp.SourceName
