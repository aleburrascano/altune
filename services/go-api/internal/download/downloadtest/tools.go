package downloadtest

import (
	"altune/go-api/internal/download/internal/adapters/memory"
	"altune/go-api/internal/download/internal/adapters/ytdlp"
)

func FfprobeAvailable(ffmpegLocation string) (ffprobe, ffmpeg bool) {
	return ytdlp.MustNewFfprobeProber(ffmpegLocation, memory.NoopVerifySkipRecorder()).Available()
}
