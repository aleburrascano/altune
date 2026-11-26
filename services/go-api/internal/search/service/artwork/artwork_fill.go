package artwork

import (
	"altune/go-api/internal/search/domain"
	"altune/go-api/internal/search/internal/core/ports"
	"altune/go-api/internal/shared/background"
	"altune/go-api/internal/shared/parallel"
	"altune/go-api/internal/shared/redact"
	"altune/go-api/internal/shared/textnorm"
	"cmp"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"slices"
	"strings"
	"time"
)

const (
	artworkFillLimit       = 50
	artworkFillConcurrency = 8
	artworkFillTimeout     = 4 * time.Second
	emptyArtHash           = "d41d8cd98f00b204e9800998ecf8427e"
)

var ErrCollaboratorRequired = errors.New("artwork filler needs every collaborator, pass the absent adapter to switch one off")

type ArtworkFiller struct {
	resolver      ports.TaggingArtworkLookup
	cache         ports.ArtworkCache
	identityStore identityLookup
	mbidIndex     ports.MBIDIndex
	bg            *background.Tracker
	live          liveStage
}

type liveStage interface {
	launch(ctx context.Context, a *ArtworkFiller, filled []domain.SearchResult, live []liveArtworkJob, jobs []int)
}

type backgroundLive struct{}

func (backgroundLive) launch(ctx context.Context, a *ArtworkFiller, filled []domain.SearchResult, live []liveArtworkJob, jobs []int) {
	results := slices.Clone(filled)
	a.bg.LaunchWithin(ctx, "artwork_fill", artworkFillTimeout, func(bgCtx context.Context) {
		a.resolveJobs(bgCtx, results, live, jobs)
	})
}

type noLive struct{}

func (noLive) launch(context.Context, *ArtworkFiller, []domain.SearchResult, []liveArtworkJob, []int) {
}

type NoFill struct{}

func (NoFill) Fill(_ context.Context, results []domain.SearchResult) []domain.SearchResult {
	return results
}

type identityLookup interface {
	LookupByProviderID(ctx context.Context, kind domain.ResultKind, provider domain.ProviderKey, externalID string) (mbid string, xref map[domain.ProviderKey]string, ok bool)
}

func NewArtworkFiller(
	resolver ports.TaggingArtworkLookup,
	cache ports.ArtworkCache,
	identityStore ports.IdentityStore,
	mbidIndex ports.MBIDIndex,
	bg *background.Tracker,
) (*ArtworkFiller, error) {
	return newFiller(resolver, cache, identityStore, mbidIndex, bg, backgroundLive{})
}

func NewPendingFiller(identityStore ports.IdentityStore, bg *background.Tracker) (*ArtworkFiller, error) {
	return newFiller(NoLookup{}, NoCache{}, identityStore, NoMBIDIndex{}, bg, noLive{})
}

func newFiller(
	resolver ports.TaggingArtworkLookup,
	cache ports.ArtworkCache,
	identityStore ports.IdentityStore,
	mbidIndex ports.MBIDIndex,
	bg *background.Tracker,
	live liveStage,
) (*ArtworkFiller, error) {
	for name, missing := range map[string]bool{
		"resolver":      resolver == nil,
		"cache":         cache == nil,
		"identityStore": identityStore == nil,
		"mbidIndex":     mbidIndex == nil,
		"tracker":       bg == nil,
	} {
		if missing {
			return nil, fmt.Errorf("%w: %s", ErrCollaboratorRequired, name)
		}
	}
	return &ArtworkFiller{resolver: resolver, cache: cache, identityStore: identityStore, mbidIndex: mbidIndex, bg: bg, live: live}, nil
}

func (a *ArtworkFiller) Fill(ctx context.Context, results []domain.SearchResult) []domain.SearchResult {
	limit := min(artworkFillLimit, len(results))
	if limit == 0 {
		return results
	}

	fillCtx, cancel := context.WithTimeout(ctx, artworkFillTimeout)
	defer cancel()

	top := results[:limit]
	filler := a.withPrefetchedIdentities(fillCtx, top)
	filled, live := filler.fillFromCache(ctx, fillCtx, top)
	filler.resolveLater(ctx, filled, live)
	return append(filled, results[limit:]...)
}

func (a *ArtworkFiller) fillFromCache(ctx, fillCtx context.Context, top []domain.SearchResult) ([]domain.SearchResult, []liveArtworkJob) {
	filled := slices.Clone(top)
	live := make([]liveArtworkJob, len(top))
	forEachBounded(ctx, len(top), func(i int) string { return top[i].Title }, func(i int) {
		path, job := a.runCachedStages(fillCtx, &filled[i])
		live[i] = job
		setArtworkPath(&filled[i], path)
	})
	return filled, live
}

