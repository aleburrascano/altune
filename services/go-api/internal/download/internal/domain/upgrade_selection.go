package domain

import (
	"altune/go-api/internal/shared/ids"
	"time"
)

type UpgradeCandidate struct {
	TrackID ids.TrackId
	UserID  ids.UserId
	Grade   string
}

type UpgradeSelection struct {
	Limit      int
	Generation int
	RetryAfter time.Duration
	MaxQueued  int

	CheckInPlace bool

	LowBitrateFloorKbps int

	Upgradable []TrackStatus
}

type UpgradeAttempt struct {
	RejectedSourceKey string
	CountsAsTry       bool
}
