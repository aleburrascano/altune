package service

import (
	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/discovery/ports"
	"altune/go-api/internal/shared/textnorm"
	"cmp"
	"context"
	"errors"
	"log/slog"
	"maps"
	"strings"
	"time"

	"golang.org/x/sync/errgroup"
)

const (
	artworkFillLimit       = 50
	artworkFillConcurrency = 8
	artworkFillTimeout     = 4 * time.Second
	emptyArtHash           = "d41d8cd98f00b204e9800998ecf8427e"
)

type ArtworkFiller struct {
	resolver      ports.TaggingArtworkResolver
	cache         ports.ArtworkCache
	identityStore identityLookup
	mbidIndex     ports.MBIDIndex
}

type identityLookup interface {
	LookupByProviderID(ctx context.Context, kind domain.ResultKind, provider domain.ProviderKey, externalID string) (mbid string, xref map[domain.ProviderKey]string, ok bool)
}

func newArtworkFiller(
	resolver ports.TaggingArtworkResolver,
	cache ports.ArtworkCache,
	identityStore ports.IdentityStore,
	mbidIndex ports.MBIDIndex,
) *ArtworkFiller {
	return &ArtworkFiller{resolver: resolver, cache: cache, identityStore: identityStore, mbidIndex: mbidIndex}
}

func (a *ArtworkFiller) fill(ctx context.Context, results []domain.SearchResult) []domain.SearchResult {
	if a.resolver == nil {
		return results
	}
	limit := artworkFillLimit
	if len(results) < limit {
		limit = len(results)
	}
	if limit == 0 {
		return results
	}

	fillCtx, cancel := context.WithTimeout(ctx, artworkFillTimeout)
	defer cancel()

	top := results[:limit]
	rest := results[limit:]
	filler := a.withPrefetchedIdentities(fillCtx, top)

	var g errgroup.Group
	g.SetLimit(artworkFillConcurrency)
	filled := make([]domain.SearchResult, len(top))

	for i, r := range top {
		filled[i] = r
		g.Go(func() error {
			defer RecoverGoroutine(ctx, "artwork_fill.panic", "title", r.Title)
			filled[i] = filler.fillOne(fillCtx, r)
			return nil
		})
	}
	_ = g.Wait()
	return append(filled, rest...)
}

func (a *ArtworkFiller) withPrefetchedIdentities(ctx context.Context, results []domain.SearchResult) *ArtworkFiller {
	batch, ok := a.identityStore.(ports.BatchIdentityLookup)
	if !ok {
		return a
	}
	refs := durableIdentityRefs(results)
	if len(refs) == 0 {
		return a
	}
	scoped := *a
	scoped.identityStore = prefetchedIdentities(batch.LookupByProviderIDs(ctx, refs))
	return &scoped
}

func durableIdentityRefs(results []domain.SearchResult) []ports.IdentityRef {
	refs := make([]ports.IdentityRef, 0, len(results))
	for _, r := range results {
		if usableArtwork(r.ImageURL) || !needsDurableIdentity(r) {
			continue
		}
		refs = append(refs, durableIdentityRef(r))
	}
	return refs
}

func needsDurableIdentity(r domain.SearchResult) bool {
	return len(r.Xref) == 0 && len(r.Sources) > 0
}

func durableIdentityRef(r domain.SearchResult) ports.IdentityRef {
	src := r.Sources[0]
	return ports.IdentityRef{Kind: r.Kind, Provider: src.Provider.Key(), ExternalID: src.ExternalID}
}

type prefetchedIdentities map[ports.IdentityRef]ports.IdentityHit

func (p prefetchedIdentities) LookupByProviderID(_ context.Context, kind domain.ResultKind, provider domain.ProviderKey, externalID string) (string, map[domain.ProviderKey]string, bool) {
	hit, ok := p[ports.IdentityRef{Kind: kind, Provider: provider, ExternalID: externalID}]
	return hit.MBID, maps.Clone(hit.Xref), ok
}

type artworkStage int

const (
	artworkStageProvider artworkStage = iota
	artworkStageCacheHit
	artworkStageCachedMiss
	artworkStageDegraded
	artworkStageLive
)

type artworkOutcome struct {
	stage       artworkStage
	resolved    string
	confidence  ports.ArtworkConfidence
	fromDurable bool
}

func (o artworkOutcome) path() string {
	switch o.stage {
	case artworkStageProvider:
		return "provider"
	case artworkStageCacheHit:
		return "cache"
	case artworkStageCachedMiss:
		return "none"
	case artworkStageDegraded:
		return "degraded"
	default:
		return artworkPathFor(o.resolved, o.confidence, o.fromDurable)
	}
}

func (a *ArtworkFiller) fillOne(ctx context.Context, result domain.SearchResult) domain.SearchResult {
	outcome := a.runStages(ctx, &result)
	setArtworkPath(&result, outcome.path())
	return result
}

func (a *ArtworkFiller) runStages(ctx context.Context, result *domain.SearchResult) artworkOutcome {
	if usableArtwork(result.ImageURL) {
		defaultArtworkSource(result)
		return artworkOutcome{stage: artworkStageProvider}
	}
	mbid, fromDurable := a.lookupMBID(ctx, result)
	if stage, settled := a.lookupArtworkCache(ctx, result, mbid); settled {
		return artworkOutcome{stage: stage}
	}
	return a.resolveLive(ctx, result, mbid, fromDurable)
}

func usableArtwork(url string) bool {
	return url != "" && !strings.Contains(url, emptyArtHash)
}

func defaultArtworkSource(result *domain.SearchResult) {
	if result.ArtworkSource != "" || len(result.Sources) == 0 {
		return
	}
	result.ArtworkSource = result.Sources[0].Provider.Key()
}

