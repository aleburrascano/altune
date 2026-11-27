package service

import (
	"altune/go-api/internal/shared/ids"
	"altune/go-api/internal/shared/logging"
	"context"
	"log/slog"
)

const forgetQueueStateAction = "queue_state.forget"

const (
	forgetTriggerSelf  = "self"
	forgetTriggerSweep = "sweep"
)

func (s *QueueService) Forget(ctx context.Context, userId ids.UserId) error {
	return s.forget(ctx, userId, forgetTriggerSelf)
}

func (s *QueueService) ForgetDeletedAccount(ctx context.Context, userId ids.UserId) error {
	return s.forget(ctx, userId, forgetTriggerSweep)
}

func (s *QueueService) forget(ctx context.Context, userId ids.UserId, trigger string) error {
	if err := s.repo.DeleteForUser(ctx, userId); err != nil {
		return err
	}
	auditQueueStateForgotten(ctx, userId, trigger)
	return nil
}

func auditQueueStateForgotten(ctx context.Context, userId ids.UserId, trigger string) {
	ctx = logging.ContextWithSubjectUser(ctx, userId.String())
	slog.InfoContext(ctx, "playback.queue_state_forgotten",
		"action", forgetQueueStateAction,
		"object", "playback_queue_state",
		"trigger", trigger)
}
