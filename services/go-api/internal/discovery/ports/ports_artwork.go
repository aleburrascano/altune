package ports

import (
	"context"

	"altune/go-api/internal/discovery/domain"
)

type ArtworkResolver interface {
	Resolve(ctx context.Context, kind domain.ResultKind, title, subtitle string, mbid string) (string, error)
}

type SourcedArtworkResolver interface {
	ArtworkSource() string
}

type TaggingArtworkResolver interface {
	ResolveTagged(ctx context.Context, kind domain.ResultKind, title, subtitle, mbid string) (url, source string, err error)
	ResolveWithIdentityTagged(ctx context.Context, kind domain.ResultKind, title, subtitle string, id ArtworkIdentity) (url, source string, err error)
}

type ArtworkIdentity struct {
	MBID        string
	ExternalIDs map[string]string
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
	Get(ctx context.Context, kind domain.ResultKind, title, subtitle, mbid string) (url, source string, found bool, err error)
	Set(ctx context.Context, kind domain.ResultKind, title, subtitle, mbid, url, source string, confidence ArtworkConfidence) error
}

type MBIDIndex interface {
	LookupMBID(ctx context.Context, kind domain.ResultKind, nameKey string) (string, bool)
	RememberMBID(ctx context.Context, kind domain.ResultKind, nameKey, mbid string) error
}

type IdentityStore interface {
	PersistBridges(ctx context.Context, kind domain.ResultKind, mbid string, xref map[string]string) error
	LookupByProviderID(ctx context.Context, kind domain.ResultKind, provider, externalID string) (mbid string, xref map[string]string, ok bool)
	Invalidate(ctx context.Context, kind domain.ResultKind, provider, externalID string) error
}
