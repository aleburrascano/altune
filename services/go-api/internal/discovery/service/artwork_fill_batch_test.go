package service

import (
	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/discovery/ports"
	"context"
	"strconv"
	"sync"
	"testing"
)

// countingIdentityStore counts every store call so the fill's round-trip cost
// is measurable. It knows an identity for every even external id.
type countingIdentityStore struct {
	mu          sync.Mutex
	singleCalls int
}

func (s *countingIdentityStore) PersistBridges(context.Context, domain.ResultKind, string, map[string]string) error {
	return nil
}

func (s *countingIdentityStore) LookupByProviderID(_ context.Context, kind domain.ResultKind, provider domain.ProviderKey, externalID string) (string, map[string]string, bool) {
	s.mu.Lock()
	s.singleCalls++
	s.mu.Unlock()
	hit, ok := knownIdentity(ports.IdentityRef{Kind: kind, Provider: provider, ExternalID: externalID})
	return hit.MBID, hit.Xref, ok
}

func (s *countingIdentityStore) Invalidate(context.Context, domain.ResultKind, domain.ProviderKey, string) error {
	return nil
}

func knownIdentity(ref ports.IdentityRef) (ports.IdentityHit, bool) {
	n, err := strconv.Atoi(ref.ExternalID)
	if err != nil || n%2 != 0 {
		return ports.IdentityHit{}, false
	}
	return ports.IdentityHit{MBID: "mbid-" + ref.ExternalID, Xref: map[string]string{"discogs": "d" + ref.ExternalID}}, true
}

// batchingIdentityStore also offers the one-round-trip batch lookup.
type batchingIdentityStore struct {
	countingIdentityStore
	batchCalls int
	batchRefs  []ports.IdentityRef
}

func (s *batchingIdentityStore) LookupByProviderIDs(_ context.Context, refs []ports.IdentityRef) map[ports.IdentityRef]ports.IdentityHit {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.batchCalls++
	s.batchRefs = append(s.batchRefs, refs...)
	hits := map[ports.IdentityRef]ports.IdentityHit{}
	for _, ref := range refs {
		if hit, ok := knownIdentity(ref); ok {
			hits[ref] = hit
		}
	}
	return hits
}

func fiftyArtlessResults() []domain.SearchResult {
	results := make([]domain.SearchResult, artworkFillLimit)
	for i := range results {
		id := strconv.Itoa(i)
		results[i] = domain.NewProviderResult(domain.ResultKindTrack, "Song "+id, "Artist", "",
			domain.SourceRef{Provider: domain.ProviderDeezer, ExternalID: id}, nil)
	}
	return results
}

func assertDurableXrefApplied(t *testing.T, got []domain.SearchResult) {
	t.Helper()
	for i, r := range got {
		want := ""
		if i%2 == 0 {
			want = "d" + strconv.Itoa(i)
		}
		if r.Xref["discogs"] != want {
			t.Errorf("result %d xref = %v, want discogs=%q", i, r.Xref, want)
		}
	}
}

func TestArtworkFiller_BatchesDurableIdentityLookups(t *testing.T) {
	store := &batchingIdentityStore{}
	f := newArtworkFiller(&fakeArtworkResolver{}, nil, store, nil)

	got := f.fill(context.Background(), fiftyArtlessResults())

	if store.batchCalls != 1 {
		t.Errorf("batch lookups = %d, want 1 for a %d-result slate", store.batchCalls, artworkFillLimit)
	}
	if store.singleCalls != 0 {
		t.Errorf("per-result lookups = %d, want 0 when the store batches", store.singleCalls)
	}
	if len(store.batchRefs) != artworkFillLimit {
		t.Errorf("batched refs = %d, want %d", len(store.batchRefs), artworkFillLimit)
	}
	assertDurableXrefApplied(t, got)
}

func TestArtworkFiller_BatchSkipsResultsThatNeedNoDurableLookup(t *testing.T) {
	store := &batchingIdentityStore{}
	f := newArtworkFiller(&fakeArtworkResolver{}, nil, store, nil)
	results := fiftyArtlessResults()[:3]
	results[0].ImageURL = "https://provider/art.jpg"
	results[1].Xref = map[string]string{"spotify": "s1"}

	f.fill(context.Background(), results)

	want := []ports.IdentityRef{{Kind: domain.ResultKindTrack, Provider: domain.ProviderDeezer.Key(), ExternalID: "2"}}
	if len(store.batchRefs) != 1 || store.batchRefs[0] != want[0] {
		t.Errorf("batched refs = %v, want only %v", store.batchRefs, want)
	}
	if store.singleCalls != 0 {
		t.Errorf("per-result lookups = %d, want 0", store.singleCalls)
	}
}

func TestArtworkFiller_FallsBackToPerResultLookupWithoutBatch(t *testing.T) {
	store := &countingIdentityStore{}
	f := newArtworkFiller(&fakeArtworkResolver{}, nil, store, nil)

	got := f.fill(context.Background(), fiftyArtlessResults())

	if store.singleCalls != artworkFillLimit {
		t.Errorf("per-result lookups = %d, want %d for a non-batching store", store.singleCalls, artworkFillLimit)
	}
	assertDurableXrefApplied(t, got)
}
