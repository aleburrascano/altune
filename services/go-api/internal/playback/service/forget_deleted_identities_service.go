package service

import (
	"altune/go-api/internal/playback/ports"
	"altune/go-api/internal/shared"
	"context"
	"errors"
	"fmt"
	"log/slog"
)

const (
	deletedIdentityBatch            = 500
	maxDeletedIdentityBatchesPerRun = 20
)

type ForgetDeletedIdentitiesService struct {
	identities ports.DeletedIdentityLister
	queue      *QueueService
	metrics    ports.ErasureSweepMetrics
	reaper     ports.ErasedStateReaper
}

type ForgetDeletedIdentitiesOption func(*ForgetDeletedIdentitiesService)

func WithErasureSweepMetrics(m ports.ErasureSweepMetrics) ForgetDeletedIdentitiesOption {
	return func(s *ForgetDeletedIdentitiesService) { s.metrics = m }
}

func WithErasedStateReaper(r ports.ErasedStateReaper) ForgetDeletedIdentitiesOption {
	return func(s *ForgetDeletedIdentitiesService) { s.reaper = r }
}

func NewForgetDeletedIdentitiesService(identities ports.DeletedIdentityLister, queue *QueueService, opts ...ForgetDeletedIdentitiesOption) *ForgetDeletedIdentitiesService {
	s := &ForgetDeletedIdentitiesService{identities: identities, queue: queue, metrics: ports.NoopErasureSweepMetrics()}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

func (s *ForgetDeletedIdentitiesService) Execute(ctx context.Context) (int, error) {
	forgotten := 0
	var failures []error
	if err := s.reapErasedStates(ctx); err != nil {
		failures = append(failures, err)
	}
	for range maxDeletedIdentityBatchesPerRun {
		owners, err := s.identities.ListOwnersWithoutIdentity(ctx, deletedIdentityBatch)
		if errors.Is(err, ports.ErrIdentityStoreUnavailable) {
			slog.WarnContext(ctx, "playback.deleted_identity_sweep_idle", "error", err)
			s.metrics.SweepIdle()
			failures = append(failures, fmt.Errorf("deleted identity sweep idle: %w", ports.ErrIdentityStoreUnavailable))
			break
		}
		if err != nil {
			failures = append(failures, fmt.Errorf("list deleted identities: %w", err))
			break
		}
		erased, batchFailures := s.forgetAll(ctx, owners)
		forgotten += erased
		failures = append(failures, batchFailures...)
		if len(owners) < deletedIdentityBatch || erased == 0 || ctx.Err() != nil {
			break
		}
	}
	logDeletedIdentitySweep(ctx, forgotten)
	return forgotten, errors.Join(failures...)
}

func (s *ForgetDeletedIdentitiesService) reapErasedStates(ctx context.Context) error {
	if s.reaper == nil {
		return nil
	}
	reaped, err := s.reaper.ReapErasedStates(ctx)
	if err != nil {
		return fmt.Errorf("reap erased queue states: %w", err)
	}
	if reaped > 0 {
		slog.InfoContext(ctx, "playback.erased_queue_states_reaped", "rows", reaped)
	}
	return nil
}

func (s *ForgetDeletedIdentitiesService) forgetAll(ctx context.Context, owners []shared.UserId) (int, []error) {
	forgotten := 0
	var failures []error
	for _, owner := range owners {
		if err := ctx.Err(); err != nil {
			return forgotten, append(failures, fmt.Errorf("forget deleted identities: %w", err))
		}
		if err := s.queue.Forget(ctx, owner); err != nil {
			failures = append(failures, fmt.Errorf("forget deleted identity: %w", err))
			continue
		}
		forgotten++
		s.metrics.QueueStateErased(1)
	}
	return forgotten, failures
}

func logDeletedIdentitySweep(ctx context.Context, forgotten int) {
	if forgotten == 0 {
		return
	}
	slog.InfoContext(ctx, "playback.deleted_identity_queue_state_erased", "accounts", forgotten)
}
