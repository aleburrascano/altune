package deleteduserdata

import (
	"altune/go-api/internal/playback/internal/core/ports"
	"altune/go-api/internal/playback/internal/core/queue"
	"altune/go-api/internal/shared/ids"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"sync"
	"time"
)

const (
	ownerScanWindow       = 500
	maxRetryOwners        = 1000
	DefaultSweepRunBudget = 30 * time.Second
)

type Sweeper struct {
	identities ports.DeletedUserDataLister
	queue      *queue.Service
	metrics    ports.ErasureSweepMetrics
	reaper     ports.DeletedUserDataCleaner
	scanWindow int
	runBudget  time.Duration
	retryCap   int

	mu          sync.Mutex
	resumeAfter ids.UserId
	retryOwners []ids.UserId
}

func NewSweeper(
	identities ports.DeletedUserDataLister,
	queues *queue.Service,
	metrics ports.ErasureSweepMetrics,
	reaper ports.DeletedUserDataCleaner,
	runBudget time.Duration,
) *Sweeper {
	if identities == nil || queues == nil || metrics == nil || reaper == nil {
		panic("deleteduserdata: NewSweeper requires the identity lister, queue service, metrics and reaper")
	}
	if runBudget <= 0 {
		panic("deleteduserdata: NewSweeper requires a positive run budget")
	}
	return &Sweeper{
		identities: identities,
		queue:      queues,
		metrics:    metrics,
		reaper:     reaper,
		scanWindow: ownerScanWindow,
		runBudget:  runBudget,
		retryCap:   maxRetryOwners,
	}
}

type sweepRun struct {
	forgotten int
	failures  []error
	cursor    ids.UserId
}

type eraseOutcome struct {
	forgotten   int
	failures    []error
	failed      []ids.UserId
	unattempted []ids.UserId
}

func (o eraseOutcome) interrupted() bool {
	return len(o.unattempted) > 0
}

func (s *Sweeper) Execute(ctx context.Context) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	run := &sweepRun{cursor: s.resumeAfter}
	if err := s.reapErasedStates(ctx); err != nil {
		run.failures = append(run.failures, err)
	}
	runCtx, cancel := context.WithTimeout(ctx, s.runBudget)
	defer cancel()
	if s.retryFailedOwners(ctx, runCtx, run) {
		s.scanOwners(ctx, runCtx, run)
	}
	s.resumeAfter = run.cursor
	logDeletedIdentitySweep(ctx, run.forgotten)
	return run.forgotten, errors.Join(run.failures...)
}

func (s *Sweeper) retryFailedOwners(ctx, runCtx context.Context, run *sweepRun) bool {
	owners := s.retryOwners
	s.retryOwners = nil
	outcome := s.forgetAll(runCtx, owners)
	run.forgotten += outcome.forgotten
	run.failures = append(run.failures, outcome.failures...)
	s.keepForRetry(ctx, outcome.failed)
	s.keepForRetry(ctx, outcome.unattempted)
	if outcome.interrupted() {
		s.recordRunInterrupted(ctx, runCtx, run)
		return false
	}
	return true
}

func (s *Sweeper) scanOwners(ctx, runCtx context.Context, run *sweepRun) {
	for {
		page, err := s.identities.ListOwnersWithoutIdentity(runCtx, run.cursor, s.scanWindow)
		if err != nil {
			s.recordScanFailure(ctx, runCtx, run, err)
			return
		}
		if !s.erasePage(ctx, runCtx, run, page) {
			return
		}
		run.cursor = page.Next
		if page.Done {
			run.cursor = ids.UserId{}
			return
		}
	}
}

func (s *Sweeper) erasePage(ctx, runCtx context.Context, run *sweepRun, page ports.OwnerScanPage) bool {
	outcome := s.forgetAll(runCtx, page.Orphaned)
	run.forgotten += outcome.forgotten
	run.failures = append(run.failures, outcome.failures...)
	s.keepForRetry(ctx, outcome.failed)
	if outcome.interrupted() {
		s.recordRunInterrupted(ctx, runCtx, run)
		return false
	}
	return true
}

func (s *Sweeper) keepForRetry(ctx context.Context, owners []ids.UserId) {
	for _, owner := range owners {
		if slices.Contains(s.retryOwners, owner) {
			continue
		}
		if len(s.retryOwners) >= s.retryCap {
			slog.WarnContext(ctx, "playback.deleted_identity_retry_owner_dropped", "user_id", owner.String())
			s.metrics.RetryOwnerDropped()
			continue
		}
		s.retryOwners = append(s.retryOwners, owner)
	}
}

func (s *Sweeper) recordScanFailure(ctx, runCtx context.Context, run *sweepRun, err error) {
	switch {
	case errors.Is(err, ports.ErrIdentityStoreUnavailable):
		slog.WarnContext(ctx, "playback.deleted_identity_sweep_idle", "error", err)
		s.metrics.SweepIdle()
		run.failures = append(run.failures, fmt.Errorf("deleted identity sweep idle: %w", ports.ErrIdentityStoreUnavailable))
	case ctx.Err() == nil && runCtx.Err() != nil:
		logRunBudgetSpent(ctx, run)
	default:
		s.recordOwnerScanTimeout(ctx, err)
		run.failures = append(run.failures, fmt.Errorf("list deleted identities: %w", err))
	}
}

func (s *Sweeper) recordRunInterrupted(ctx, runCtx context.Context, run *sweepRun) {
	if err := ctx.Err(); err != nil {
		run.failures = append(run.failures, fmt.Errorf("forget deleted identities: %w", err))
		return
	}
	logRunBudgetSpent(ctx, run)
}

func logRunBudgetSpent(ctx context.Context, run *sweepRun) {
	slog.InfoContext(ctx, "playback.deleted_identity_sweep_budget_spent", "accounts", run.forgotten)
}

func (s *Sweeper) recordOwnerScanTimeout(ctx context.Context, err error) {
	if !errors.Is(err, context.DeadlineExceeded) || ctx.Err() != nil {
		return
	}
	slog.WarnContext(ctx, "playback.deleted_identity_owner_scan_timed_out", "error", err)
	s.metrics.OwnerScanTimedOut()
}

func (s *Sweeper) reapErasedStates(ctx context.Context) error {
	reaped, err := s.reaper.ReapErasedStates(ctx)
	if err != nil {
		return fmt.Errorf("reap erased queue states: %w", err)
	}
	if reaped > 0 {
		slog.InfoContext(ctx, "playback.erased_queue_states_reaped", "rows", reaped)
	}
	return nil
}

func (s *Sweeper) forgetAll(ctx context.Context, owners []ids.UserId) eraseOutcome {
	var outcome eraseOutcome
	for i, owner := range owners {
		if ctx.Err() != nil {
			outcome.unattempted = owners[i:]
			return outcome
		}
		if err := s.queue.ForgetDeletedAccount(ctx, owner); err != nil {
			if ctx.Err() != nil {
				outcome.unattempted = owners[i:]
				return outcome
			}
			outcome.failures = append(outcome.failures, fmt.Errorf("forget deleted identity: %w", err))
			outcome.failed = append(outcome.failed, owner)
			continue
		}
		outcome.forgotten++
		s.metrics.QueueStateErased(1)
	}
	return outcome
}

func logDeletedIdentitySweep(ctx context.Context, forgotten int) {
	if forgotten == 0 {
		return
	}
	slog.InfoContext(ctx, "playback.deleted_identity_queue_state_erased", "accounts", forgotten)
}
