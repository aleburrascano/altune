package memory

import (
	"altune/go-api/internal/download/internal/domain"
	"altune/go-api/internal/download/internal/ports"
	"altune/go-api/internal/shared/clock"
	"context"
	"maps"
	"sync"
	"time"
)

const outcomeSignalBuffer = 8

type OutcomeRepo struct {
	mu        sync.Mutex
	now       clock.Func
	recorded  []domain.AcquisitionOutcome
	err       error
	hang      time.Duration
	after     func(time.Duration) <-chan time.Time
	calls     chan domain.AcquisitionOutcome
	deadlines chan time.Duration
	ctxErrs   chan error
}

var _ ports.OutcomeRecorder = (*OutcomeRepo)(nil)

func NewOutcomeRepo(now clock.Func) *OutcomeRepo {
	return &OutcomeRepo{
		now:       now,
		calls:     make(chan domain.AcquisitionOutcome, outcomeSignalBuffer),
		deadlines: make(chan time.Duration, outcomeSignalBuffer),
		ctxErrs:   make(chan error, outcomeSignalBuffer),
	}
}

func (r *OutcomeRepo) FailWith(err error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.err = err
}

func (r *OutcomeRepo) HangFor(d time.Duration, after func(time.Duration) <-chan time.Time) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.hang, r.after = d, after
}

func (r *OutcomeRepo) Calls() chan domain.AcquisitionOutcome { return r.calls }

func (r *OutcomeRepo) Deadlines() <-chan time.Duration { return r.deadlines }

func (r *OutcomeRepo) ContextErrors() <-chan error { return r.ctxErrs }

func (r *OutcomeRepo) Recorded(trackID string) []domain.AcquisitionOutcome {
	r.mu.Lock()
	defer r.mu.Unlock()
	var got []domain.AcquisitionOutcome
	for _, o := range r.recorded {
		if o.TrackID == trackID {
			got = append(got, o)
		}
	}
	return got
}

func (r *OutcomeRepo) Record(ctx context.Context, o domain.AcquisitionOutcome) error {
	r.mu.Lock()
	failure, hang, after := r.err, r.hang, r.after
	r.mu.Unlock()
	r.signal(ctx)
	if hang > 0 {
		select {
		case <-after(hang):
		case <-ctx.Done():
			r.calls <- o
			return ctx.Err()
		}
	}
	r.calls <- o
	if failure != nil {
		return failure
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	r.keep(o)
	return nil
}

func (r *OutcomeRepo) signal(ctx context.Context) {
	if deadline, ok := ctx.Deadline(); ok {
		select {
		case r.deadlines <- deadline.Sub(r.now()):
		default:
		}
	}
	select {
	case r.ctxErrs <- ctx.Err():
	default:
	}
}

func (r *OutcomeRepo) keep(o domain.AcquisitionOutcome) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(o.StageMs) == 0 {
		o.StageMs = nil
	} else {
		o.StageMs = maps.Clone(o.StageMs)
	}
	r.recorded = append(r.recorded, o)
}
