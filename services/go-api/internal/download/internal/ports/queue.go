package ports

import (
	"altune/go-api/internal/shared/ids"
	"context"
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

type ClaimNumber int

type DownloadQueue interface {
	UpgradeJobQueue
	QueueDepthReader
	Enqueue(ctx context.Context, trackID ids.TrackId, kind JobKind, delay time.Duration) error
	Claim(ctx context.Context, lease time.Duration) (Job, error)
	Heartbeat(ctx context.Context, trackID ids.TrackId, fence ClaimNumber, lease time.Duration) error
	Release(ctx context.Context, trackID ids.TrackId, fence ClaimNumber, delay time.Duration) error
	Settle(ctx context.Context, trackID ids.TrackId, fence ClaimNumber) error
}

type UpgradeJobQueue interface {
	ClaimUpgrade(ctx context.Context, lease time.Duration) (Job, error)
	VerifyLease(ctx context.Context, trackID ids.TrackId, fence ClaimNumber) error
}

type QueueDepthReader interface {
	PendingDepth(ctx context.Context) (pending int, oldestAge time.Duration, err error)
}

type JobNotifier interface {
	Listen(ctx context.Context, wake chan<- struct{})
}
