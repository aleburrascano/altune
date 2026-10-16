package catalogbridge

import (
	acqdomain "altune/go-api/internal/acquisition/domain"
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
		ID:                     track.ID,
		UserId:                 track.UserId,
		Title:                  track.Title,
		Artist:                 track.Artist,
		Album:                  track.Album,
		CatalogDurationSeconds: track.CatalogDurationSeconds,
		CatalogMBID:            track.CatalogMBID,
		CatalogExplicit:        track.CatalogExplicit,
		CatalogSources:         snapshotSources(track.CatalogSources),
		ISRC:                   track.ISRC,
		Year:                   track.Year,
		TrackNumber:            track.TrackNumber,
		AlbumArtist:            track.AlbumArtist,
		Genre:                  track.Genre,
		AudioRef:               track.AudioRef,
		AudioSourceURL:         track.AudioSourceURL,
		StoredDurationSeconds:  track.DurationSeconds,
		RejectedSourceKeys:     track.RejectedSourceKeys,
		Evidence:               track.AcquisitionEvidence(),
		Status:                 mapStatus(track.AcquisitionStatus),
		Streamable:             track.IsStreamable(),
		Version:                track.Version,
	}
}

func snapshotSources(sources []domain.CatalogSource) []ports.CatalogSource {
	if len(sources) == 0 {
		return nil
	}
	out := make([]ports.CatalogSource, len(sources))
	for i, s := range sources {
		out[i] = ports.CatalogSource{Provider: s.Provider, ExternalID: s.ExternalID, URL: s.URL}
	}
	return out
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

func (l *TrackLedger) Complete(ctx context.Context, id shared.TrackId, userId shared.UserId, fence ports.Fence, c ports.Completion) error {
	return l.loadAndUpdate(ctx, id, userId, fence, func(track *domain.Track) error {
		return complete(ctx, track, c)
	})
}

func complete(ctx context.Context, track *domain.Track, c ports.Completion) error {
	if err := settleAudio(track, c); err != nil {
		return err
	}
	recordMetadata(ctx, track, c)
	track.AdoptISRC(c.ISRC)
	recordLengthCheck(ctx, track, c.Duration)
	return nil
}

func recordLengthCheck(ctx context.Context, track *domain.Track, measuredSeconds float64) {
	if measuredSeconds <= 0 || track.CatalogDurationSeconds == nil {
		return
	}
	toleranceSeconds := acqdomain.LengthAuthoritative.Allowance(*track.CatalogDurationSeconds)
	track.RecordAudioLength(measuredSeconds, toleranceSeconds)
	if track.AudioLengthMismatch {
		slog.WarnContext(ctx, "acquisition.audio_length_mismatch",
			"track_id", track.ID.String(), "catalog_seconds", *track.CatalogDurationSeconds,
			"audio_seconds", measuredSeconds, "tolerance_seconds", toleranceSeconds)
	}
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

func (l *TrackLedger) Fail(ctx context.Context, id shared.TrackId, userId shared.UserId, fence ports.Fence, reason string) error {
	return l.loadAndUpdate(ctx, id, userId, fence, func(track *domain.Track) error {
		return track.FailAcquisition(reason)
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

func (l *TrackLedger) loadAndUpdate(ctx context.Context, id shared.TrackId, userId shared.UserId, fence ports.Fence, mutate func(*domain.Track) error) error {
	var lastErr error
	for attempt := 0; attempt < maxAttempts; attempt++ {
		lastErr = l.attempt(ctx, id, userId, fence, mutate)
		if !errors.Is(lastErr, catalogports.ErrTrackVersionConflict) {
			return lastErr
		}
	}
	return fmt.Errorf("update track: exhausted %d CAS attempts: %w", maxAttempts, lastErr)
}

func (l *TrackLedger) attempt(ctx context.Context, id shared.TrackId, userId shared.UserId, fence ports.Fence, mutate func(*domain.Track) error) error {
	track, err := l.store.GetByID(ctx, id, userId)
	if err != nil {
		return fmt.Errorf("get track: %w", err)
	}
	if track == nil {
		return ports.ErrTrackNotFound
	}
	if leaseSuperseded(fence, track.AcquisitionFence) {
		return fmt.Errorf("track %s claimed by fence %d, caller holds %d: %w", id, track.AcquisitionFence, fence, acqdomain.ErrLeaseLost)
	}
	expectedVersion := track.Version
	if err := mutate(track); err != nil {
		return translate(err)
	}
	return l.write(ctx, track, expectedVersion)
}

func (l *TrackLedger) write(ctx context.Context, track *domain.Track, expectedVersion int) error {
	if err := l.store.Update(ctx, track, expectedVersion); err != nil {
		if errors.Is(err, catalogports.ErrTrackVersionConflict) {
			return err
		}
		return fmt.Errorf("update track: %w", err)
	}
	return nil
}

func leaseSuperseded(held ports.Fence, current int) bool {
	return held > 0 && current > 0 && current != int(held)
}
