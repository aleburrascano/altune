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

// ArtworkFiller is the artwork-resolution collaborator: it fills missing cover
// art on the top of a ranked slate from the artwork cache, the durable identity
// store, the shared MBID index, and the tagging artwork resolver. It replaces
// the four artwork/identity-lookup ports that used to sit on the Service god
// object, so artwork sourcing can change without touching the orchestrator.
// A nil resolver disables the fill and leaves results untouched.
type ArtworkFiller struct {
	resolver      ports.TaggingArtworkResolver
	cache         ports.ArtworkCache
	identityStore identityLookup
	mbidIndex     ports.MBIDIndex
}

// identityLookup is the single-ref durable identity read the cascade needs; a
// ports.IdentityStore satisfies it, and so does a batch-prefetched snapshot.
type identityLookup interface {
	LookupByProviderID(ctx context.Context, kind domain.ResultKind, provider domain.ProviderKey, externalID string) (mbid string, xref map[string]string, ok bool)
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

// withPrefetchedIdentities resolves every durable identity the slate needs in
// one batched store call and returns a filler whose cascade reads that
// snapshot, instead of issuing one store round-trip per result. A store
// without the batch capability keeps the per-result lookup.
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

// durableIdentityRefs lists the refs the cascade would look up: results with no
// usable provider art that still need the durable store.
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

// prefetchedIdentities answers single-ref lookups from a batch result; a ref
// the batch did not return is a miss. It is read-only once built, so the
// concurrent fill goroutines can share it; each hit's xref is cloned so results
// that share a ref never alias one map.
type prefetchedIdentities map[ports.IdentityRef]ports.IdentityHit

func (p prefetchedIdentities) LookupByProviderID(_ context.Context, kind domain.ResultKind, provider domain.ProviderKey, externalID string) (string, map[string]string, bool) {
	hit, ok := p[ports.IdentityRef{Kind: kind, Provider: provider, ExternalID: externalID}]
	return hit.MBID, maps.Clone(hit.Xref), ok
}

// artworkStage names the cascade stage that settled a result's artwork.
type artworkStage int

const (
	artworkStageProvider   artworkStage = iota // the provider already supplied usable art
	artworkStageCacheHit                       // the artwork cache held a usable URL
	artworkStageCachedMiss                     // the cache recorded a miss for a non-artist kind
	artworkStageDegraded                       // the live resolver failed, so its miss proves nothing
	artworkStageLive                           // the live resolver ran (hit or miss)
)

// artworkOutcome is what the cascade learned; the artwork_path label is derived
// from it once, after the cascade, rather than stamped at every exit.
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

// runStages tries each artwork stage in order, returning at the first that
// settles the result: provider art, then the artwork cache keyed on the MBID
// (from the durable identity store or the shared MBID index), then the live
// resolver.
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
	result.ArtworkSource = result.Sources[0].Provider.String()
}

// lookupMBID resolves the MBID to key artwork on: the result's own MBID, else
// the durable identity store's, else the shared MBID index's. fromDurable
// reports whether the durable store knew the result, even when the result's own
// MBID wins.
func (a *ArtworkFiller) lookupMBID(ctx context.Context, result *domain.SearchResult) (mbid string, fromDurable bool) {
	durableMBID, fromDurable := a.lookupDurableIdentity(ctx, result)
	mbid = cmp.Or(result.MBID, durableMBID)
	if mbid == "" {
		mbid = a.lookupMBIDIndex(ctx, *result)
	}
	return mbid, fromDurable
}

// lookupDurableIdentity consults the durable identity store for a result that
// has no bridged ids yet, stamping the stored xref onto it. ok reports a hit.
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

// lookupArtworkCache consults the artwork cache and reports whether the cached
// entry settled the result (a usable URL, or a cached miss for a non-artist
// kind), in which case the live resolver must not run.
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
		result.ArtworkSource = cachedSource.String()
		return artworkStageCacheHit, true
	}
	if result.Kind != domain.ResultKindArtist {
		return artworkStageCachedMiss, true
	}
	return artworkStageLive, false
}

// resolveLive runs the live resolver, records its answer (hit or miss) in the
// artwork cache, and applies a hit to the result. A miss the resolvers could
// not vouch for is never written: a negative entry would blank this art for the
// whole negative TTL over a provider blip that lasted seconds.
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
	result.ArtworkSource = source.String()
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

// resolve walks the id-pinned resolver then the name-keyed one. A miss carries
// every failure both legs reported, so the caller can tell "no art exists" from
// "we could not look"; a hit carries none.
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

// resolveByIdentity runs the id-pinned resolver, reporting a clean empty when
// the result carries no durable id to pin on.
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
