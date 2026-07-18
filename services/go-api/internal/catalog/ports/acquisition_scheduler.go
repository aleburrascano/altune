package ports

import (
	"altune/go-api/internal/catalog/domain"
	"altune/go-api/internal/shared"
)

type AcquisitionScheduler interface {
	Schedule(userId shared.UserId, trackId domain.TrackId, sourceURL string)
}
