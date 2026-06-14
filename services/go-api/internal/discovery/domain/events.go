package domain

import (
	"time"

	"altune/go-api/internal/shared"
)

type SearchPerformed struct {
	OccurredAt time.Time
	UserId     shared.UserId
	Query      string
	QueryNorm  string
}

type ResultClicked struct {
	OccurredAt      time.Time
	UserId          shared.UserId
	QueryNorm       string
	ResultSignature string
	Position        int
}
