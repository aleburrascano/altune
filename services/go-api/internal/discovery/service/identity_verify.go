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

func verifiableEdge(key domain.ProviderKey) (domain.ProviderName, bool) {
	provider, ok := key.ProviderName()
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

func (v *IdentityVerifier) VerifyXref(ctx context.Context, kind domain.ResultKind, mbid string, xref map[domain.ProviderKey]string) (map[domain.ProviderKey]string, bool) {
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
		if !v.edgeMatches(ctx, mbid, key, id, mbSet) {
			delete(out, key)
		}
	}
	v.memo.mark(mbid)
	return out, true
}

func (v *IdentityVerifier) edgeMatches(ctx context.Context, mbid string, key domain.ProviderKey, id string, mbSet map[string]bool) bool {
	provider, ok := verifiableEdge(key)
	if !ok || id == "" {
		return true
	}
	p := v.providers[provider]
	if p == nil {
		return true
	}
	albums, err := p.GetArtistAlbums(ctx, provider, id)
	if err != nil || len(albums) == 0 {
		return true
	}
	if groupMatchesAnchor(ReleaseGroup{Releases: albums}, mbSet) {
		return true
	}
	slog.InfoContext(ctx, "identity.verify_dropped_edge",
		"mbid", mbid, "provider", provider.String(), "external_id", id)
	return false
}

func (v *IdentityVerifier) Forget(mbid string) {
	if v == nil {
		return
	}
	v.memo.forget(mbid)
}

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
