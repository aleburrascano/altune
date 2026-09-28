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
	ErrTrackNotFound                 = &domain.CodedError{Msg: "track not found", Status: 404, Code: "catalog.track_not_found"}
	ErrPlaylistNotFound              = &domain.CodedError{Msg: "playlist not found", Status: 404, Code: "catalog.playlist_not_found"}
	ErrAudioNotAvailable             = &domain.CodedError{Msg: "audio not available", Status: 404, Code: "catalog.audio_not_available"}
	ErrAudioTemporarilyUnavailable   = &domain.CodedError{Msg: "audio temporarily unavailable", Status: 503, Code: "catalog.audio_temporarily_unavailable"}
	ErrCatalogTemporarilyUnavailable = &domain.CodedError{Msg: "catalog temporarily unavailable", Status: 503, Code: "catalog.temporarily_unavailable"}
	ErrAudioOrphaned                 = &domain.CodedError{Msg: "track deleted but audio file orphaned", Status: 500, Code: "catalog.audio_orphaned"}
)

func wrapRepoError(ctx context.Context, op string, err error) error {
	if errors.Is(err, ports.ErrDBTransient) {
		slog.WarnContext(ctx, "catalog.db_transient", "op", op, "error", err)
		return fmt.Errorf("%s: %w: %w", op, ErrCatalogTemporarilyUnavailable, err)
	}
	return fmt.Errorf("%s: %w", op, err)
}
