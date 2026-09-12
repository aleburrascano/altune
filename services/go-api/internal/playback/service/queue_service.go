package service

import (
	"context"
	"fmt"
	"log/slog"

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

func NewQueueService(repo ports.QueueStateRepository, nowPlaying ports.NowPlayingReader) *QueueService {
	return &QueueService{repo: repo, nowPlaying: nowPlaying}
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
	trackId, isPlaying := currentTrackId(state)
	if !isPlaying {
		return view, nil
	}

	current, err := s.nowPlaying.Lookup(ctx, userId, trackId)
	if err != nil {
		slog.WarnContext(ctx, "resume.current_track_enrichment_failed",
			"track_id", trackId, "error", err)
		return view, nil
	}
	view.CurrentTrack = current
	return view, nil
}

// Forget erases the user's persisted queue state. This is the erasure
// entrypoint for account-deletion / right-to-be-forgotten flows: the stored
// queue holds PII (full track list, natural order, and a free-text search
// source_id). Identities are owned out-of-band (Supabase), so no in-repo
// account-deletion sweep calls this yet; wiring it into that sweep is the
// remaining piece (see issue #243 / PR).
func (s *QueueService) Forget(ctx context.Context, userId shared.UserId) error {
	return s.repo.DeleteForUser(ctx, userId)
}

func currentTrackId(state *domain.QueueState) (string, bool) {
	if state.CurrentIdx < 0 || state.CurrentIdx >= len(state.TrackIds) {
		return "", false
	}
	return state.TrackIds[state.CurrentIdx], true
}
