package service

import (
	"altune/go-api/internal/search/internal/domain"
	"altune/go-api/internal/search/internal/ports"
	"context"
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
	if externalID == "" {
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

type ArtistIdentity interface {
	Identify(ctx context.Context, provider domain.ProviderName, externalID string, known map[domain.ProviderName]string) (identity ResolvedArtistIdentity, servesIdentityPath bool)
}

type StoreIdentity struct {
	store ports.IdentityStore
}

func NewStoreIdentity(store ports.IdentityStore) (*StoreIdentity, error) {
	if err := requireCollaborators("the store identity", collaborator{"an identity store", store == nil}); err != nil {
		return nil, err
	}
	return &StoreIdentity{store: store}, nil
}

func (i *StoreIdentity) Identify(ctx context.Context, provider domain.ProviderName, externalID string, known map[domain.ProviderName]string) (ResolvedArtistIdentity, bool) {
	identity, _ := resolveArtistIdentity(ctx, i.store, provider, externalID)
	mergeKnownIDs(identity, known)
	return identity, true
}

type SeedPathIdentity struct{}

func (SeedPathIdentity) Identify(_ context.Context, provider domain.ProviderName, externalID string, known map[domain.ProviderName]string) (ResolvedArtistIdentity, bool) {
	identity := ResolvedArtistIdentity{ProviderIDs: map[domain.ProviderName]string{provider: externalID}}
	mergeKnownIDs(identity, known)
	return identity, len(known) > 0
}

func mergeKnownIDs(identity ResolvedArtistIdentity, known map[domain.ProviderName]string) {
	for name, id := range known {
		if identity.ProviderIDs[name] == "" && id != "" {
			identity.ProviderIDs[name] = id
		}
	}
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
