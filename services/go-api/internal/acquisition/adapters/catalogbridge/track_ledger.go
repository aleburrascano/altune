package catalogbridge

import (
	"altune/go-api/internal/acquisition/ports"
	"altune/go-api/internal/catalog/domain"
	catalogports "altune/go-api/internal/catalog/ports"
	"altune/go-api/internal/shared"
	"altune/go-api/internal/shared/redact"
	"context"
	"errors"
	"fmt"
	"log/slog"
)

const maxAttempts = 5

type TrackStore interface {
	GetByID(ctx context.Context, id domain.TrackId, userId shared.UserId) (*domain.Track, error)
	Update(ctx context.Context, track *domain.Track, expectedVersion int) error
	AudioRefInUse(ctx context.Context, audioRef string, excludeTrackID domain.TrackId) (bool, error)
}

type TrackLedger struct {
	store TrackStore
}

var _ ports.TrackLedger = (*TrackLedger)(nil)

func NewTrackLedger(store TrackStore) *TrackLedger {
	return &TrackLedger{store: store}
}

type illegalTransitionError struct {
	err error
}

func (e illegalTransitionError) Error() string { return e.err.Error() }

func (e illegalTransitionError) Unwrap() error { return e.err }

func (e illegalTransitionError) Is(target error) bool { return target == ports.ErrIllegalTransition }

func translate(err error) error {
	if errors.Is(err, domain.ErrIllegalAcquisitionTransition) {
		return illegalTransitionError{err: err}
	}
	return err
}

func (l *TrackLedger) AudioRefInUse(ctx context.Context, audioRef string, excludeTrackID shared.TrackId) (bool, error) {
	return l.store.AudioRefInUse(ctx, audioRef, excludeTrackID)
}

func (l *TrackLedger) Get(ctx context.Context, id shared.TrackId, userId shared.UserId) (*ports.TrackSnapshot, error) {
	track, err := l.store.GetByID(ctx, id, userId)
	if err != nil {
		return nil, err
	}
	if track == nil {
		return nil, nil
	}
	return snapshotOf(track), nil
}

func snapshotOf(track *domain.Track) *ports.TrackSnapshot {
	return &ports.TrackSnapshot{
		ID:                 track.ID,
		UserId:             track.UserId,
		Title:              track.Title,
		Artist:             track.Artist,
		Album:              track.Album,
		DurationSeconds:    track.DurationSeconds,
		ISRC:               track.ISRC,
		Year:               track.Year,
		TrackNumber:        track.TrackNumber,
		AlbumArtist:        track.AlbumArtist,
		Genre:              track.Genre,
		AudioRef:           track.AudioRef,
		AudioSourceURL:     track.AudioSourceURL,
		RejectedSourceKeys: track.RejectedSourceKeys,
		Status:             mapStatus(track.AcquisitionStatus),
		Streamable:         track.IsStreamable(),
		Version:            track.Version,
	}
}

func mapStatus(s domain.AcquisitionStatus) ports.TrackStatus {
	switch s {
	case domain.AcquisitionReady:
		return ports.TrackReady
	case domain.AcquisitionFailed:
		return ports.TrackFailed
	default:
		return ports.TrackPending
	}
}

func (l *TrackLedger) Complete(ctx context.Context, id shared.TrackId, userId shared.UserId, c ports.Completion) error {
	return l.loadAndUpdate(ctx, id, userId, func(track *domain.Track) error {
		return complete(ctx, track, c)
	})
}

func complete(ctx context.Context, track *domain.Track, c ports.Completion) error {
	if err := settleAudio(track, c); err != nil {
		return err
	}
	recordMetadata(ctx, track, c)
	return nil
}

func settleAudio(track *domain.Track, c ports.Completion) error {
	if c.ReplaceAudio {
		if err := track.ReplaceAudio(c.AudioRef); err != nil {
			return fmt.Errorf("replace audio: %w", err)
		}
		return nil
	}
	if err := track.MarkReady(c.AudioRef); err != nil {
		return fmt.Errorf("mark ready: %w", err)
	}
	return nil
}

func recordMetadata(ctx context.Context, track *domain.Track, c ports.Completion) {
	if c.Duration > 0 {
		if err := track.SetDuration(c.Duration); err != nil {
			slog.WarnContext(ctx, "acquisition.duration_rejected",
				"track_id", track.ID.String(), "duration", c.Duration, "error", redact.LogError(err))
		}
	}
	track.SetAcquisitionProvenance(c.Provenance)
	for _, key := range c.RejectedSourceKeys {
		track.RejectAudioSource(key)
	}
	if c.SourceURL != "" {
		track.SetAudioSource(c.SourceURL)
	}
	if c.Evidence != nil {
		if err := track.SetAcquisitionConfidence(c.Confidence, c.Evidence); err != nil {
			slog.WarnContext(ctx, "acquisition.confidence_rejected",
				"track_id", track.ID.String(), "confidence", c.Confidence, "error", redact.LogError(err))
		}
	}
}

func (l *TrackLedger) Fail(ctx context.Context, id shared.TrackId, userId shared.UserId, reason string) error {
	return l.loadAndUpdate(ctx, id, userId, func(track *domain.Track) error {
		return track.FailAcquisition(reason)
	})
}

func (l *TrackLedger) Revert(ctx context.Context, id shared.TrackId, userId shared.UserId) error {
	return l.loadAndUpdate(ctx, id, userId, func(track *domain.Track) error {
		return track.RevertToPending()
	})
}

func (l *TrackLedger) RevertAt(ctx context.Context, snapshot ports.TrackSnapshot) error {
	track, err := l.store.GetByID(ctx, snapshot.ID, snapshot.UserId)
	if err != nil {
		return fmt.Errorf("get track: %w", err)
	}
	if track == nil {
		return ports.ErrTrackNotFound
	}
	if track.Version != snapshot.Version {
		return fmt.Errorf("revert to pending: %w", catalogports.ErrTrackVersionConflict)
	}
	if err := track.RevertToPending(); err != nil {
		return fmt.Errorf("revert to pending: %w", translate(err))
	}
	if err := l.store.Update(ctx, track, snapshot.Version); err != nil {
		return fmt.Errorf("revert to pending: %w", err)
	}
	return nil
}

func (l *TrackLedger) loadAndUpdate(ctx context.Context, id shared.TrackId, userId shared.UserId, mutate func(*domain.Track) error) error {
	var lastErr error
	for attempt := 0; attempt < maxAttempts; attempt++ {
		lastErr = l.attempt(ctx, id, userId, mutate)
		if !errors.Is(lastErr, catalogports.ErrTrackVersionConflict) {
			return lastErr
		}
	}
	return fmt.Errorf("update track: exhausted %d CAS attempts: %w", maxAttempts, lastErr)
}

func (l *TrackLedger) attempt(ctx context.Context, id shared.TrackId, userId shared.UserId, mutate func(*domain.Track) error) error {
	track, err := l.store.GetByID(ctx, id, userId)
	if err != nil {
		return fmt.Errorf("get track: %w", err)
	}
	if track == nil {
		return ports.ErrTrackNotFound
	}
	expectedVersion := track.Version
	if err := mutate(track); err != nil {
		return translate(err)
	}
	if err := l.store.Update(ctx, track, expectedVersion); err != nil {
		if errors.Is(err, catalogports.ErrTrackVersionConflict) {
			return err
		}
		return fmt.Errorf("update track: %w", err)
	}
	return nil
}
