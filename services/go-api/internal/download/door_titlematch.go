package download

import (
	audiojudge "altune/go-api/internal/download/internal/core/audioverdict"
	ports "altune/go-api/internal/download/internal/core/ports"
)

type JudgeKind = audiojudge.Kind

const JudgeUnknown = audiojudge.Unknown

type (
	LinkedRecording = ports.LinkedRecording
	RecordingFacts  = ports.RecordingFacts
)
