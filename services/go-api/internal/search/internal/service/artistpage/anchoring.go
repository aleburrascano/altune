package artistpage

import (
	"altune/go-api/internal/search/internal/core/albumgrouping"
	"altune/go-api/internal/search/internal/core/ports"
	"altune/go-api/internal/search/internal/domain"
	"altune/go-api/internal/search/internal/service/providerguard"
	"altune/go-api/internal/shared/logging"
	"altune/go-api/internal/shared/redact"
	"context"
	"log/slog"
)

type albumGroupsFetch func(ctx context.Context, identity ResolvedArtistIdentity) []albumgrouping.ReleaseGroup

type Anchoring interface {
	verifyGroups(ctx context.Context, identity ResolvedArtistIdentity, groups []albumgrouping.ReleaseGroup) (out []albumgrouping.ReleaseGroup, anchorFailed bool)
	dropMixedPageTracks(ctx context.Context, identity ResolvedArtistIdentity, trackGroups []albumgrouping.ReleaseGroup, albumGroups albumGroupsFetch) []albumgrouping.ReleaseGroup
}

type NoAnchoring struct{}

func (NoAnchoring) verifyGroups(_ context.Context, _ ResolvedArtistIdentity, groups []albumgrouping.ReleaseGroup) ([]albumgrouping.ReleaseGroup, bool) {
	return groups, false
}

func (NoAnchoring) dropMixedPageTracks(_ context.Context, _ ResolvedArtistIdentity, trackGroups []albumgrouping.ReleaseGroup, _ albumGroupsFetch) []albumgrouping.ReleaseGroup {
	return trackGroups
}

type MusicBrainzAnchoring struct {
	anchor  ports.MusicBrainzDiscographyAnchor
	breaker *providerguard.CircuitBreaker
}

func NewMusicBrainzAnchoring(anchor ports.MusicBrainzDiscographyAnchor, breaker *providerguard.CircuitBreaker) (*MusicBrainzAnchoring, error) {
	if err := requireCollaborators("the MusicBrainz anchoring",
		collaborator{"a MusicBrainz discography anchor", anchor == nil},
		collaborator{"a circuit breaker", breaker == nil},
	); err != nil {
		return nil, err
	}
	return &MusicBrainzAnchoring{anchor: anchor, breaker: breaker}, nil
}

func (a *MusicBrainzAnchoring) releaseGroupTitles(ctx context.Context, mbid string) ([]string, error) {
	return providerguard.GuardedFetch(ctx, a.breaker, domain.ProviderMusicBrainz, func() ([]string, error) {
		return a.anchor.ReleaseGroupTitles(ctx, mbid)
	})
}

func (a *MusicBrainzAnchoring) verifyGroups(ctx context.Context, identity ResolvedArtistIdentity, groups []albumgrouping.ReleaseGroup) ([]albumgrouping.ReleaseGroup, bool) {
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
	return albumgrouping.FilterGroupsByMBAnchor(albumgrouping.NormalizeTitleSet(titles), groups), false
}

func (a *MusicBrainzAnchoring) dropMixedPageTracks(ctx context.Context, identity ResolvedArtistIdentity, trackGroups []albumgrouping.ReleaseGroup, albumGroups albumGroupsFetch) []albumgrouping.ReleaseGroup {
	if identity.MBID == "" {
		return trackGroups
	}
	albums := albumGroups(ctx, identity)
	titles, err := a.releaseGroupTitles(ctx, identity.MBID)
	if err != nil || len(titles) == 0 {
		return trackGroups
	}
	return albumgrouping.FilterMixedPageTracks(albumgrouping.NormalizeTitleSet(titles), albums, trackGroups)
}
