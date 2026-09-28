package ports

import (
	"context"
	"time"

	"altune/go-api/internal/feedback/domain"
)

type IssueRef struct {
	Number int
	URL    string
}

type IssueTracker interface {
	Create(ctx context.Context, report *domain.Report) (IssueRef, error)
}

type TrackerThrottle interface {
	Throttled() (backoff time.Duration, ok bool)
}

type TrackerUncreated interface {
	Uncreated() bool
}
