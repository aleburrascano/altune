package service

import (
	"altune/go-api/internal/catalog/domain"
	"altune/go-api/internal/catalog/ports"
	"context"
	"errors"
	"fmt"
	"log/slog"
)

var (
	ErrTrackNotFound     = &domain.CodedError{Msg: "track not found", Status: 404, Code: "catalog.track_not_found"}
	ErrPlaylistNotFound  = &domain.CodedError{Msg: "playlist not found", Status: 404, Code: "catalog.playlist_not_found"}
	ErrAudioNotAvailable = &domain.CodedError{Msg: "audio not available", Status: 404, Code: "catalog.audio_not_available"}
	// ErrAudioTemporarilyUnavailable signals a transient storage failure where
	// the audio file is not confirmed absent (the stream call failed but the
	// file is present, or its existence could not be verified). The client
	// should retry rather than treat the track as genuinely gone.
	ErrAudioTemporarilyUnavailable = &domain.CodedError{Msg: "audio temporarily unavailable", Status: 503, Code: "catalog.audio_temporarily_unavailable"}
	// ErrCatalogTemporarilyUnavailable signals a transient database failure (the
	// persistence adapter classified it as ports.ErrDBTransient): the call timed
	// out or the connection was lost, so the client should retry rather than
	// treat the request as failed for good.
	ErrCatalogTemporarilyUnavailable = &domain.CodedError{Msg: "catalog temporarily unavailable", Status: 503, Code: "catalog.temporarily_unavailable"}
	// ErrAudioOrphaned signals a partial track deletion: the track row was
	// removed from the database, but its audio object could not be deleted from
	// storage, so a user's personal audio file is left behind as an orphan. It
	// is surfaced (not swallowed as success) so the caller knows the deletion was
	// incomplete; the orphan is counted via the OrphanedDelete metric and emitted
	// on a marked log line (event=catalog.orphaned_audio) for reconciliation.
	ErrAudioOrphaned = &domain.CodedError{Msg: "track deleted but audio file orphaned", Status: 500, Code: "catalog.audio_orphaned"}
)

// wrapRepoError wraps a repository failure for op. A failure the adapter
// classified as transient additionally carries ErrCatalogTemporarilyUnavailable
// (a retryable 503) and is logged, since the HTTP layer does not log coded
// errors; any other failure is wrapped unchanged and surfaces as a 500.
func wrapRepoError(ctx context.Context, op string, err error) error {
	if errors.Is(err, ports.ErrDBTransient) {
		slog.WarnContext(ctx, "catalog.db_transient", "op", op, "error", err)
		return fmt.Errorf("%s: %w: %w", op, ErrCatalogTemporarilyUnavailable, err)
	}
	return fmt.Errorf("%s: %w", op, err)
}
