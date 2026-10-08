package domain

import (
	"altune/go-api/internal/shared"
	"time"
)

type VerifiedToken struct {
	UserID    shared.UserId
	ExpiresAt time.Time
}
