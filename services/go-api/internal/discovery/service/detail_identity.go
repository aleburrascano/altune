package service

import (
	"context"

	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/discovery/ports"
)

type ResolvedArtistIdentity struct {
	MBID        string
	ProviderIDs map[domain.ProviderName]string
}

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

	mbid, xref, ok := store.LookupByProviderID(ctx, domain.ResultKindArtist, provider.Key(), externalID)
	if !ok {
		return identity, false
	}

	identity.MBID = mbid
	for name, id := range xref {
		if id == "" {
			continue
		}
		if pn, ok := name.ProviderName(); ok {
			identity.ProviderIDs[pn] = id
		}
	}
	return identity, true
}

func (s *GetArtistContentService) artistIdentity(
	ctx context.Context,
	provider domain.ProviderName,
	externalID string,
	known map[domain.ProviderName]string,
) ResolvedArtistIdentity {
	identity, _ := resolveArtistIdentity(ctx, s.identityStore, provider, externalID)
	for name, id := range known {
		if identity.ProviderIDs[name] == "" && id != "" {
			identity.ProviderIDs[name] = id
		}
	}
	return identity
}

var catalogProviders = []domain.ProviderName{
	domain.ProviderDeezer, domain.ProviderAppleMusic, domain.ProviderITunes, domain.ProviderSpotify,
}

func (id ResolvedArtistIdentity) knownToACatalog() bool {
	for _, name := range catalogProviders {
		if id.ProviderIDs[name] != "" {
			return true
		}
	}
	return false
}

func providerContentID(identity ResolvedArtistIdentity, name domain.ProviderName) string {
	if id := identity.ProviderIDs[name]; id != "" {
		return id
	}
	if name == domain.ProviderAppleMusic {
		return identity.ProviderIDs[domain.ProviderITunes]
	}
	return ""
}
