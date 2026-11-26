package downloadfromlibrary

import (
	"altune/go-api/internal/download"
	"altune/go-api/internal/library"
	"altune/go-api/internal/library/domain"
	"altune/go-api/internal/shared/clock"
	"altune/go-api/internal/shared/ids"
	"altune/go-api/internal/shared/redact"
	"altune/go-api/internal/shared/tracklength"
	"context"
	"errors"
	"fmt"
	"log/slog"
)

const maxAttempts = 5

type TrackStore interface {
	GetByID(ctx context.Context, id domain.TrackId, userId ids.UserId) (*domain.Track, error)
	Update(ctx context.Context, track *domain.Track, expectedVersion int) error
	UpdateFenced(ctx context.Context, track *domain.Track, expectedVersion, fence int) error
	AudioRefInUse(ctx context.Context, audioRef string, excludeTrackID domain.TrackId) (bool, error)
	Requeue(ctx context.Context, id domain.TrackId) error
}

type LibraryTracks struct {
	store TrackStore
	now   clock.Func
	state download.UpgradeState
}

func NewLibraryTracks(store TrackStore, now clock.Func, state download.UpgradeState) *LibraryTracks {
	if now == nil {
		panic("downloadfromlibrary: NewLibraryTracks requires a clock")
	}
	if state == nil {
		panic("downloadfromlibrary: NewLibraryTracks requires the upgrade state, or rejected source keys are silently dropped")
	}
	return &LibraryTracks{store: store, now: now, state: state}
}

type illegalTransitionError struct {
	err error
}

func (e illegalTransitionError) Error() string { return e.err.Error() }

func (e illegalTransitionError) Unwrap() error { return e.err }

func (e illegalTransitionError) Is(target error) bool {
	return target == download.ErrIllegalTransition
}

func translate(err error) error {
	if errors.Is(err, domain.ErrIllegalAcquisitionTransition) {
		return illegalTransitionError{err: err}
	}
	return err
}

func (l *LibraryTracks) AudioRefInUse(ctx context.Context, audioRef string, excludeTrackID ids.TrackId) (bool, error) {
	return l.store.AudioRefInUse(ctx, audioRef, excludeTrackID)
}

func (l *LibraryTracks) Requeue(ctx context.Context, id ids.TrackId) error {
	return l.store.Requeue(ctx, id)
}

func (l *LibraryTracks) Get(ctx context.Context, id ids.TrackId, userId ids.UserId) (*download.TrackSnapshot, error) {
	track, err := l.store.GetByID(ctx, id, userId)
	if errors.Is(err, domain.ErrTrackNotFound) {
		return nil, download.ErrTrackNotFound
	}
	if err != nil {
		return nil, err
	}
	if track == nil {
		return nil, download.ErrTrackNotFound
	}
	snapshot := snapshotOf(track)
	state, err := l.state.State(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("read upgrade state: %w", err)
	}
	snapshot.RejectedSourceKeys = state.RejectedSourceKeys
	if snapshot.Evidence, err = download.MergeUpgradeEvidence(snapshot.Evidence, state.Evidence); err != nil {
		return nil, fmt.Errorf("merge upgrade evidence: %w", err)
	}
	return snapshot, nil
}

func snapshotOf(track *domain.Track) *download.TrackSnapshot {
	snapshot := catalogFactsOf(track)
	snapshot.AudioRef = track.AudioRef
	snapshot.AudioSourceURL = track.AudioSourceURL
	snapshot.StoredDurationSeconds = track.DurationSeconds
	snapshot.Evidence = track.AcquisitionEvidence()
	snapshot.Status = mapStatus(track.DownloadStatus)
	snapshot.Streamable = track.IsStreamable()
	snapshot.Version = track.Version
	return snapshot
}

func catalogFactsOf(track *domain.Track) *download.TrackSnapshot {
	return &download.TrackSnapshot{
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
	}
}

func snapshotSources(sources []domain.CatalogSource) []download.CatalogSource {
	if len(sources) == 0 {
		return nil
	}
	out := make([]download.CatalogSource, len(sources))
	for i, s := range sources {
		out[i] = download.CatalogSource{Provider: s.Provider, ExternalID: s.ExternalID, URL: s.URL}
	}
	return out
}

func mapStatus(s domain.DownloadStatus) download.TrackStatus {
	switch s {
	case domain.AcquisitionReady:
		return download.TrackReady
	case domain.AcquisitionFailed:
		return download.TrackFailed
	default:
		return download.TrackPending
	}
}

func (l *LibraryTracks) Complete(ctx context.Context, id ids.TrackId, userId ids.UserId, fence download.ClaimNumber, c download.Completion) error {
	if err := l.settleUpgradeState(ctx, id, userId, fence, c); err != nil {
		return err
	}
	return l.loadAndUpdate(ctx, id, userId, fence, func(track *domain.Track) error {
		return complete(ctx, track, c)
	})
}

func (l *LibraryTracks) settleUpgradeState(ctx context.Context, id ids.TrackId, userId ids.UserId, fence download.ClaimNumber, c download.Completion) error {
	reset := evidenceAccepted(c)
	if len(c.RejectedSourceKeys) == 0 && !reset {
		return nil
	}
	track, err := l.store.GetByID(ctx, id, userId)
	if errors.Is(err, domain.ErrTrackNotFound) {
		return download.ErrTrackNotFound
	}
	if err != nil {
		return fmt.Errorf("get track: %w", err)
	}
	if leaseSuperseded(fence, track.AcquisitionFence) {
		return fmt.Errorf("track %s claimed by fence %d, caller holds %d: %w", id, track.AcquisitionFence, fence, download.ErrLeaseLost)
	}
	if err := l.state.SettleAcquisition(ctx, id, c.RejectedSourceKeys, reset); err != nil {
		return fmt.Errorf("settle upgrade state: %w", err)
	}
	return nil
}

