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

func (c *verifyMemo) forget(mbid string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.m, mbid)
}
