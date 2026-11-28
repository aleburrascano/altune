package memory

import (
	"altune/go-api/internal/search/internal/domain"
	"altune/go-api/internal/search/internal/ports"
	"context"
	"sync/atomic"
)

type ArtworkLookup struct {
	URL string
	Err error

	calls int32
}

var (
	_ ports.ArtworkLookup        = (*ArtworkLookup)(nil)
	_ ports.TaggingArtworkLookup = (*ArtworkLookup)(nil)
)

func (r *ArtworkLookup) Calls() int {
	return int(atomic.LoadInt32(&r.calls))
}

func (r *ArtworkLookup) Resolve(_ context.Context, _ domain.ResultKind, _, _, _ string) (string, error) {
	atomic.AddInt32(&r.calls, 1)
	return r.URL, r.Err
}

func (r *ArtworkLookup) ArtworkSource() domain.ProviderKey { return "" }

func (r *ArtworkLookup) ResolveTagged(_ context.Context, _ domain.ResultKind, _, _, _ string) (string, domain.ProviderKey, error) {
	atomic.AddInt32(&r.calls, 1)
	return r.URL, "", r.Err
}

func (r *ArtworkLookup) ResolveWithIdentityTagged(_ context.Context, _ domain.ResultKind, _, _ string, _ ports.ArtworkIdentity) (string, domain.ProviderKey, error) {
	return "", "", nil
}
