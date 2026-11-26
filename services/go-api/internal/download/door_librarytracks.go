package download

import ports "altune/go-api/internal/download/internal/core/ports"

type TrackLabel = ports.TrackLabel

var (
	ErrIllegalTransition = ports.ErrIllegalTransition
	ErrTrackNotFound     = ports.ErrTrackNotFound
)
