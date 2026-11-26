package ports

import (
	"altune/go-api/internal/search/internal/domain"
	"context"
	"errors"
)

var ErrIdentityStoreUnavailable = errors.New("identity store unavailable")

type MBIDIndex interface {
	LookupMBID(ctx context.Context, kind domain.ResultKind, nameKey string) (string, bool)
	RememberMBID(ctx context.Context, kind domain.ResultKind, nameKey, mbid string) error
}

type IdentityStore interface {
	LookupByProviderID(ctx context.Context, kind domain.ResultKind, provider domain.ProviderKey, externalID string) (mbid string, xref map[domain.ProviderKey]string, ok bool)
}

type IdentityRef struct {
	Kind       domain.ResultKind
	Provider   domain.ProviderKey
	ExternalID string
}

type IdentityHit struct {
	MBID string
	Xref map[domain.ProviderKey]string
}

type BatchIdentityLookup interface {
	LookupByProviderIDs(ctx context.Context, refs []IdentityRef) map[IdentityRef]IdentityHit
}
