package service

import (
	"context"

	"altune/go-api/internal/catalog/domain"
	"altune/go-api/internal/shared"
)

// trackByIDGetter is the shared GetByID slice used by services that only need
// to fetch a single track. Defined once here so the signature cannot drift
// between consumers.
type trackByIDGetter interface {
	GetByID(ctx context.Context, id domain.TrackId, userId shared.UserId) (*domain.Track, error)
}
