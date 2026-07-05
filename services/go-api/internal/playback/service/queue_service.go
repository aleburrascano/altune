package service

import (
	"context"
	"fmt"

	"altune/go-api/internal/playback/domain"
	"altune/go-api/internal/playback/ports"
	"altune/go-api/internal/shared"
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
	repo       ports.QueueStateRepository
	nowPlaying ports.NowPlayingReader
}

// QueueServiceOption configures optional dependencies (functional options — the
// house constructor idiom). The NowPlayingReader is optional so a service built
// without it (tests, environments without the catalog bridge) simply omits the
// current-track snapshot from ResumeView.
type QueueServiceOption func(*QueueService)

// WithNowPlayingReader wires the catalog bridge that lets ResumeView embed the
// currently-playing track's metadata for instant client render.
func WithNowPlayingReader(reader ports.NowPlayingReader) QueueServiceOption {
	return func(s *QueueService) { s.nowPlaying = reader }
}

func NewQueueService(repo ports.QueueStateRepository, opts ...QueueServiceOption) *QueueService {
	s := &QueueService{repo: repo}
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
	state, err := domain.NewQueueState(
		userId, input.TrackIds, input.CurrentIdx, input.PositionMs, input.Shuffled, rm, input.SourceId,
		domain.WithNaturalOrder(input.NaturalOrder),
	)
	if err != nil {
		return fmt.Errorf("invalid queue state: %w", err)
	}
	return s.repo.Upsert(ctx, state)
}

// Resume returns the user's saved snapshot, or the empty snapshot when none is
// stored — callers never receive nil.
func (s *QueueService) Resume(ctx context.Context, userId shared.UserId) (*domain.QueueState, error) {
	state, err := s.repo.GetForUser(ctx, userId)
	if err != nil {
		return nil, err
	}
	if state == nil {
		return domain.EmptyQueueState(userId), nil
	}
	return state, nil
}

// ResumeView is Resume plus the currently-playing track's display snapshot, so the
// client can render now-playing from this one small call instead of blocking on a
// full library rehydrate. CurrentTrack is nil when there is no NowPlayingReader,
// the queue is empty, the index is out of range, or the track can't be found.
type ResumeView struct {
	State        *domain.QueueState
	CurrentTrack *ports.NowPlayingTrack
}

func (s *QueueService) ResumeView(ctx context.Context, userId shared.UserId) (*ResumeView, error) {
	state, err := s.Resume(ctx, userId)
	if err != nil {
		return nil, err
	}

	view := &ResumeView{State: state}
	idx := state.CurrentIdx
	if s.nowPlaying == nil || idx < 0 || idx >= len(state.TrackIds) {
		return view, nil
	}

	current, ok, err := s.nowPlaying.Lookup(ctx, userId, state.TrackIds[idx])
	if err != nil {
		return nil, fmt.Errorf("resume current track: %w", err)
	}
	if ok {
		view.CurrentTrack = current
	}
	return view, nil
}
