package ports

import (
	"altune/go-api/internal/discovery/domain"
	"context"
	"errors"
)

// ErrArtworkDegraded marks an empty artwork answer that means "we could not
// look", not "no art exists": a provider failed or the deadline cut the walk
// short. Recording such an answer as a negative cache entry blanks the art for
// the whole negative TTL over an outage that lasted seconds, so callers must
// branch on it before they write.
//
// The verdict is only as honest as the ArtworkResolver implementations behind
// it. Most provider adapters still map their own transport and 5xx failures to
// ("", nil) under a //nolint:nilerr "best-effort" comment, so their outages are
// reported here as verified misses and are still negative-cached. Until those
// adapters propagate, this catches the deadline-cut walk and the resolvers that
// do return an error. See issue #2253.
var ErrArtworkDegraded = errors.New("artwork resolution degraded")

// IsUnverifiedArtworkMiss reports whether an empty artwork answer came from a
// failure rather than from a complete look. It is the one test that guards a
// negative-cache write, so the rule cannot drift between the callers that make
// one. Any error counts, not only ErrArtworkDegraded: a resolver that does not
// classify its failures still has not verified the miss.
func IsUnverifiedArtworkMiss(url string, err error) bool {
	return url == "" && err != nil
}

type ArtworkResolver interface {
	Resolve(ctx context.Context, kind domain.ResultKind, title, subtitle string, mbid string) (string, error)
}

type SourcedArtworkResolver interface {
	ArtworkSource() domain.ProviderKey
}

// TaggingArtworkResolver resolves cover art and names the provider it came
// from. An empty url with a nil err is a verified miss: every provider was
// asked and none had art. An empty url with a non-nil err is unverified, see
// ErrArtworkDegraded. A hit always carries a nil err.
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
