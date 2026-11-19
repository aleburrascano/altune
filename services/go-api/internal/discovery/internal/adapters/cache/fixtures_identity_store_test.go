package cache

import (
	"altune/go-api/internal/discovery/domain"
	"context"
)

type recordingIdentityStore struct {
	mbid  string
	xref  map[domain.ProviderKey]string
	found bool

	lookupCalls int
}

func (f *recordingIdentityStore) LookupByProviderID(context.Context, domain.ResultKind, domain.ProviderKey, string) (string, map[domain.ProviderKey]string, bool) {
	f.lookupCalls++
	return f.mbid, f.xref, f.found
}
