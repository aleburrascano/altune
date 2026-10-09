package cache

import (
	"altune/go-api/internal/discovery/domain"
	"context"
)

type recordingIdentityStore struct {
	mbid  string
	xref  map[domain.ProviderKey]string
	found bool

	persistCalls    int
	lookupCalls     int
	invalidateCalls int
	invalidateErr   error
}

func (f *recordingIdentityStore) PersistBridges(context.Context, domain.ResultKind, string, map[domain.ProviderKey]string) error {
	f.persistCalls++
	return nil
}

func (f *recordingIdentityStore) LookupByProviderID(context.Context, domain.ResultKind, domain.ProviderKey, string) (string, map[domain.ProviderKey]string, bool) {
	f.lookupCalls++
	return f.mbid, f.xref, f.found
}

func (f *recordingIdentityStore) Invalidate(context.Context, domain.ResultKind, domain.ProviderKey, string) error {
	f.invalidateCalls++
	return f.invalidateErr
}
