package queue

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
	TrackIds       []string
	CurrentIdx     int
	PositionMs     int64
	Shuffled       bool
	RepeatMode     string
	Source         domain.QueueSource
	LegacySourceId string
	NaturalOrder   []string
}

type Service struct {
	repo               ports.QueueStateRepository
	nowPlaying         ports.NowPlayingReader
	enrichmentDisabled bool
}

type Option func(*Service)

func WithNowPlayingEnrichment(enabled bool) Option {
	return func(s *Service) { s.enrichmentDisabled = !enabled }
}

func New(repo ports.QueueStateRepository, nowPlaying ports.NowPlayingReader, opts ...Option) *Service {
	s := &Service{repo: repo, nowPlaying: nowPlaying}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

func (s *Service) Save(ctx context.Context, userId shared.UserId, input SaveQueueStateInput) error {
	state, err := newQueueState(userId, input)
	if err != nil {
		return err
	}
	return s.repo.Upsert(ctx, state)
}

func parseSourceAndRepeat(input SaveQueueStateInput) (string, domain.RepeatMode, error) {
	sourceId, err := domain.FormatQueueSource(input.Source, input.LegacySourceId)
	if err != nil {
		return "", 0, fmt.Errorf("invalid queue source: %w", err)
	}
	rm, err := domain.ParseRepeatMode(input.RepeatMode)
	if err != nil {
		return "", 0, fmt.Errorf("invalid repeat mode: %w", err)
	}
	return sourceId, rm, nil
}

func newQueueState(userId shared.UserId, input SaveQueueStateInput) (*domain.QueueState, error) {
	sourceId, rm, err := parseSourceAndRepeat(input)
	if err != nil {
		return nil, err
	}
	state, err := domain.NewQueueState(domain.QueueStateInput{
		UserId:       userId,
		TrackIds:     input.TrackIds,
		CurrentIdx:   input.CurrentIdx,
		PositionMs:   input.PositionMs,
		Shuffled:     input.Shuffled,
		RepeatMode:   rm,
		SourceId:     sourceId,
		NaturalOrder: input.NaturalOrder,
	})
	if err != nil {
		return nil, fmt.Errorf("invalid queue state: %w", err)
	}
	return state, nil
}

type SaveQueuePositionInput struct {
	CurrentIdx     int
	CurrentTrackId string
	PositionMs     int64
}

func (s *Service) SavePosition(ctx context.Context, userId shared.UserId, input SaveQueuePositionInput) error {
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

func (s *Service) Resume(ctx context.Context, userId shared.UserId) (*domain.QueueState, error) {
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

func (s *Service) ResumeView(ctx context.Context, userId shared.UserId) (*ResumeView, error) {
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

const (
	forgetTriggerSelf  = "self"
	forgetTriggerSweep = "sweep"
)

func (s *Service) Forget(ctx context.Context, userId shared.UserId) error {
	return s.forget(ctx, userId, forgetTriggerSelf)
}

func (s *Service) ForgetDeletedAccount(ctx context.Context, userId shared.UserId) error {
	return s.forget(ctx, userId, forgetTriggerSweep)
}

func (s *Service) forget(ctx context.Context, userId shared.UserId, trigger string) error {
	if err := s.repo.DeleteForUser(ctx, userId); err != nil {
		return err
	}
	auditQueueStateForgotten(ctx, userId, trigger)
	return nil
}

func auditQueueStateForgotten(ctx context.Context, userId shared.UserId, trigger string) {
	slog.InfoContext(ctx, "playback.queue_state_forgotten",
		"action", forgetQueueStateAction,
		"user_id", userId.String(),
		"object", "playback_queue_state",
		"trigger", trigger)
}
