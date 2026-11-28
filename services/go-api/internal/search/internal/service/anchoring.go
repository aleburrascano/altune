package service

import (
	"altune/go-api/internal/search/internal/domain"
	"altune/go-api/internal/search/internal/ports"
	"altune/go-api/internal/shared/logging"
	"altune/go-api/internal/shared/redact"
	"context"
	"fmt"
	"log/slog"
)

type albumGroupsFetch func(ctx context.Context, identity ResolvedArtistIdentity) []domain.ReleaseGroup

type Anchoring interface {
	verifyGroups(ctx context.Context, identity ResolvedArtistIdentity, groups []domain.ReleaseGroup) (out []domain.ReleaseGroup, anchorFailed bool)
	dropMixedPageTracks(ctx context.Context, identity ResolvedArtistIdentity, trackGroups []domain.ReleaseGroup, albumGroups albumGroupsFetch) []domain.ReleaseGroup
}

type NoAnchoring struct{}

func (NoAnchoring) verifyGroups(_ context.Context, _ ResolvedArtistIdentity, groups []domain.ReleaseGroup) ([]domain.ReleaseGroup, bool) {
	return groups, false
}

func (NoAnchoring) dropMixedPageTracks(_ context.Context, _ ResolvedArtistIdentity, trackGroups []domain.ReleaseGroup, _ albumGroupsFetch) []domain.ReleaseGroup {
	return trackGroups
}

type MusicBrainzAnchoring struct {
	anchor  ports.MusicBrainzDiscographyAnchor
	breaker *CircuitBreaker
}

func NewMusicBrainzAnchoring(anchor ports.MusicBrainzDiscographyAnchor, breaker *CircuitBreaker) (*MusicBrainzAnchoring, error) {
	if err := requireCollaborators("the MusicBrainz anchoring",
		collaborator{"a MusicBrainz discography anchor", anchor == nil},
		collaborator{"a circuit breaker", breaker == nil},
	); err != nil {
		return nil, err
	}
	return &MusicBrainzAnchoring{anchor: anchor, breaker: breaker}, nil
}

func (a *MusicBrainzAnchoring) releaseGroupTitles(ctx context.Context, mbid string) ([]string, error) {
	r0, err := GuardedFetch(ctx, a.breaker, domain.ProviderMusicBrainz, func() ([]string, error) {
		return a.anchor.ReleaseGroupTitles(ctx, mbid)
	})
	if err != nil {
		return r0, fmt.Errorf("guarded fetch: %w", err)
	}
	return r0, nil
}

func (a *MusicBrainzAnchoring) verifyGroups(ctx context.Context, identity ResolvedArtistIdentity, groups []domain.ReleaseGroup) ([]domain.ReleaseGroup, bool) {
	if identity.MBID == "" {
		return groups, false
	}
	titles, err := a.releaseGroupTitles(ctx, identity.MBID)
	if err != nil {
		if ctx.Err() != nil {
			return groups, false
		}
		slog.WarnContext(ctx, "artist_albums.mb_anchor_failed", "mbid", identity.MBID, logging.KeyError, redact.Secrets(err.Error()))
		return groups, true
	}
	if len(titles) == 0 {
		return groups, false
	}
	return domain.FilterGroupsByMBAnchor(domain.NormalizeTitleSet(titles), groups), false
}

func (a *MusicBrainzAnchoring) dropMixedPageTracks(ctx context.Context, identity ResolvedArtistIdentity, trackGroups []domain.ReleaseGroup, albumGroups albumGroupsFetch) []domain.ReleaseGroup {
	if identity.MBID == "" {
		return trackGroups
	}
	albums := albumGroups(ctx, identity)
	titles, err := a.releaseGroupTitles(ctx, identity.MBID)
	if err != nil || len(titles) == 0 {
		return trackGroups
	}
	return domain.FilterMixedPageTracks(domain.NormalizeTitleSet(titles), albums, trackGroups)
}
