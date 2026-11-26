package download

import (
	domain "altune/go-api/internal/download/internal/core/domain"
	ports "altune/go-api/internal/download/internal/core/ports"
)

const (
	JobKindAcquire = ports.JobKindAcquire
	JobKindReplace = ports.JobKindReplace
	TrackFailed    = ports.TrackFailed
)

var ErrNoJobAvailable = domain.ErrNoJobAvailable
