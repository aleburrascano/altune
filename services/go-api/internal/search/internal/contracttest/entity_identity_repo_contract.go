package contracttest

import (
	"altune/go-api/internal/search/internal/domain"
	"altune/go-api/internal/search/internal/ports"
	"testing"

	"github.com/google/uuid"
)

type IdentityRepoUnderTest interface {
	ports.IdentityStore
	SeedIdentity(t *testing.T, kind domain.ResultKind, mbid string, xref map[domain.ProviderKey]string)
}

func IdentityRepoContract(t *testing.T, newRepo func(t *testing.T) IdentityRepoUnderTest) {
	t.Helper()
	contracts := map[string]func(*testing.T, IdentityRepoUnderTest){
		"any provider id of an entity finds its mbid and full bridge": contractIdentityFoundByAnyProvider,
		"the kind is part of the key":                                 contractIdentityKindIsPartOfTheKey,
		"an unknown id misses":                                        contractIdentityUnknownMisses,
		"an empty provider or id misses":                              contractIdentityEmptyMisses,
		"a batch returns the hits and omits the misses":               contractIdentityBatchReturnsHitsOnly,
		"a batch of nothing returns no hits":                          contractIdentityEmptyBatch,
	}
	for name, contract := range contracts {
		t.Run(name, func(t *testing.T) { contract(t, newRepo(t)) })
	}
}

type seededIdentity struct {
	mbid     string
	deezerID string
	discogs  string
}

func seedArtist(t *testing.T, r IdentityRepoUnderTest) seededIdentity {
	t.Helper()
	s := seededIdentity{mbid: uuid.NewString(), deezerID: "dz-" + uuid.NewString(), discogs: "dc-" + uuid.NewString()}
	r.SeedIdentity(t, domain.ResultKindArtist, s.mbid, map[domain.ProviderKey]string{"deezer": s.deezerID, "discogs": s.discogs})
	return s
}

func contractIdentityFoundByAnyProvider(t *testing.T, r IdentityRepoUnderTest) {
	s := seedArtist(t, r)
	for provider, externalID := range map[domain.ProviderKey]string{"deezer": s.deezerID, "discogs": s.discogs} {
		mbid, xref, ok := r.LookupByProviderID(t.Context(), domain.ResultKindArtist, provider, externalID)
		if !ok || mbid != s.mbid || xref["deezer"] != s.deezerID || xref["discogs"] != s.discogs {
			t.Errorf("lookup via %s = %q, %v, %v, want %q with the full bridge", provider, mbid, xref, ok, s.mbid)
		}
	}
}

func contractIdentityKindIsPartOfTheKey(t *testing.T, r IdentityRepoUnderTest) {
	s := seedArtist(t, r)

	if _, _, ok := r.LookupByProviderID(t.Context(), domain.ResultKindAlbum, "deezer", s.deezerID); ok {
		t.Error("lookup as an album hit, want a miss")
	}
}

func contractIdentityUnknownMisses(t *testing.T, r IdentityRepoUnderTest) {
	if _, _, ok := r.LookupByProviderID(t.Context(), domain.ResultKindArtist, "deezer", "absent-"+uuid.NewString()); ok {
		t.Error("lookup of an unknown id hit, want a miss")
	}
}

func contractIdentityEmptyMisses(t *testing.T, r IdentityRepoUnderTest) {
	s := seedArtist(t, r)

	if _, _, ok := r.LookupByProviderID(t.Context(), domain.ResultKindArtist, "", s.deezerID); ok {
		t.Error("lookup without a provider hit, want a miss")
	}
	if _, _, ok := r.LookupByProviderID(t.Context(), domain.ResultKindArtist, "deezer", ""); ok {
		t.Error("lookup without an id hit, want a miss")
	}
}

func contractIdentityBatchReturnsHitsOnly(t *testing.T, r IdentityRepoUnderTest) {
	s := seedArtist(t, r)
	known := ports.IdentityRef{Kind: domain.ResultKindArtist, Provider: "deezer", ExternalID: s.deezerID}
	unknown := ports.IdentityRef{Kind: domain.ResultKindArtist, Provider: "deezer", ExternalID: "absent-" + uuid.NewString()}
	blank := ports.IdentityRef{Kind: domain.ResultKindArtist}

	hits := r.LookupByProviderIDs(t.Context(), []ports.IdentityRef{known, unknown, blank})

	if len(hits) != 1 || hits[known].MBID != s.mbid || hits[known].Xref["discogs"] != s.discogs {
		t.Errorf("hits = %+v, want only %v resolved with its bridge", hits, known)
	}
}

func contractIdentityEmptyBatch(t *testing.T, r IdentityRepoUnderTest) {
	if hits := r.LookupByProviderIDs(t.Context(), nil); len(hits) != 0 {
		t.Errorf("hits = %+v, want none", hits)
	}
}
