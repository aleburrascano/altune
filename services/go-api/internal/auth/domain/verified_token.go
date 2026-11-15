package domain

import (
	shared "altune/go-api/internal/shared/kernel"
	"time"
)

type VerifiedToken struct {
	UserID    shared.UserId
	ExpiresAt time.Time
}
