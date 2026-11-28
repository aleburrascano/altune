package downloadtest

import (
	"altune/go-api/internal/download/internal/adapters/ytdlp"
	"altune/go-api/internal/download/internal/ports"
)

func FfprobeAvailable(ffmpegLocation string) (ffprobe, ffmpeg bool) {
	return ytdlp.MustNewFfprobeProber(ffmpegLocation, ports.NoopVerifySkipRecorder()).Available()
}
