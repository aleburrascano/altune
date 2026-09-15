package service

import (
	"cmp"
	"context"
	"log/slog"
	"strings"
	"time"

	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/discovery/ports"
	"altune/go-api/internal/shared/textnorm"

	"golang.org/x/sync/errgroup"
)

// enrichmentNameKey builds the name key used to read and write the shared MBID
// index. It must stay byte-for-byte identical to the MusicBrainz enricher's key
// (service/enrich) so RememberMBID writes and LookupMBID reads line up.
func enrichmentNameKey(title, subtitle string) string {
	return textnorm.NormalizeForMatch(strings.TrimSpace(title) + " " + strings.TrimSpace(subtitle))
}

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
	identityStore ports.IdentityStore
	mbidIndex     ports.MBIDIndex
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

	var g errgroup.Group
	g.SetLimit(artworkFillConcurrency)
	filled := make([]domain.SearchResult, len(top))

	for i, r := range top {
		filled[i] = r
		g.Go(func() error {
			defer RecoverGoroutine(ctx, "artwork_fill.panic", "title", r.Title)
			filled[i] = a.fillOne(fillCtx, r)
			return nil
		})
	}
	_ = g.Wait()
	return append(filled, rest...)
}

// artworkStage names the cascade stage that settled a result's artwork.
type artworkStage int

const (
	artworkStageProvider   artworkStage = iota // the provider already supplied usable art
	artworkStageCacheHit                       // the artwork cache held a usable URL
	artworkStageCachedMiss                     // the cache recorded a miss for a non-artist kind
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
	if len(result.Xref) > 0 || a.identityStore == nil || len(result.Sources) == 0 {
		return "", false
	}
	src := result.Sources[0]
	mbid, xref, ok := a.identityStore.LookupByProviderID(ctx, result.Kind, src.Provider.String(), src.ExternalID)
	if !ok {
		return "", false
	}
	if len(xref) > 0 {
		result.Xref = xref
	}
	slog.DebugContext(ctx, "identity.durable_resolved",
		"kind", result.Kind.String(), "provider", src.Provider.String(),
		"external_id", src.ExternalID, "mbid", mbid, "bridged_ids", len(xref))
	return mbid, true
}

func (a *ArtworkFiller) lookupMBIDIndex(ctx context.Context, result domain.SearchResult) string {
	if a.mbidIndex == nil {
		return ""
	}
	mbid, ok := a.mbidIndex.LookupMBID(ctx, result.Kind, enrichmentNameKey(result.Title, result.Subtitle))
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
		result.ArtworkSource = cachedSource
		return artworkStageCacheHit, true
	}
	if result.Kind != domain.ResultKindArtist {
		return artworkStageCachedMiss, true
	}
	return artworkStageLive, false
}

// resolveLive runs the live resolver, records its answer (hit or miss) in the
// artwork cache, and applies a hit to the result.
func (a *ArtworkFiller) resolveLive(ctx context.Context, result *domain.SearchResult, mbid string, fromDurable bool) artworkOutcome {
	resolved, source, confidence := a.resolve(ctx, *result, mbid)
	if a.cache != nil {
		_ = a.cache.Set(ctx, result.Kind, result.Title, result.Subtitle, mbid, resolved, source, confidence)
	}
	if resolved != "" {
		result.ImageURL = resolved
		result.ArtworkSource = source
	}
	slog.DebugContext(ctx, "artwork.enriched",
		"kind", result.Kind.String(), "source", source,
		"resolved", resolved != "", "had_mbid", mbid != "")
	return artworkOutcome{stage: artworkStageLive, resolved: resolved, confidence: confidence, fromDurable: fromDurable}
}

func setArtworkPath(r *domain.SearchResult, path string) {
	r.PutExtra("artwork_path", path)
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

func (a *ArtworkFiller) resolve(ctx context.Context, result domain.SearchResult, mbid string) (string, string, ports.ArtworkConfidence) {
	identity := artworkIdentity(result, mbid)

	if identity.HasLinks() {
		if url, src, _ := a.resolver.ResolveWithIdentityTagged(ctx, result.Kind, result.Title, result.Subtitle, identity); url != "" {
			return url, src, ports.ArtworkConfidenceIdentity
		}
	}
	if url, src, _ := a.resolver.ResolveTagged(ctx, result.Kind, result.Title, result.Subtitle, mbid); url != "" {
		return url, src, ports.ArtworkConfidenceName
	}
	return "", "", ports.ArtworkConfidenceNone
}

func artworkIdentity(result domain.SearchResult, mbid string) ports.ArtworkIdentity {
	id := ports.ArtworkIdentity{MBID: mbid}
	if len(result.Xref) > 0 {
		id.ExternalIDs = result.Xref
	}
	return id
}
