package download

import (
	"altune/go-api/internal/download/internal/domain"
	"altune/go-api/internal/download/internal/ports"
)

const (
	JobKindAcquire = ports.JobKindAcquire
	JobKindReplace = ports.JobKindReplace
	TrackFailed    = ports.TrackFailed
)

var ErrNoJobAvailable = domain.ErrNoJobAvailable
