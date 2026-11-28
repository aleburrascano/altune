package service

import (
	"altune/go-api/internal/playback/internal/domain"
	"altune/go-api/internal/playback/internal/ports"
	"altune/go-api/internal/shared/ids"
	"altune/go-api/internal/shared/logging"
	"context"
	"errors"
	"log/slog"
)

func (s *QueueService) Resume(ctx context.Context, userId ids.UserId) (*domain.QueueState, error) {
	state, err := s.repo.GetForUser(ctx, userId)
	if errors.Is(err, domain.ErrCorruptStoredState) {
		slog.ErrorContext(ctx, "resume.corrupt_stored_queue_state",
			logging.KeyError, err)
		return domain.EmptyQueueState(userId, s.now()), nil
	}
	if errors.Is(err, domain.ErrQueueStateNotFound) {
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

func (s *QueueService) ResumeView(ctx context.Context, userId ids.UserId) (*ResumeView, error) {
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
	if errors.Is(err, domain.ErrNowPlayingTrackNotFound) {
		return view, nil
	}
	if err != nil {
		slog.WarnContext(ctx, "resume.current_track_enrichment_failed",
			logging.KeyError, err)
		view.CurrentTrackUnavailable = true
		return view, nil
	}
	view.CurrentTrack = current
	return view, nil
}
