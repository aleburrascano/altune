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

	mbid, xref, ok := store.LookupByProviderID(ctx, domain.ResultKindArtist, provider.String(), externalID)
	if !ok {
		return identity, false
	}

	identity.MBID = mbid
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
