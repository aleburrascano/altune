package memory

import (
	"altune/go-api/internal/search/internal/core/ports"
	"altune/go-api/internal/search/internal/domain"
	"context"
	"sync/atomic"
)

type FakeIdentityStore struct {
	Mbid    string
	Xref    map[domain.ProviderKey]string
	Lookups int32
}

func (f *FakeIdentityStore) LookupByProviderID(_ context.Context, _ domain.ResultKind, _ domain.ProviderKey, _ string) (string, map[domain.ProviderKey]string, bool) {
	atomic.AddInt32(&f.Lookups, 1)
	if f.Mbid == "" {
		return "", nil, false
	}
	return f.Mbid, f.Xref, true
}

func (f *FakeIdentityStore) LookupByProviderIDs(ctx context.Context, refs []ports.IdentityRef) map[ports.IdentityRef]ports.IdentityHit {
	hits := map[ports.IdentityRef]ports.IdentityHit{}
	for _, ref := range refs {
		if mbid, xref, ok := f.LookupByProviderID(ctx, ref.Kind, ref.Provider, ref.ExternalID); ok {
			hits[ref] = ports.IdentityHit{MBID: mbid, Xref: xref}
		}
	}
	return hits
}
