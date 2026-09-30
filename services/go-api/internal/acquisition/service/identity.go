package service

import (
	"altune/go-api/internal/acquisition/ports"
	"context"
	"log/slog"
)

func (s *AcquireTrackAudioService) resolveIdentity(ctx context.Context, ac *AcquisitionContext) {
	ResolveIdentity(ctx, s.recordings, s.identifier, ac)
}

func ResolveIdentity(ctx context.Context, recordings ports.RecordingResolver, identifier ports.AudioIdentifier, ac *AcquisitionContext) {
	identity, err := recordings.Resolve(ctx, ports.RecordingQuery{
		Title:    ac.Track.Title,
		Artist:   ac.Track.Artist,
		Album:    ac.Track.Album,
		ISRC:     ac.Track.ISRC,
		Duration: ac.Track.Duration,
	})
	if err != nil {
		slog.WarnContext(ctx, "acquisition.identity_resolve_failed",
			"track_id", ac.Track.ID, "error", logSafeError(err))
		return
	}
	if identity.IsZero() {
		return
	}
	adoptIdentity(ctx, ac, identity)
	resolveExpectedCluster(ctx, identifier, ac)
}

func adoptIdentity(ctx context.Context, ac *AcquisitionContext, identity ports.RecordingIdentity) {
	ac.Identity = identity
	if ac.Track.Duration <= 0 && identity.Duration > 0 {
		ac.Track.Duration = identity.Duration
		slog.InfoContext(ctx, "acquisition.identity_supplied_duration",
			"track_id", ac.Track.ID, "duration", identity.Duration)
	}
	if ac.Track.ISRC == "" && identity.ISRC != "" {
		ac.Track.ISRC = identity.ISRC
	}
}

func resolveExpectedCluster(ctx context.Context, identifier ports.AudioIdentifier, ac *AcquisitionContext) {
	if identifier == nil || ac.Identity.MBID == "" {
		return
	}

	cluster, err := identifier.AcoustIDsFor(ctx, ac.Identity.MBID)
	if err != nil {
		slog.WarnContext(ctx, identifyFailureEvent(err, "acquisition.expected_cluster_failed"),
			"track_id", ac.Track.ID, "mbid", ac.Identity.MBID, "error", logSafeError(err))
		return
	}
	if len(cluster) == 0 {
		slog.InfoContext(ctx, "acquisition.expected_cluster_unknown",
			"track_id", ac.Track.ID, "mbid", ac.Identity.MBID)
		return
	}

	ac.Identity.AcoustIDs = cluster
	slog.InfoContext(ctx, "acquisition.expected_cluster_resolved",
		"track_id", ac.Track.ID, "mbid", ac.Identity.MBID, "acoustids", len(cluster))
}
