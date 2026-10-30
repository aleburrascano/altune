package unreleased

import (
	"context"
	"log/slog"
	"time"
)

func (s *Service) RefreshDue(ctx context.Context, now time.Time, limit int) error {
	if s.store == nil {
		return nil
	}
	due, err := s.store.Due(ctx, now, limit)
	if err != nil {
		return err
	}
	for _, artist := range due[:min(len(due), max(limit, 0))] {
		if _, err := s.Rebuild(ctx, artist.Key, artist.Name, artist.KnownIDs); err != nil {
			slog.WarnContext(ctx, "unreleased.refresh_build_failed", "provider", artist.Key.Provider, "external_id", artist.Key.ExternalID, "error", err)
		}
	}
	return nil
}
