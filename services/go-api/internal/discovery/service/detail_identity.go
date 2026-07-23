package service

import (
	"context"

	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/discovery/ports"
)

// ResolvedArtistIdentity is an artist's full cross-provider identity: the MBID
// plus each provider's own external id for the same artist. The identity-first
// detail path fans out over ProviderIDs — asking each provider for THIS artist by
// ITS id — so a same-name artist ("Che") can never bleed into the discography or
// top tracks the way a single trusted provider id lets it.
type ResolvedArtistIdentity struct {
	MBID        string
	ProviderIDs map[domain.ProviderName]string
}

// resolveArtistIdentity expands a single (provider, externalID) into the artist's
// full cross-provider identity via the durable IdentityStore (keyed on stable
// ids, never names). The seed id is always present in the result. ok is false when
// the store is absent or has no bridge for this id yet — the caller then falls
// back to the current single-provider path rather than guessing.
func resolveArtistIdentity(
	ctx context.Context,
	store ports.IdentityStore,
	provider domain.ProviderName,
	externalID string,
) (ResolvedArtistIdentity, bool) {
	identity := ResolvedArtistIdentity{
		ProviderIDs: map[domain.ProviderName]string{provider: externalID},
	}
	if store == nil || externalID == "" {
		return identity, false
	}

	mbid, xref, ok := store.LookupByProviderID(ctx, domain.ResultKindArtist, provider.String(), externalID)
	if !ok {
		return identity, false
	}

	identity.MBID = mbid
	// Overlay the bridged ids. The seed (provider→externalID) stays even if xref
	// omits it, so the resolved identity is never narrower than the input.
	for name, id := range xref {
		if id == "" {
			continue
		}
		if pn, err := domain.ParseProviderName(name); err == nil {
			identity.ProviderIDs[pn] = id
		}
	}
	return identity, true
}

// providerContentID resolves the id to query a given content provider by, for one
// artist. Most providers use their own bridged id; the two exceptions are the
// essential differences imposed by their APIs, handled here in one place rather
// than scattered per call site:
//   - Last.fm keys artist content on the MBID (its "id" is a name, ambiguous).
//   - Apple Music shares the iTunes catalog id space (the bridge only emits the
//     "itunes" key), so it reuses that id.
//
// Returns "" when no id is available — the provider then sits out this artist.
func providerContentID(identity ResolvedArtistIdentity, name domain.ProviderName) string {
	if id := identity.ProviderIDs[name]; id != "" {
		return id
	}
	switch name {
	case domain.ProviderLastFM:
		return identity.MBID
	case domain.ProviderAppleMusic:
		return identity.ProviderIDs[domain.ProviderITunes]
	}
	return ""
}

// resolveArtistIDByName is the fallback for a provider the cross-provider identity
// carries no id for (SoundCloud, which MusicBrainz never bridges): if the provider
// implements ArtistIDResolver, resolve its own id from the artist name once, so its
// exclusive catalogue joins the id-based fan-out. Returns "" when the provider
// can't resolve (or no name), leaving it to sit out rather than guess.
func resolveArtistIDByName(ctx context.Context, p ports.ArtistContentProvider, artistName string) string {
	if artistName == "" {
		return ""
	}
	resolver, ok := p.(ports.ArtistIDResolver)
	if !ok {
		return ""
	}
	if id, ok := resolver.ResolveArtistID(ctx, artistName); ok {
		return id
	}
	return ""
}
