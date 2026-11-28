package memory

import (
	"altune/go-api/internal/search/internal/domain"
	"altune/go-api/internal/search/internal/ports"
	"context"
	"maps"
	"sync"
)

var _ ports.IdentityStore = (*IdentityRepo)(nil)

type IdentityRepo struct {
	mu   sync.Mutex
	rows map[ports.IdentityRef]ports.IdentityHit
}

func (r *IdentityRepo) Seed(kind domain.ResultKind, mbid string, xref map[domain.ProviderKey]string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.rows == nil {
		r.rows = map[ports.IdentityRef]ports.IdentityHit{}
	}
	for provider, externalID := range xref {
		r.rows[ports.IdentityRef{Kind: kind, Provider: provider, ExternalID: externalID}] = ports.IdentityHit{MBID: mbid, Xref: maps.Clone(xref)}
	}
}

func (r *IdentityRepo) LookupByProviderID(_ context.Context, kind domain.ResultKind, provider domain.ProviderKey, externalID string) (string, map[domain.ProviderKey]string, bool) {
	if provider == "" || externalID == "" {
		return "", nil, false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	hit, ok := r.rows[ports.IdentityRef{Kind: kind, Provider: provider, ExternalID: externalID}]
	if !ok || hit.MBID == "" {
		return "", nil, false
	}
	return hit.MBID, maps.Clone(hit.Xref), true
}

func (r *IdentityRepo) LookupByProviderIDs(ctx context.Context, refs []ports.IdentityRef) map[ports.IdentityRef]ports.IdentityHit {
	hits := map[ports.IdentityRef]ports.IdentityHit{}
	for _, ref := range refs {
		if mbid, xref, ok := r.LookupByProviderID(ctx, ref.Kind, ref.Provider, ref.ExternalID); ok {
			hits[ref] = ports.IdentityHit{MBID: mbid, Xref: xref}
		}
	}
	return hits
}
