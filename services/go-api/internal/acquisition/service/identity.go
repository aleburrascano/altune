package service

import (
	"altune/go-api/internal/acquisition/ports"
	"altune/go-api/internal/shared/redact"
	"context"
	"log/slog"
)

func (s *AcquireTrackAudioService) resolveIdentity(ctx context.Context, ac *AcquisitionContext) {
	ResolveIdentity(ctx, s.recordings, s.identifier, ac)
}

func ResolveIdentity(ctx context.Context, recordings ports.RecordingResolver, _ ports.AudioIdentifier, ac *AcquisitionContext) {
	identity, err := recordings.Resolve(ctx, ports.RecordingQuery{
		Title:    ac.Track.Title,
		Artist:   ac.Track.Artist,
		Album:    ac.Track.Album,
		ISRC:     ac.Track.ISRC,
		Duration: ac.Track.Duration,
		MBID:     ac.Track.CatalogMBID,
	})
	if err != nil {
		slog.WarnContext(ctx, "acquisition.identity_resolve_failed",
			"track_id", ac.Track.ID, "error", redact.LogError(err))
	}
	identity = withCatalogPin(identity, ac.Track.CatalogMBID)
	if identity.IsZero() {
		return
	}
	ac.Identity = identity
}

func withCatalogPin(identity ports.RecordingIdentity, pin string) ports.RecordingIdentity {
	if pin == "" || identity.MBID != "" {
		return identity
	}
	identity.MBID = pin
	identity.MBIDs = []string{pin}
	return identity
}
