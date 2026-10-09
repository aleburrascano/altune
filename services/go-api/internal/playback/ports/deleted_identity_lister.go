package ports

import (
	"altune/go-api/internal/shared"
	"context"
	"errors"
)

var ErrIdentityStoreUnavailable = errors.New("identity store unavailable")

type DeletedIdentityLister interface {
	ListOwnersWithoutIdentity(ctx context.Context, limit int) ([]shared.UserId, error)
}
