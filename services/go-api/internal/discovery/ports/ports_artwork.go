package ports

import (
	"context"

	"altune/go-api/internal/discovery/domain"
)

type ArtworkResolver interface {
	Resolve(ctx context.Context, kind domain.ResultKind, title, subtitle string, mbid string) (string, error)
}

type SourcedArtworkResolver interface {
	ArtworkSource() domain.ProviderKey
}

type TaggingArtworkResolver interface {
	ResolveTagged(ctx context.Context, kind domain.ResultKind, title, subtitle, mbid string) (url string, source domain.ProviderKey, err error)
	ResolveWithIdentityTagged(ctx context.Context, kind domain.ResultKind, title, subtitle string, id ArtworkIdentity) (url string, source domain.ProviderKey, err error)
}

type ArtworkIdentity struct {
	MBID        string
	ExternalIDs map[string]string
}

// ExternalID returns the bridged external ID stored under key, or "".
func (id ArtworkIdentity) ExternalID(key domain.ProviderKey) string {
	return id.ExternalIDs[key.String()]
}

func (id ArtworkIdentity) HasLinks() bool {
	return id.MBID != "" || len(id.ExternalIDs) > 0
}

type IdentityArtworkResolver interface {
	ResolveByIdentity(ctx context.Context, kind domain.ResultKind, id ArtworkIdentity) (string, error)
}

type ArtworkConfidence int

const (
	ArtworkConfidenceNone ArtworkConfidence = iota
	ArtworkConfidenceName
	ArtworkConfidenceIdentity
)

type ArtworkCache interface {
	Get(ctx context.Context, kind domain.ResultKind, title, subtitle, mbid string) (url string, source domain.ProviderKey, found bool, err error)
	Set(ctx context.Context, kind domain.ResultKind, title, subtitle, mbid, url string, source domain.ProviderKey, confidence ArtworkConfidence) error
}

type MBIDIndex interface {
	LookupMBID(ctx context.Context, kind domain.ResultKind, nameKey string) (string, bool)
	RememberMBID(ctx context.Context, kind domain.ResultKind, nameKey, mbid string) error
}

type IdentityStore interface {
	PersistBridges(ctx context.Context, kind domain.ResultKind, mbid string, xref map[string]string) error
	LookupByProviderID(ctx context.Context, kind domain.ResultKind, provider domain.ProviderKey, externalID string) (mbid string, xref map[string]string, ok bool)
	Invalidate(ctx context.Context, kind domain.ResultKind, provider domain.ProviderKey, externalID string) error
}

// IdentityRef names one durable identity: a provider's external id for a kind.
// It is comparable, so it keys the batch lookup's result map.
type IdentityRef struct {
	Kind       domain.ResultKind
	Provider   domain.ProviderKey
	ExternalID string
}

// IdentityHit is a durable identity the store knows: its MBID and bridged ids.
type IdentityHit struct {
	MBID string
	Xref map[string]string
}

// BatchIdentityLookup is an optional IdentityStore capability: resolve many
// refs in one round-trip. The returned map holds only hits; a ref that is
// absent (or a lookup that failed) is a miss, exactly as LookupByProviderID
// reports ok=false. Callers type-assert for it and fall back to per-ref
// LookupByProviderID when a store does not implement it.
type BatchIdentityLookup interface {
	LookupByProviderIDs(ctx context.Context, refs []IdentityRef) map[IdentityRef]IdentityHit
}
