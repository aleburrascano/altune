package acquisition

import (
	audiojudge "altune/go-api/internal/acquisition/internal/core/audiojudge"
	ports "altune/go-api/internal/acquisition/internal/core/ports"
)

type JudgeKind = audiojudge.Kind

const JudgeUnknown = audiojudge.Unknown

type (
	LinkedRecording = ports.LinkedRecording
	RecordingFacts  = ports.RecordingFacts
)
