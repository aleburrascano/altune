package ports

import (
	"altune/go-api/internal/discovery/domain"
	"context"
	"errors"
)

var ErrArtworkDegraded = errors.New("artwork resolution degraded")

var ErrArtworkUnavailable = errors.New("artwork provider unavailable")

func IsUnverifiedArtworkMiss(url string, err error) bool {
	return url == "" && err != nil
}

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
	ExternalIDs map[domain.ProviderKey]string
}

func (id ArtworkIdentity) ExternalID(key domain.ProviderKey) string {
	return id.ExternalIDs[key]
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