func evidenceAccepted(c download.Completion) bool {
	return c.Evidence != nil && (&domain.Track{}).SetAcquisitionConfidence(c.Confidence, c.Evidence) == nil
}

func complete(ctx context.Context, track *domain.Track, c download.Completion) error {
	if err := settleAudio(track, c); err != nil {
		return err
	}
	if c.Duration.Known() {
		if err := track.SetDuration(c.Duration.Seconds()); err != nil {
			return fmt.Errorf("set duration: %w", err)
		}
	}
	recordMetadata(ctx, track, c)
	if c.Relabel != nil {
		if err := relabel(ctx, track, *c.Relabel); err != nil {
			return err
		}
	}
	track.AdoptISRC(c.ISRC)
	recordLengthCheck(ctx, track, c.Duration)
	return nil
}

func relabel(ctx context.Context, track *domain.Track, label download.TrackLabel) error {
	oldMBID, oldTitle := "", track.Title
	if track.CatalogMBID != nil {
		oldMBID = *track.CatalogMBID
	}
	if err := track.RelabelAsRecording(label.MBID, label.Title, label.Album, label.Year, label.ISRC, label.DurationSeconds); err != nil {
		return fmt.Errorf("relabel as recording: %w", err)
	}
	slog.InfoContext(ctx, "acquisition.track_relabelled",
		"track_id", track.ID.String(),
		"old_mbid", oldMBID, "new_mbid", *track.CatalogMBID,
		"old_title", oldTitle, "new_title", track.Title)
	return nil
}

func recordLengthCheck(ctx context.Context, track *domain.Track, measured tracklength.TrackLength) {
	if track.RecordAudioLength(measured) && track.AudioLengthMismatch {
		slog.WarnContext(ctx, "acquisition.audio_length_mismatch",
			"track_id", track.ID.String(), "catalog_seconds", *track.CatalogDurationSeconds, "audio_seconds", measured.Seconds())
	}
}

func settleAudio(track *domain.Track, c download.Completion) error {
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

func recordMetadata(ctx context.Context, track *domain.Track, c download.Completion) {
	track.SetAcquisitionProvenance(c.Provenance)
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

func (l *LibraryTracks) Fail(ctx context.Context, id ids.TrackId, userId ids.UserId, fence download.ClaimNumber, reason string) error {
	return l.loadAndUpdate(ctx, id, userId, fence, func(track *domain.Track) error {
		return track.FailAcquisition(reason)
	})
}

func (l *LibraryTracks) RevertAt(ctx context.Context, snapshot download.TrackSnapshot) error {
	track, err := l.store.GetByID(ctx, snapshot.ID, snapshot.UserId)
	if errors.Is(err, domain.ErrTrackNotFound) {
		return download.ErrTrackNotFound
	}
	if err != nil {
		return fmt.Errorf("get track: %w", err)
	}
	if track.Version != snapshot.Version {
		return fmt.Errorf("revert to pending: %w", library.ErrTrackVersionConflict)
	}
	if err := track.RevertToPending(l.now); err != nil {
		return fmt.Errorf("revert to pending: %w", translate(err))
	}
	if err := l.store.Update(ctx, track, snapshot.Version); err != nil {
		return fmt.Errorf("revert to pending: %w", err)
	}
	return nil
}

func (l *LibraryTracks) loadAndUpdate(ctx context.Context, id ids.TrackId, userId ids.UserId, fence download.ClaimNumber, mutate func(*domain.Track) error) error {
	var lastErr error
	for attempt := 0; attempt < maxAttempts; attempt++ {
		lastErr = l.attempt(ctx, id, userId, fence, mutate)
		if !errors.Is(lastErr, library.ErrTrackVersionConflict) {
			return lastErr
		}
	}
	return fmt.Errorf("update track: exhausted %d CAS attempts: %w", maxAttempts, lastErr)
}

func (l *LibraryTracks) attempt(ctx context.Context, id ids.TrackId, userId ids.UserId, fence download.ClaimNumber, mutate func(*domain.Track) error) error {
	track, err := l.store.GetByID(ctx, id, userId)
	if errors.Is(err, domain.ErrTrackNotFound) {
		return download.ErrTrackNotFound
	}
	if err != nil {
		return fmt.Errorf("get track: %w", err)
	}
	if leaseSuperseded(fence, track.AcquisitionFence) {
		return fmt.Errorf("track %s claimed by fence %d, caller holds %d: %w", id, track.AcquisitionFence, fence, download.ErrLeaseLost)
	}
	expectedVersion := track.Version
	if err := mutate(track); err != nil {
		return translate(err)
	}
	return l.writeFenced(ctx, track, expectedVersion, fence)
}

func (l *LibraryTracks) writeFenced(ctx context.Context, track *domain.Track, expectedVersion int, fence download.ClaimNumber) error {
	if err := l.store.UpdateFenced(ctx, track, expectedVersion, int(fence)); err != nil {
		if errors.Is(err, library.ErrTrackVersionConflict) {
			return err
		}
		if errors.Is(err, library.ErrTrackFenceSuperseded) {
			return fmt.Errorf("%w: %w", download.ErrLeaseLost, err)
		}
		return fmt.Errorf("update track: %w", err)
	}
	return nil
}

func leaseSuperseded(held download.ClaimNumber, current int) bool {
	return held > 0 && current > 0 && current != int(held)
}
