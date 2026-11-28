package domain

import (
	"altune/go-api/internal/shared/ids"
	"time"
)

type JobKind string

const (
	JobKindAcquire JobKind = "acquire"
	JobKindReplace JobKind = "replace"
	JobKindUpgrade JobKind = "upgrade"
)

type Job struct {
	TrackID     ids.TrackId
	UserID      ids.UserId
	Kind        JobKind
	Run         int
	ClaimNumber ClaimNumber
	Waited      time.Duration

	CorrelationID string
}
