package service

import (
	"altune/go-api/internal/playback/domain"
	"altune/go-api/internal/playback/ports"
	"altune/go-api/internal/shared"
	"context"
	"errors"
	"fmt"
	"log/slog"
)

type SaveQueueStateInput struct {
	TrackIds     []string
	CurrentIdx   int
	PositionMs   int64
	Shuffled     bool
	RepeatMode   string
	SourceId     string
	NaturalOrder []string
}

type QueueService struct {
	repo               ports.QueueStateRepository
	nowPlaying         ports.NowPlayingReader
	enrichmentDisabled bool
}

type QueueServiceOption func(*QueueService)

func WithNowPlayingEnrichment(enabled bool) QueueServiceOption {
	return func(s *QueueService) { s.enrichmentDisabled = !enabled }
}

func NewQueueService(repo ports.QueueStateRepository, nowPlaying ports.NowPlayingReader, opts ...QueueServiceOption) *QueueService {
	s := &QueueService{repo: repo, nowPlaying: nowPlaying}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

func (s *QueueService) Save(ctx context.Context, userId shared.UserId, input SaveQueueStateInput) error {
	rm, err := domain.ParseRepeatMode(input.RepeatMode)
	if err != nil {
		return fmt.Errorf("invalid repeat mode: %w", err)
	}
	state, err := domain.NewQueueState(domain.QueueStateInput{
		UserId:       userId,
		TrackIds:     input.TrackIds,
		CurrentIdx:   input.CurrentIdx,
		PositionMs:   input.PositionMs,
		Shuffled:     input.Shuffled,
		RepeatMode:   rm,
		SourceId:     input.SourceId,
		NaturalOrder: input.NaturalOrder,
	})
	if err != nil {
		return fmt.Errorf("invalid queue state: %w", err)
	}
	return s.repo.Upsert(ctx, state)
}

type SaveQueuePositionInput struct {
	CurrentIdx     int
	CurrentTrackId string
	PositionMs     int64
}

func (s *QueueService) SavePosition(ctx context.Context, userId shared.UserId, input SaveQueuePositionInput) error {
	position, err := domain.NewQueuePosition(domain.QueuePositionInput{
		UserId:         userId,
		CurrentIdx:     input.CurrentIdx,
		CurrentTrackId: input.CurrentTrackId,
		PositionMs:     input.PositionMs,
	})
	if err != nil {
		return fmt.Errorf("invalid queue position: %w", err)
	}
	return s.repo.UpdatePosition(ctx, position)
}

func (s *QueueService) Resume(ctx context.Context, userId shared.UserId) (*domain.QueueState, error) {
	state, err := s.repo.GetForUser(ctx, userId)
	if errors.Is(err, ports.ErrCorruptStoredState) {
		slog.ErrorContext(ctx, "resume.corrupt_stored_queue_state",
			"user_id", userId.String(), "error", err)
		return domain.EmptyQueueState(userId), nil
	}
	if err != nil {
		return nil, err
	}
	if state == nil {
		return domain.EmptyQueueState(userId), nil
	}
	return state, nil
}

type ResumeView struct {
	State                   *domain.QueueState
	CurrentTrack            *ports.NowPlayingTrack
	CurrentTrackUnavailable bool
}

func (s *QueueService) ResumeView(ctx context.Context, userId shared.UserId) (*ResumeView, error) {
	state, err := s.Resume(ctx, userId)
	if err != nil {
		return nil, err
	}

	view := &ResumeView{State: state}
	trackId, isPlaying := state.CurrentTrackId()
	if !isPlaying || s.enrichmentDisabled {
		return view, nil
	}

	current, err := s.nowPlaying.Lookup(ctx, userId, trackId)
	if err != nil {
		slog.WarnContext(ctx, "resume.current_track_enrichment_failed",
			"user_id", userId.String(), "error", err)
		view.CurrentTrackUnavailable = true
		return view, nil
	}
	view.CurrentTrack = current
	return view, nil
}

const forgetQueueStateAction = "queue_state.forget"

func (s *QueueService) Forget(ctx context.Context, userId shared.UserId) error {
	if err := s.repo.DeleteForUser(ctx, userId); err != nil {
		return err
	}
	auditQueueStateForgotten(ctx, userId)
	return nil
}

func auditQueueStateForgotten(ctx context.Context, userId shared.UserId) {
	slog.InfoContext(ctx, "playback.queue_state_forgotten",
		"action", forgetQueueStateAction,
		"user_id", userId.String(),
		"object", "playback_queue_state")
}
