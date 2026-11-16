package acquisition

import ytdlp "altune/go-api/internal/acquisition/internal/adapters/ytdlp"

type (
	CanarySource       = ytdlp.CanarySource
	YtDlpAudioSearcher = ytdlp.YtDlpAudioSearcher
)

var (
	CopyToTempFile        = ytdlp.CopyToTempFile
	NewFfprobeProber      = ytdlp.NewFfprobeProber
	NewYtDlpSource        = ytdlp.NewSource
	NewYtDlpAudioSearcher = ytdlp.NewYtDlpAudioSearcher
	WithSkipRecorder      = ytdlp.WithSkipRecorder
)

var (
	SoundCloudCanary = ytdlp.SoundCloudCanary
	YTMusicCanary    = ytdlp.YTMusicCanary
	YouTubeCanary    = ytdlp.YouTubeCanary
)

const YtDlpSourceName = ytdlp.SourceName
