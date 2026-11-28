package domain

type Verification struct {
	Ffprobe             bool
	Ffmpeg              bool
	Fpcalc              bool
	YtDlp               bool
	FingerprintVerified bool
}

func (v Verification) ToolsPresent() bool {
	return v.Ffprobe && v.Ffmpeg && v.Fpcalc && v.YtDlp
}

const (
	SkipDecodeTimeout      = "decode_timeout"
	SkipDecoderUnavailable = "decoder_unavailable"
	SkipIdentifyFailed     = "identify_failed"
	SkipPreviewFallback    = "preview_fallback"
	SkipProbeFailed        = "probe_failed"
	SkipNoDuration         = "no_duration"
)
