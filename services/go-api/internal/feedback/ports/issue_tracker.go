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

// TrackerThrottle is implemented by a Create error that may carry the tracker's
// own rate-limit signal. Throttled reports ok=true when the tracker refused the
// call because the shared credential is rate limited, with backoff set to how
// long the tracker asked callers to wait (zero when it gave no hint), so the
// application can stop calling it through the lockout.
type TrackerThrottle interface {
	Throttled() (backoff time.Duration, ok bool)
}
