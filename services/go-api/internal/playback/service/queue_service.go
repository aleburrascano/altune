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

// QueueServiceOption configures optional QueueService behavior.
type QueueServiceOption func(*QueueService)

// WithNowPlayingEnrichment toggles the best-effort now-playing lookup in
// ResumeView (PLAYBACK_NOW_PLAYING_ENRICHMENT_ENABLED). Enabled by default.
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

// SaveQueuePositionInput is a position-only save: the index and position
// within the queue already stored, plus the id of the track at that index.
type SaveQueuePositionInput struct {
	CurrentIdx     int
	CurrentTrackId string
	PositionMs     int64
}

// SavePosition persists only where playback is within the stored queue. It
// skips everything Save pays for the track lists (validation, encoding and the
// write), and returns domain.ErrStaleQueueWrite or
// domain.ErrQueuePositionMismatch, both 409s, when nothing was written.
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
		// The stored queue is a resumable-position cache, not a ledger:
		// losing it just means starting fresh, so a poisoned row must not
		// permanently fail this user's resume. The repository adapter counts
		// each occurrence (playback_corrupt_stored_state_total).
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

// ResumeView is the resumable queue plus its now-playing enrichment.
// CurrentTrack is nil both when nothing is playing and when the track is
// absent; CurrentTrackUnavailable is true only when the enrichment lookup
// itself failed (a dependency fault), so callers can tell a transient outage
// from "no current track" while the resume still succeeds.
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
		// Disabled enrichment is an operator decision to shed catalog load, not
		// a dependency fault, so CurrentTrackUnavailable stays false: flagging
		// it would invite clients to retry into the very load being shed.
		return view, nil
	}

	current, err := s.nowPlaying.Lookup(ctx, userId, trackId)
	if err != nil {
		// The track id is deliberately not logged: it belongs to the stored
		// queue that Forget treats as PII, and user_id already scopes the line.
		// The rate of this path is counted by the now-playing reader adapter
		// (playback_now_playing_enrichment_failures_total).
		slog.WarnContext(ctx, "resume.current_track_enrichment_failed",
			"user_id", userId.String(), "error", err)
		view.CurrentTrackUnavailable = true
		return view, nil
	}
	view.CurrentTrack = current
	return view, nil
}

// forgetQueueStateAction names the erasure in audit records, so every trace of
// it is reachable by one action value however Forget was reached.
const forgetQueueStateAction = "queue_state.forget"

// Forget erases the user's persisted queue state. This is the erasure
// entrypoint for right-to-be-forgotten flows: the stored queue holds PII
// (full track list, natural order, and a free-text search source_id). It is
// reachable via the authenticated self-service DELETE /queue-state route and,
// for an identity deleted out-of-band in Supabase without one,
// ForgetDeletedIdentitiesService.
//
// The erasure also fences the saves already in flight when it runs: a save
// handled before it is rejected (409) afterwards instead of recreating the
// erased row, while a save handled after it applies (#1594). Both callers get
// that ordering, so a sweep erasing an account the owner is still writing from
// cannot be undone by the write that was in the air.
func (s *QueueService) Forget(ctx context.Context, userId shared.UserId) error {
	if err := s.repo.DeleteForUser(ctx, userId); err != nil {
		return err
	}
	auditQueueStateForgotten(ctx, userId)
	return nil
}

// auditQueueStateForgotten is the only trace an erasure leaves, the erased row
// having been the record of itself (#1567). It runs after the erasure returns so
// no record can claim an erasure that did not happen, and runs even when no row
// existed: the auditable event is the erasure completing. On a self-service
// erasure the actor also owns the object, so user_id carries both; on the
// deleted-identity sweep the actor is this service and the erased account no
// longer exists to be named, so user_id names the object's owner alone. The erased
// queue is the PII, so the line names the object, never its contents (#1097).
func auditQueueStateForgotten(ctx context.Context, userId shared.UserId) {
	slog.InfoContext(ctx, "playback.queue_state_forgotten",
		"action", forgetQueueStateAction,
		"user_id", userId.String(),
		"object", "playback_queue_state")
}
