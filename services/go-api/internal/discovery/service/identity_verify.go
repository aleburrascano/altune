package service

import (
	"context"
	"log/slog"
	"maps"
	"sync"
	"time"

	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/discovery/ports"
)

// IdentityVerifier verifies a learned cross-provider identity bridge before it is
// persisted — the permanent identity-bridge fix (docs/discovery-detail-pipeline.md
// §7). MusicBrainz's url-relations are not always correct: a wrong streaming link
// fuses two same-name artists (a wrong Deezer "Che"). Before the bridge is stored,
// each streaming-provider edge is checked — if that provider id's catalogue does
// not overlap the artist's MusicBrainz release-groups, the edge is a mis-bridge
// and is dropped, so the durable identity (and the detail fan-out / artwork that
// read it) never inherit the contamination. This is the same overlap test the
// detail-time MB anchor runs, moved upstream to persist time and applied per edge;
// with it, the detail anchor becomes a belt-and-suspenders guard against new
// un-verified bridges rather than the primary fix.
//
// Fail-open everywhere: no anchor, no MBID, an MB error, too few release-groups to
// judge, or a provider fetch failure all keep the edge. Only a positively
// non-overlapping catalogue drops one.
type IdentityVerifier struct {
	anchor    ports.MBDiscographyAnchor
	providers map[domain.ProviderName]ports.ArtistContentProvider
	memo      *verifyMemo
}

func NewIdentityVerifier(
	anchor ports.MBDiscographyAnchor,
	providers map[domain.ProviderName]ports.ArtistContentProvider,
) *IdentityVerifier {
	return &IdentityVerifier{anchor: anchor, providers: providers, memo: newVerifyMemo(6 * time.Hour)}
}

// verifiableEdge maps an identity xref key to the content provider that fetches
// its catalogue. Apple Music shares the iTunes id space (the bridge emits the
// "itunes" key). Keys absent here are left untouched: discogs/wikidata are not
// catalogues, and soundcloud carries an MB-authoritative profile handle (not a
// numeric id) so it is trusted as-is.
func verifiableEdge(key string) (domain.ProviderName, bool) {
	switch key {
	case "deezer":
		return domain.ProviderDeezer, true
	case "spotify":
		return domain.ProviderSpotify, true
	case "itunes":
		return domain.ProviderAppleMusic, true
	}
	var zero domain.ProviderName
	return zero, false
}

// VerifyXref returns xref with each mis-bridged streaming edge removed. It fetches
// the artist's MusicBrainz release-groups once, then each verifiable provider's
// catalogue, dropping an edge whose titles don't overlap (groupMatchesAnchor, the
// same test the detail anchor uses). Memoized per MBID so repeated searches of the
// same artist don't re-fetch within the window.
func (v *IdentityVerifier) VerifyXref(ctx context.Context, kind domain.ResultKind, mbid string, xref map[string]string) map[string]string {
	if v == nil || v.anchor == nil || mbid == "" || kind != domain.ResultKindArtist || len(xref) == 0 {
		return xref
	}
	if v.memo.seen(mbid) {
		return xref
	}
	titles, err := v.anchor.ReleaseGroupTitles(ctx, mbid)
	if err != nil || len(titles) < mbAnchorMinReleaseGroups {
		return xref // fail-open: no / too few MB release-groups to judge against
	}
	mbSet := normalizeTitleSet(titles)

	out := maps.Clone(xref)
	for key, id := range xref {
		provider, ok := verifiableEdge(key)
		if !ok || id == "" {
			continue
		}
		p := v.providers[provider]
		if p == nil {
			continue
		}
		albums, err := p.GetArtistAlbums(ctx, provider, id)
		if err != nil || len(albums) == 0 {
			continue // fail-open: never drop an edge on a fetch failure / empty result
		}
		if !groupMatchesAnchor(ReleaseGroup{Releases: albums}, mbSet) {
			delete(out, key)
			slog.InfoContext(ctx, "identity.verify_dropped_edge",
				"mbid", mbid, "provider", provider.String(), "external_id", id)
		}
	}
	v.memo.mark(mbid)
	return out
}

// verifyMemo bounds re-verification cost: an MBID verified within the TTL is not
// re-fetched (the durable upsert already reflects the verified set). No eviction
// beyond TTL — a household's artist working set is small (mirrors the MB memo).
type verifyMemo struct {
	mu  sync.Mutex
	ttl time.Duration
	m   map[string]time.Time
}

func newVerifyMemo(ttl time.Duration) *verifyMemo {
	return &verifyMemo{ttl: ttl, m: make(map[string]time.Time)}
}

func (c *verifyMemo) seen(mbid string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	exp, ok := c.m[mbid]
	return ok && time.Now().Before(exp)
}

func (c *verifyMemo) mark(mbid string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.m[mbid] = time.Now().Add(c.ttl)
}