func (a *ArtworkFiller) lookupMBID(ctx context.Context, result *domain.SearchResult) (mbid string, fromDurable bool) {
	durableMBID, fromDurable := a.lookupDurableIdentity(ctx, result)
	mbid = cmp.Or(result.MBID, durableMBID)
	if mbid == "" {
		mbid = a.lookupMBIDIndex(ctx, *result)
	}
	return mbid, fromDurable
}

func (a *ArtworkFiller) lookupDurableIdentity(ctx context.Context, result *domain.SearchResult) (mbid string, ok bool) {
	if a.identityStore == nil || !needsDurableIdentity(*result) {
		return "", false
	}
	ref := durableIdentityRef(*result)
	mbid, xref, ok := a.identityStore.LookupByProviderID(ctx, ref.Kind, ref.Provider, ref.ExternalID)
	if !ok {
		return "", false
	}
	if len(xref) > 0 {
		result.Xref = xref
	}
	slog.DebugContext(ctx, "identity.durable_resolved",
		"kind", result.Kind.String(), "provider", ref.Provider.String(),
		"external_id", ref.ExternalID, "mbid", mbid, "bridged_ids", len(xref))
	return mbid, true
}

func (a *ArtworkFiller) lookupMBIDIndex(ctx context.Context, result domain.SearchResult) string {
	if a.mbidIndex == nil {
		return ""
	}
	mbid, ok := a.mbidIndex.LookupMBID(ctx, result.Kind, textnorm.NameKey(result.Title, result.Subtitle))
	if !ok {
		return ""
	}
	return mbid
}

func (a *ArtworkFiller) lookupArtworkCache(ctx context.Context, result *domain.SearchResult, mbid string) (stage artworkStage, settled bool) {
	if a.cache == nil {
		return artworkStageLive, false
	}
	cachedURL, cachedSource, found, _ := a.cache.Get(ctx, result.Kind, result.Title, result.Subtitle, mbid)
	if !found {
		return artworkStageLive, false
	}
	if usableArtwork(cachedURL) {
		result.ImageURL = cachedURL
		result.ArtworkSource = cachedSource
		return artworkStageCacheHit, true
	}
	if result.Kind != domain.ResultKindArtist {
		return artworkStageCachedMiss, true
	}
	return artworkStageLive, false
}

func (a *ArtworkFiller) resolveLive(ctx context.Context, result *domain.SearchResult, mbid string, fromDurable bool) artworkOutcome {
	resolved, source, confidence, err := a.resolve(ctx, *result, mbid)
	if ports.IsUnverifiedArtworkMiss(resolved, err) {
		slog.WarnContext(ctx, "artwork.not_cached_degraded",
			"kind", result.Kind.String(), "had_mbid", mbid != "", "error", err)
		return artworkOutcome{stage: artworkStageDegraded, fromDurable: fromDurable}
	}
	if a.cache != nil {
		_ = a.cache.Set(ctx, result.Kind, result.Title, result.Subtitle, mbid, resolved, source, confidence)
	}
	applyResolvedArtwork(result, resolved, source)
	slog.DebugContext(ctx, "artwork.enriched",
		"kind", result.Kind.String(), "source", source.String(),
		"resolved", resolved != "", "had_mbid", mbid != "")
	return artworkOutcome{stage: artworkStageLive, resolved: resolved, confidence: confidence, fromDurable: fromDurable}
}

func applyResolvedArtwork(result *domain.SearchResult, url string, source domain.ProviderKey) {
	if url == "" {
		return
	}
	result.ImageURL = url
	result.ArtworkSource = source
}

func setArtworkPath(r *domain.SearchResult, path string) {
	r.PutExtra(domain.ExtraArtworkPath, path)
}

func artworkPathFor(resolved string, confidence ports.ArtworkConfidence, fromDurable bool) string {
	if resolved == "" {
		return "none"
	}
	switch {
	case confidence >= ports.ArtworkConfidenceIdentity && fromDurable:
		return "durable-identity"
	case confidence >= ports.ArtworkConfidenceIdentity:
		return "identity"
	case confidence == ports.ArtworkConfidenceName:
		return "name"
	default:
		return "provider"
	}
}

func (a *ArtworkFiller) resolve(ctx context.Context, result domain.SearchResult, mbid string) (string, domain.ProviderKey, ports.ArtworkConfidence, error) {
	idURL, idSource, idErr := a.resolveByIdentity(ctx, result, mbid)
	if idURL != "" {
		return idURL, idSource, ports.ArtworkConfidenceIdentity, nil
	}
	nameURL, nameSource, nameErr := a.resolver.ResolveTagged(ctx, result.Kind, result.Title, result.Subtitle, mbid)
	if nameURL != "" {
		return nameURL, nameSource, ports.ArtworkConfidenceName, nil
	}
	return "", "", ports.ArtworkConfidenceNone, errors.Join(idErr, nameErr)
}

func (a *ArtworkFiller) resolveByIdentity(ctx context.Context, result domain.SearchResult, mbid string) (string, domain.ProviderKey, error) {
	identity := artworkIdentity(result, mbid)
	if !identity.HasLinks() {
		return "", "", nil
	}
	return a.resolver.ResolveWithIdentityTagged(ctx, result.Kind, result.Title, result.Subtitle, identity)
}

func artworkIdentity(result domain.SearchResult, mbid string) ports.ArtworkIdentity {
	id := ports.ArtworkIdentity{MBID: mbid}
	if len(result.Xref) > 0 {
		id.ExternalIDs = result.Xref
	}
	return id
}
