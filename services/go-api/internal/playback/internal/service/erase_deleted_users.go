package service

import (
	"altune/go-api/internal/playback/internal/domain"
	"altune/go-api/internal/playback/internal/ports"
	"altune/go-api/internal/shared/ids"
	"altune/go-api/internal/shared/logging"
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

type ErasureSweeper struct {
	identities ports.DeletedUserDataLister
	queue      *QueueService
	metrics    ports.ErasureSweepMetrics
	reaper     ports.DeletedUserDataCleaner
	scanWindow int
	runBudget  time.Duration
	retryCap   int

	mu          sync.Mutex
	resumeAfter ids.UserId
	retryOwners []ids.UserId
}

func NewErasureSweeper(
	identities ports.DeletedUserDataLister,
	queues *QueueService,
	metrics ports.ErasureSweepMetrics,
	reaper ports.DeletedUserDataCleaner,
	runBudget time.Duration,
) *ErasureSweeper {
	if identities == nil || queues == nil || metrics == nil || reaper == nil {
		panic("deleteduserdata: NewErasureSweeper requires the identity lister, queue service, metrics and reaper")
	}
	if runBudget <= 0 {
		panic("deleteduserdata: NewErasureSweeper requires a positive run budget")
	}
	return &ErasureSweeper{
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

func (s *ErasureSweeper) Execute(ctx context.Context) (int, error) {
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

func (s *ErasureSweeper) retryFailedOwners(ctx, runCtx context.Context, run *sweepRun) bool {
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

func (s *ErasureSweeper) scanOwners(ctx, runCtx context.Context, run *sweepRun) {
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

func (s *ErasureSweeper) erasePage(ctx, runCtx context.Context, run *sweepRun, page ports.OwnerScanPage) bool {
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

func (s *ErasureSweeper) keepForRetry(ctx context.Context, owners []ids.UserId) {
	for _, owner := range owners {
		if slices.Contains(s.retryOwners, owner) {
			continue
		}
		if len(s.retryOwners) >= s.retryCap {
			slog.WarnContext(logging.ContextWithSubjectUser(ctx, owner.String()), "playback.deleted_identity_retry_owner_dropped")
			s.metrics.RetryOwnerDropped()
			continue
		}
		s.retryOwners = append(s.retryOwners, owner)
	}
}

func (s *ErasureSweeper) recordScanFailure(ctx, runCtx context.Context, run *sweepRun, err error) {
	switch {
	case errors.Is(err, domain.ErrIdentityStoreUnavailable):
		slog.WarnContext(ctx, "playback.deleted_identity_sweep_idle", logging.KeyError, err)
		s.metrics.SweepIdle()
		run.failures = append(run.failures, fmt.Errorf("deleted identity sweep idle: %w", domain.ErrIdentityStoreUnavailable))
	case ctx.Err() == nil && runCtx.Err() != nil:
		logRunBudgetSpent(ctx, run)
	default:
		s.recordOwnerScanTimeout(ctx, err)
		run.failures = append(run.failures, fmt.Errorf("list deleted identities: %w", err))
	}
}

func (s *ErasureSweeper) recordRunInterrupted(ctx, runCtx context.Context, run *sweepRun) {
	if err := ctx.Err(); err != nil {
		run.failures = append(run.failures, fmt.Errorf("forget deleted identities: %w", err))
		return
	}
	logRunBudgetSpent(ctx, run)
}

func logRunBudgetSpent(ctx context.Context, run *sweepRun) {
	slog.InfoContext(ctx, "playback.deleted_identity_sweep_budget_spent", "accounts", run.forgotten)
}

func (s *ErasureSweeper) recordOwnerScanTimeout(ctx context.Context, err error) {
	if !errors.Is(err, context.DeadlineExceeded) || ctx.Err() != nil {
		return
	}
	slog.WarnContext(ctx, "playback.deleted_identity_owner_scan_timed_out", logging.KeyError, err)
	s.metrics.OwnerScanTimedOut()
}

func (s *ErasureSweeper) reapErasedStates(ctx context.Context) error {
	reaped, err := s.reaper.ReapErasedStates(ctx)
	if err != nil {
		return fmt.Errorf("reap erased queue states: %w", err)
	}
	if reaped > 0 {
		slog.InfoContext(ctx, "playback.erased_queue_states_reaped", "rows", reaped)
	}
	return nil
}

func (s *ErasureSweeper) forgetAll(ctx context.Context, owners []ids.UserId) eraseOutcome {
	var outcome eraseOutcome
	for i, owner := range owners {
		if ctx.Err() != nil {
			outcome.unattempted = owners[i:]
			return outcome
		}
		err := s.queue.ForgetDeletedAccount(ctx, owner)
		if err != nil && ctx.Err() != nil {
			outcome.unattempted = owners[i:]
			return outcome
		}
		if err != nil {
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
