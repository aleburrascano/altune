package ports

import (
	"context"

	"altune/go-api/internal/discovery/domain"
)

// ArtworkResolver is the chain-member port: one artwork source (CAA, Fanart,
// Deezer, iTunes, …) resolving by name/MBID. The chained resolver iterates
// these; the search service consumes the chain through TaggingArtworkResolver,
// never this interface directly.
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

// TaggingArtworkResolver is the service-side artwork port: resolve artwork AND
// report which source supplied it (recorded as SearchResult.ArtworkSource for
// per-provider coverage visibility). ResolveWithIdentityTagged resolves strictly
// by proven identity (bridged ids) and never name-searches; ResolveTagged is the
// name path. Source is "" when nothing resolved. Implemented by the chained
// resolver wired at the composition root.
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

// ArtworkConfidence grades how trustworthy a resolved artwork URL is, so the
// cache can treat a proven-identity image as authoritative (long TTL, not
// overwritable by a weaker result) and a name-searched one as provisional (short
// TTL, re-checked soon so it can upgrade to identity once that is learned).
// Higher is more authoritative.
type ArtworkConfidence int

const (
	ArtworkConfidenceNone     ArtworkConfidence = iota // nothing resolved
	ArtworkConfidenceName                              // resolved by name search — provisional
	ArtworkConfidenceIdentity                          // resolved via proven MBID/xref — authoritative
)

type ArtworkCache interface {
	Get(ctx context.Context, kind domain.ResultKind, title, subtitle, mbid string) (url, source string, found bool, err error)
	// Set stores a resolved (or negative, url == "") artwork entry.
	// Confidence-guard invariant: a write at LOWER confidence than an existing
	// POSITIVE entry (non-empty URL) must be a no-op — a name guess or a later
	// resolution failure never clobbers a proven-identity image. Equal or higher
	// confidence overwrites/refreshes normally.
	Set(ctx context.Context, kind domain.ResultKind, title, subtitle, mbid, url, source string, confidence ArtworkConfidence) error
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
	// Invalidate removes one recorded identity (durable row and any cache entry).
	// The purge/remediation surface for verification tooling — nothing on the
	// search/detail pipeline calls it; it exists so a bad bridge found after the
	// fact can be excised without a manual DELETE + cache flush.
	Invalidate(ctx context.Context, kind domain.ResultKind, provider, externalID string) error
}
