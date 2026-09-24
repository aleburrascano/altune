package service

import (
	"altune/go-api/internal/playback/ports"
	"altune/go-api/internal/shared"
	"context"
	"errors"
	"fmt"
	"log/slog"
)

// deletedIdentityBatch caps how many accounts one run erases. Each erased row
// disappears from the next run's query, so a backlog drains a batch per tick
// instead of one run holding the identity store open for an unbounded scan.
const deletedIdentityBatch = 500

// ForgetDeletedIdentitiesService erases the queue state of accounts deleted
// out-of-band in Supabase, which never tells this service and leaves no cascade
// behind: without this the PII of an account nobody can log into any more
// (#1593) survives until its owner happens to call the self-service erasure
// they no longer have an identity for.
//
// Every erasure runs through QueueService.Forget, so this path leaves the same
// audit record as the self-service route.
type ForgetDeletedIdentitiesService struct {
	identities ports.DeletedIdentityLister
	queue      *QueueService
	metrics    ports.ErasureSweepMetrics
}

type ForgetDeletedIdentitiesOption func(*ForgetDeletedIdentitiesService)

func WithErasureSweepMetrics(m ports.ErasureSweepMetrics) ForgetDeletedIdentitiesOption {
	return func(s *ForgetDeletedIdentitiesService) { s.metrics = m }
}

func NewForgetDeletedIdentitiesService(identities ports.DeletedIdentityLister, queue *QueueService, opts ...ForgetDeletedIdentitiesOption) *ForgetDeletedIdentitiesService {
	s := &ForgetDeletedIdentitiesService{identities: identities, queue: queue, metrics: ports.NoopErasureSweepMetrics()}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// Execute erases one batch and reports how many accounts it forgot. An identity
// store this deployment cannot read erases nothing and is not an error: the
// sweep says so and waits for the next run.
func (s *ForgetDeletedIdentitiesService) Execute(ctx context.Context) (int, error) {
	owners, err := s.identities.ListOwnersWithoutIdentity(ctx, deletedIdentityBatch)
	if errors.Is(err, ports.ErrIdentityStoreUnavailable) {
		slog.WarnContext(ctx, "playback.deleted_identity_sweep_idle", "error", err)
		s.metrics.SweepIdle()
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("list deleted identities: %w", err)
	}
	return s.forgetAll(ctx, owners)
}

// forgetAll erases each owner in turn, stopping at the first failure so the
// erasure is retried on the next run rather than reported as done. The count is
// of erasures that completed, whether or not the run as a whole did.
func (s *ForgetDeletedIdentitiesService) forgetAll(ctx context.Context, owners []shared.UserId) (int, error) {
	forgotten := 0
	for _, owner := range owners {
		if err := ctx.Err(); err != nil {
			return forgotten, fmt.Errorf("forget deleted identities: %w", err)
		}
		if err := s.queue.Forget(ctx, owner); err != nil {
			return forgotten, fmt.Errorf("forget deleted identity: %w", err)
		}
		forgotten++
		s.metrics.QueueStateErased(1)
	}
	logDeletedIdentitySweep(ctx, forgotten)
	return forgotten, nil
}

func logDeletedIdentitySweep(ctx context.Context, forgotten int) {
	if forgotten == 0 {
		return
	}
	slog.InfoContext(ctx, "playback.deleted_identity_queue_state_erased", "accounts", forgotten)
}
