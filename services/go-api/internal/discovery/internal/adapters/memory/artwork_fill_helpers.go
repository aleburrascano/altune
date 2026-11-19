package memory

import (
	"altune/go-api/internal/discovery/domain"
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
