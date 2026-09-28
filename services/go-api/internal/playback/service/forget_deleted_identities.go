package service

import (
	"altune/go-api/internal/playback/ports"
	"altune/go-api/internal/shared"
	"context"
	"errors"
	"fmt"
	"log/slog"
)

const deletedIdentityBatch = 500

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
