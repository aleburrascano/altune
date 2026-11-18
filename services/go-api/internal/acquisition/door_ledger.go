package acquisition

import ports "altune/go-api/internal/acquisition/internal/core/ports"

type RecordingLabel = ports.RecordingLabel

var (
	ErrIllegalTransition = ports.ErrIllegalTransition
	ErrTrackNotFound     = ports.ErrTrackNotFound
)
