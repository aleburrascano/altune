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
	"slices"
	"strings"
	"sync"
	"time"
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

func (a *ArtworkFiller) fill(ctx context.Context, results []domain.SearchResult, bg *backgroundRunner) []domain.SearchResult {
	limit := min(artworkFillLimit, len(results))
	if a.resolver == nil || limit == 0 {
		return results
	}

	fillCtx, cancel := context.WithTimeout(ctx, artworkFillTimeout)
	defer cancel()

	top := results[:limit]
	filler := a.withPrefetchedIdentities(fillCtx, top)
	filled, live := filler.fillFromCache(ctx, fillCtx, top)
	filler.resolveLater(ctx, bg, filled, live)
	return append(filled, results[limit:]...)
}

func (a *ArtworkFiller) fillFromCache(ctx, fillCtx context.Context, top []domain.SearchResult) ([]domain.SearchResult, []artworkOutcome) {
	filled := slices.Clone(top)
	live := make([]artworkOutcome, len(top))
	forEachBounded(len(top), func(i int) {
		defer RecoverGoroutine(ctx, "artwork_fill.panic", "title", top[i].Title)
		outcome, settled := a.runCachedStages(fillCtx, &filled[i])
		if !settled {
			live[i] = outcome
			outcome.stage = artworkStagePending
		}
		setArtworkPath(&filled[i], outcome.path())
	})
	return filled, live
}

func (a *ArtworkFiller) resolveLater(ctx context.Context, bg *backgroundRunner, filled []domain.SearchResult, live []artworkOutcome) {
	jobs := liveArtworkJobs(live)
	if a.cache == nil || bg == nil || len(jobs) == 0 {
		return
	}
	results := slices.Clone(filled)
	bg.launch(ctx, "artwork_fill", func(bgCtx context.Context) {
		bgCtx, cancel := context.WithTimeout(bgCtx, artworkFillTimeout)
		defer cancel()
		a.resolveJobs(bgCtx, results, live, jobs)
	})
}

func liveArtworkJobs(live []artworkOutcome) []int {
	var jobs []int
	for i, outcome := range live {
		if outcome.stage == artworkStageLive {
			jobs = append(jobs, i)
		}
	}
	return jobs
}

func (a *ArtworkFiller) resolveJobs(ctx context.Context, results []domain.SearchResult, live []artworkOutcome, jobs []int) {
	forEachBounded(len(jobs), func(n int) {
		i := jobs[n]
		defer RecoverGoroutine(ctx, "artwork_fill.panic", "title", results[i].Title)
		a.resolveLive(ctx, &results[i], live[i].mbid, live[i].fromDurable)
	})
}

func forEachBounded(n int, work func(i int)) {
	var wg sync.WaitGroup
	slots := make(chan struct{}, artworkFillConcurrency)
	for i := range n {
		slots <- struct{}{}
		wg.Add(1)
		go func() {
			defer func() { <-slots; wg.Done() }()
			work(i)
		}()
	}
	wg.Wait()
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
	artworkStagePending
)

type artworkOutcome struct {
	stage       artworkStage
	resolved    string
	confidence  ports.ArtworkConfidence
	fromDurable bool
	mbid        string
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
	case artworkStagePending:
		return "pending"
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
	outcome, settled := a.runCachedStages(ctx, result)
	if settled {
		return outcome
	}
	return a.resolveLive(ctx, result, outcome.mbid, outcome.fromDurable)
}

func (a *ArtworkFiller) runCachedStages(ctx context.Context, result *domain.SearchResult) (artworkOutcome, bool) {
	if usableArtwork(result.ImageURL) {
		defaultArtworkSource(result)
		return artworkOutcome{stage: artworkStageProvider}, true
	}
	mbid, fromDurable := a.lookupMBID(ctx, result)
	if stage, settled := a.lookupArtworkCache(ctx, result, mbid); settled {
		return artworkOutcome{stage: stage}, true
	}
	return artworkOutcome{stage: artworkStageLive, mbid: mbid, fromDurable: fromDurable}, false
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
