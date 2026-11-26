package download

import ytdlp "altune/go-api/internal/download/internal/adapters/ytdlp"

type (
	CanarySource         = ytdlp.CanarySource
	YouTubeAudioSearcher = ytdlp.YouTubeAudioSearcher
	FfprobeProber        = ytdlp.FfprobeProber
)

var (
	NewFfprobeProber      = ytdlp.NewFfprobeProber
	MustNewFfprobeProber  = ytdlp.MustNewFfprobeProber
	NewYtDlpSource        = ytdlp.NewSource
	NewYtDlpAudioSearcher = ytdlp.NewYtDlpAudioSearcher
	WithDecodeTimeout     = ytdlp.WithDecodeTimeout
)

var (
	SoundCloudCanary = ytdlp.SoundCloudCanary
	YTMusicCanary    = ytdlp.YTMusicCanary
	YouTubeCanary    = ytdlp.YouTubeCanary
)

var ErrSkipRecorderRequired = ytdlp.ErrSkipRecorderRequired

const YtDlpSourceName = ytdlp.SourceName
