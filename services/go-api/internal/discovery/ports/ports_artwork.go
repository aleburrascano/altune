package ports

import (
	"context"

	"altune/go-api/internal/discovery/domain"
)

type ArtworkResolver interface {
	Resolve(ctx context.Context, kind domain.ResultKind, title, subtitle string, mbid string) (string, error)
}

// SourcedArtworkResolver is the optional capability a resolver implements to
// name itself ("fanart", "discogs", …). The chain reads it to tag which provider
// supplied a result's artwork, for per-provider coverage visibility. A resolver
// that doesn't implement it is reported as an empty source.
type SourcedArtworkResolver interface {
	ArtworkSource() string
}

// TaggingArtworkResolver resolves artwork AND reports which source supplied it.
// The chain implements it; the enrichment use case type-asserts for it to record
// SearchResult.ArtworkSource, falling back to the plain (untagged) resolver when
// absent. Source is "" when nothing resolved.
type TaggingArtworkResolver interface {
	ResolveTagged(ctx context.Context, kind domain.ResultKind, title, subtitle, mbid string) (url, source string, err error)
	ResolveWithIdentityTagged(ctx context.Context, kind domain.ResultKind, title, subtitle string, id ArtworkIdentity) (url, source string, err error)
}

// ArtworkIdentity carries an entity's proven cross-provider identity: its MBID
// plus the bridged provider ids (discogs, spotify, deezer, …) the merge's
// identity bridge stamped from MusicBrainz url-relations. Resolvers that can
// fetch by id use it to return the exact entity instead of name-searching — the
// only way to get the right face for same-name artists.
type ArtworkIdentity struct {
	MBID        string
	ExternalIDs map[string]string
}

// HasLinks reports whether the identity carries any usable identifier.
func (id ArtworkIdentity) HasLinks() bool {
	return id.MBID != "" || len(id.ExternalIDs) > 0
}

// IdentityArtworkResolver is the optional capability a resolver implements when
// it can fetch artwork for a proven identity by a bridged provider id rather
// than by name. The chain tries these before any name-based resolver, and a
// resolver implementing it is treated as identity-only — it never name-searches.
type IdentityArtworkResolver interface {
	ResolveByIdentity(ctx context.Context, kind domain.ResultKind, id ArtworkIdentity) (string, error)
}

// IdentityAwareArtworkResolver is an ArtworkResolver that also resolves from a
// full proven identity. The chain implements it; callers type-assert for it and
// fall back to the name-only Resolve when an entity has no identity.
type IdentityAwareArtworkResolver interface {
	ArtworkResolver
	ResolveWithIdentity(ctx context.Context, kind domain.ResultKind, title, subtitle string, id ArtworkIdentity) (string, error)
}

type ArtworkCache interface {
	Get(ctx context.Context, kind domain.ResultKind, title, subtitle, mbid string) (url, source string, found bool, err error)
	Set(ctx context.Context, kind domain.ResultKind, title, subtitle, mbid, url, source string) error
}

type PopularityResolver interface {
	GetPopularity(ctx context.Context, title, artist string) (int64, error)
}

type MbidResolver interface {
	Resolve(ctx context.Context, url string) (string, error)
}

// MBIDIndex is a cache-only name→MBID memo. A detail-open's strict name
// resolution remembers (kind, nameKey) → mbid; the search path reads it to
// attach an MBID to a non-MB result so the MBID-keyed artwork tier (Cover Art
// Archive / Fanart.tv) fires on the search card too. Cache-only — never an MB
// call on the search path; a miss degrades to the provider's own thumbnail.
type MBIDIndex interface {
	LookupMBID(ctx context.Context, kind domain.ResultKind, nameKey string) (string, bool)
	RememberMBID(ctx context.Context, kind domain.ResultKind, nameKey, mbid string) error
}

// IdentityStore is the durable reverse identity map: (provider, external_id, kind)
// → the entity's MBID plus the bridged cross-provider ids (xref). It is the
// persisted, MB-independent counterpart to the in-flight identity bridge: when
// MusicBrainz answers a search, PersistBridges records what the merge learned; on
// a later search where MusicBrainz is absent (rate-limited / circuit-open), the
// enrichment read path resolves a provider-only result's identity from here, so
// artwork stays identity-first and correct instead of falling back to a name
// guess. Keyed on stable provider ids — never names — so same-name entities
// ("Che") cannot inherit each other's identity. Postgres is the source of truth;
// a Redis read-through fronts it. Implementations are nil-safe no-ops when unset.
type IdentityStore interface {
	// PersistBridges records, for each (provider, external_id) in xref, a row
	// pointing at mbid and carrying the full xref. Idempotent upsert.
	PersistBridges(ctx context.Context, kind domain.ResultKind, mbid string, xref map[string]string) error
	// LookupByProviderID returns the bridged identity for one provider id, or
	// ok=false when none was ever recorded.
	LookupByProviderID(ctx context.Context, kind domain.ResultKind, provider, externalID string) (mbid string, xref map[string]string, ok bool)
}
