package download

import "altune/go-api/internal/download/internal/ports"

type TrackLabel = ports.TrackLabel

var (
	ErrIllegalTransition = ports.ErrIllegalTransition
	ErrTrackNotFound     = ports.ErrTrackNotFound
)
