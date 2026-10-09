package discoverytest

import (
	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/discovery/ports"
	"context"
	"sync/atomic"
)

type ArtworkResolver struct {
	URL string
	Err error

	calls int32
}

var (
	_ ports.ArtworkResolver        = (*ArtworkResolver)(nil)
	_ ports.TaggingArtworkResolver = (*ArtworkResolver)(nil)
)

func (r *ArtworkResolver) Calls() int {
	return int(atomic.LoadInt32(&r.calls))
}

func (r *ArtworkResolver) Resolve(_ context.Context, _ domain.ResultKind, _, _, _ string) (string, error) {
	atomic.AddInt32(&r.calls, 1)
	return r.URL, r.Err
}

func (r *ArtworkResolver) ResolveTagged(_ context.Context, _ domain.ResultKind, _, _, _ string) (string, domain.ProviderKey, error) {
	atomic.AddInt32(&r.calls, 1)
	return r.URL, "", r.Err
}

func (r *ArtworkResolver) ResolveWithIdentityTagged(_ context.Context, _ domain.ResultKind, _, _ string, _ ports.ArtworkIdentity) (string, domain.ProviderKey, error) {
	return "", "", nil
}