func (a *ArtworkFiller) resolveLater(ctx context.Context, filled []domain.SearchResult, live []liveArtworkJob) {
	jobs := liveArtworkJobs(live)
	if len(jobs) == 0 {
		return
	}
	a.live.launch(ctx, a, filled, live, jobs)
}

func liveArtworkJobs(live []liveArtworkJob) []int {
	var jobs []int
	for i, job := range live {
		if job.pending {
			jobs = append(jobs, i)
		}
	}
	return jobs
}

func (a *ArtworkFiller) resolveJobs(ctx context.Context, results []domain.SearchResult, live []liveArtworkJob, jobs []int) {
	forEachBounded(ctx, len(jobs), func(n int) string { return results[jobs[n]].Title }, func(n int) {
		i := jobs[n]
		a.resolveLive(ctx, results[i], live[i].mbid)
	})
}

func forEachBounded(ctx context.Context, n int, titleOf func(i int) string, work func(i int)) {
	g := parallel.New("artwork_fill.panic", artworkFillConcurrency)
	for i := range n {
		g.Go(ctx, func() { work(i) }, "title", titleOf(i))
	}
	_ = g.Wait()
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

type liveArtworkJob struct {
	pending bool
	mbid    string
}

func (a *ArtworkFiller) runCachedStages(ctx context.Context, result *domain.SearchResult) (domain.ArtworkPath, liveArtworkJob) {
	if usableArtwork(result.ImageURL) {
		defaultArtworkSource(result)
		return domain.ArtworkPathProvider, liveArtworkJob{}
	}
	mbid := a.lookupMBID(ctx, result)
	if path, settled := a.lookupArtworkCache(ctx, result, mbid); settled {
		return path, liveArtworkJob{}
	}
	return domain.ArtworkPathPending, liveArtworkJob{pending: true, mbid: mbid}
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

func (a *ArtworkFiller) lookupMBID(ctx context.Context, result *domain.SearchResult) string {
	if mbid := cmp.Or(result.EntityMBID(), a.lookupDurableIdentity(ctx, result)); mbid != "" {
		return mbid
	}
	return a.lookupMBIDIndex(ctx, *result)
}

func (a *ArtworkFiller) lookupDurableIdentity(ctx context.Context, result *domain.SearchResult) string {
	if !needsDurableIdentity(*result) {
		return ""
	}
	ref := durableIdentityRef(*result)
	mbid, xref, ok := a.identityStore.LookupByProviderID(ctx, ref.Kind, ref.Provider, ref.ExternalID)
	if !ok {
		return ""
	}
	if len(xref) > 0 {
		result.Xref = xref
	}
	slog.DebugContext(ctx, "identity.durable_resolved",
		"kind", result.Kind.String(), "provider", ref.Provider.String(),
		"external_id", ref.ExternalID, "mbid", mbid, "bridged_ids", len(xref))
	return mbid
}

func (a *ArtworkFiller) lookupMBIDIndex(ctx context.Context, result domain.SearchResult) string {
	mbid, ok := a.mbidIndex.LookupMBID(ctx, result.Kind, textnorm.NameKey(result.Title, result.Subtitle))
	if !ok {
		return ""
	}
	return mbid
}

func (a *ArtworkFiller) lookupArtworkCache(ctx context.Context, result *domain.SearchResult, mbid string) (path domain.ArtworkPath, settled bool) {
	cachedURL, cachedSource, found, _ := a.cache.Get(ctx, result.Kind, result.Title, result.Subtitle, mbid)
	if !found {
		return "", false
	}
	if usableArtwork(cachedURL) {
		result.ImageURL = cachedURL
		result.ArtworkSource = cachedSource
		return domain.ArtworkPathCache, true
	}
	if result.Kind != domain.ResultKindArtist {
		return domain.ArtworkPathNone, true
	}
	return "", false
}

func (a *ArtworkFiller) resolveLive(ctx context.Context, result domain.SearchResult, mbid string) {
	resolved, source, confidence, err := a.resolve(ctx, result, mbid)
	if ports.IsUnverifiedArtworkMiss(resolved, err) {
		slog.WarnContext(ctx, "artwork.not_cached_degraded",
			"kind", result.Kind.String(), "had_mbid", mbid != "", "error", err)
		return
	}
	if err := a.cache.Set(ctx, result.Kind, result.Title, result.Subtitle, mbid, resolved, source, confidence); err != nil {
		slog.WarnContext(ctx, "artwork.cache_set_failed", "kind", result.Kind.String(), "error", redact.Secrets(err.Error()))
	}
	slog.DebugContext(ctx, "artwork.enriched",
		"kind", result.Kind.String(), "source", source.String(),
		"resolved", resolved != "", "had_mbid", mbid != "")
}

func setArtworkPath(r *domain.SearchResult, path domain.ArtworkPath) {
	r.ArtworkPath = path
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
