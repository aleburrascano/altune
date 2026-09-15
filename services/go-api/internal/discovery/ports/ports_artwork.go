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
