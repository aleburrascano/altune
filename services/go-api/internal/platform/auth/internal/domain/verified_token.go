package domain

import (
	"altune/go-api/internal/shared/ids"
	"time"
)

type VerifiedToken struct {
	UserID    ids.UserId
	ExpiresAt time.Time
}
