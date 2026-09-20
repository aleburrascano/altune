package service

import (
	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/discovery/ports"
	"context"
	"log/slog"
	"maps"
	"sync"
	"time"
)

type IdentityVerifier struct {
	anchor    ports.MBDiscographyAnchor
	providers map[domain.ProviderName]ports.ArtistContentProvider
	memo      *verifyMemo
}

func NewIdentityVerifier(
	anchor ports.MBDiscographyAnchor,
	providers map[domain.ProviderName]ports.ArtistContentProvider,
) *IdentityVerifier {
	return &IdentityVerifier{anchor: anchor, providers: providers, memo: newVerifyMemo(6*time.Hour, time.Now)}
}

// verifiableEdge reports the content provider that can verify the xref edge
// stored under key. iTunes ids are served by the Apple Music content provider.
func verifiableEdge(key string) (domain.ProviderName, bool) {
	provider, ok := domain.ProviderKey(key).ProviderName()
	switch {
	case !ok:
		return domain.ProviderUnknown, false
	case provider == domain.ProviderDeezer, provider == domain.ProviderSpotify:
		return provider, true
	case provider == domain.ProviderITunes:
		return domain.ProviderAppleMusic, true
	}
	return domain.ProviderUnknown, false
}

func (v *IdentityVerifier) VerifyXref(ctx context.Context, kind domain.ResultKind, mbid string, xref map[string]string) (map[string]string, bool) {
	if v == nil || v.anchor == nil || mbid == "" || kind != domain.ResultKindArtist || len(xref) == 0 {
		return xref, true
	}
	if v.memo.seen(mbid) {
		return nil, false
	}
	titles, err := v.anchor.ReleaseGroupTitles(ctx, mbid)
	if err != nil || len(titles) < mbAnchorMinReleaseGroups {
		return xref, true
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
			continue
		}
		if !groupMatchesAnchor(ReleaseGroup{Releases: albums}, mbSet) {
			delete(out, key)
			slog.InfoContext(ctx, "identity.verify_dropped_edge",
				"mbid", mbid, "provider", provider.String(), "external_id", id)
		}
	}
	v.memo.mark(mbid)
	return out, true
}

func (v *IdentityVerifier) Forget(mbid string) {
	if v == nil {
		return
	}
	v.memo.forget(mbid)
}

// verifyMemo suppresses re-verifying an artist for ttl after its first pass.
// One key per distinct MBID, so the set is swept at most once per ttl and holds
// only the artists verified in the current window rather than every artist the
// process has ever seen.
type verifyMemo struct {
	mu        sync.Mutex
	ttl       time.Duration
	now       func() time.Time
	m         map[string]time.Time
	lastSweep time.Time
}

func newVerifyMemo(ttl time.Duration, now func() time.Time) *verifyMemo {
	return &verifyMemo{ttl: ttl, now: now, m: make(map[string]time.Time), lastSweep: now()}
}

func (c *verifyMemo) seen(mbid string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	exp, ok := c.m[mbid]
	return ok && c.now().Before(exp)
}

func (c *verifyMemo) mark(mbid string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	now := c.now()
	c.dropExpired(now)
	c.m[mbid] = now.Add(c.ttl)
}

// dropExpired runs at most once per ttl, so marking costs one full scan per ttl
// rather than one per call. The caller holds c.mu.
func (c *verifyMemo) dropExpired(now time.Time) {
	if now.Sub(c.lastSweep) < c.ttl {
		return
	}
	c.lastSweep = now
	maps.DeleteFunc(c.m, func(_ string, exp time.Time) bool { return !now.Before(exp) })
}

func (c *verifyMemo) forget(mbid string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.m, mbid)
}
