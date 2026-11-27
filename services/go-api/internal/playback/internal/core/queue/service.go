package queue

import (
	"altune/go-api/internal/playback/internal/core/domain"
	"altune/go-api/internal/playback/internal/core/ports"
	"altune/go-api/internal/shared/ids"
	"altune/go-api/internal/shared/logging"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"
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
	repo       ports.QueueStateRepository
	nowPlaying ports.NowPlayingReader
	now        func() time.Time
}

var ErrClockRequired = errors.New("queue: the queue service requires a clock")

func New(repo ports.QueueStateRepository, nowPlaying ports.NowPlayingReader, now func() time.Time) (*Service, error) {
	if now == nil {
		return nil, fmt.Errorf("queue.New: %w", ErrClockRequired)
	}
	return &Service{repo: repo, nowPlaying: nowPlaying, now: now}, nil
}

func MustNew(repo ports.QueueStateRepository, nowPlaying ports.NowPlayingReader, now func() time.Time) *Service {
	s, err := New(repo, nowPlaying, now)
	if err != nil {
		panic(err)
	}
	return s
}

func (s *Service) Save(ctx context.Context, userId ids.UserId, input SaveQueueStateInput) error {
	state, err := newQueueState(userId, input, s.now())
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

func newQueueState(userId ids.UserId, input SaveQueueStateInput, now time.Time) (*domain.QueueState, error) {
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
	}, now)
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

func (s *Service) SavePosition(ctx context.Context, userId ids.UserId, input SaveQueuePositionInput) error {
	position, err := domain.NewQueuePosition(domain.QueuePositionInput{
		UserId:         userId,
		CurrentIdx:     input.CurrentIdx,
		CurrentTrackId: input.CurrentTrackId,
		PositionMs:     input.PositionMs,
	}, s.now())
	if err != nil {
		return fmt.Errorf("invalid queue position: %w", err)
	}
	return s.repo.UpdatePosition(ctx, position)
}

func (s *Service) Resume(ctx context.Context, userId ids.UserId) (*domain.QueueState, error) {
	state, err := s.repo.GetForUser(ctx, userId)
	if errors.Is(err, ports.ErrCorruptStoredState) {
		slog.ErrorContext(ctx, "resume.corrupt_stored_queue_state",
			logging.KeyError, err)
		return domain.EmptyQueueState(userId, s.now()), nil
	}
	if errors.Is(err, ports.ErrQueueStateNotFound) {
		return domain.EmptyQueueState(userId, s.now()), nil
	}
	if err != nil {
		return nil, err
	}
	return state, nil
}

type ResumeView struct {
	State                   *domain.QueueState
	CurrentTrack            *ports.NowPlayingTrack
	CurrentTrackUnavailable bool
}

func (s *Service) ResumeView(ctx context.Context, userId ids.UserId) (*ResumeView, error) {
	state, err := s.Resume(ctx, userId)
	if err != nil {
		return nil, err
	}

	view := &ResumeView{State: state}
	trackId, isPlaying := state.CurrentTrackId()
	if !isPlaying {
		return view, nil
	}

	current, err := s.nowPlaying.Lookup(ctx, userId, trackId)
	if errors.Is(err, ports.ErrNowPlayingTrackNotFound) {
		return view, nil
	}
	if err != nil {
		slog.WarnContext(ctx, "resume.current_track_enrichment_failed",
			logging.KeyUserID, userId.String(), logging.KeyError, err)
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

func (s *Service) Forget(ctx context.Context, userId ids.UserId) error {
	return s.forget(ctx, userId, forgetTriggerSelf)
}

func (s *Service) ForgetDeletedAccount(ctx context.Context, userId ids.UserId) error {
	return s.forget(ctx, userId, forgetTriggerSweep)
}

func (s *Service) forget(ctx context.Context, userId ids.UserId, trigger string) error {
	if err := s.repo.DeleteForUser(ctx, userId); err != nil {
		return err
	}
	auditQueueStateForgotten(ctx, userId, trigger)
	return nil
}

func auditQueueStateForgotten(ctx context.Context, userId ids.UserId, trigger string) {
	slog.InfoContext(ctx, "playback.queue_state_forgotten",
		"action", forgetQueueStateAction,
		logging.KeyUserID, userId.String(),
		"object", "playback_queue_state",
		"trigger", trigger)
}
