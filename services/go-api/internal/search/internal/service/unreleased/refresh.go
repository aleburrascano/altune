package unreleased

import (
	"altune/go-api/internal/shared/logging"
	"context"
	"log/slog"
	"time"
)

func (s *Service) RefreshDue(ctx context.Context, now time.Time, limit int) error {
	due, err := s.storage.due(ctx, now, limit)
	if err != nil {
		return err
	}
	for _, artist := range due[:min(len(due), max(limit, 0))] {
		if _, err := s.Rebuild(ctx, artist.Key, artist.Name, artist.KnownIDs); err != nil {
			slog.WarnContext(ctx, "unreleased.refresh_build_failed", "provider", artist.Key.Provider, "external_id", artist.Key.ExternalID, logging.KeyError, err)
		}
	}
	return nil
}
