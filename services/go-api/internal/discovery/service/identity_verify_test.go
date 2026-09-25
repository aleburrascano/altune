package service

import (
	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/discovery/ports"
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"
)

type fakeMBAnchor struct {
	titles []string
	err    error
	calls  int
}

func (f *fakeMBAnchor) ReleaseGroupTitles(_ context.Context, _ string) ([]string, error) {
	f.calls++
	return f.titles, f.err
}

func verifyAlbums(titles ...string) []domain.SearchResult {
	out := make([]domain.SearchResult, 0, len(titles))
	for _, t := range titles {
		out = append(out, domain.SearchResult{Kind: domain.ResultKindAlbum, Title: t})
	}
	return out
}

func albumProvider(fn func() ([]domain.SearchResult, error)) *fakeArtistContentProvider {
	return &fakeArtistContentProvider{
		getAlbumsFn: func(_ context.Context, _ domain.ProviderName, _ string) ([]domain.SearchResult, error) {
			return fn()
		},
	}
}

func TestIdentityVerifier_dropsMisbridgedEdge(t *testing.T) {
	anchor := &fakeMBAnchor{titles: []string{"Alpha", "Bravo", "Charlie", "Delta", "Echo", "Foxtrot"}}
	deezer := albumProvider(func() ([]domain.SearchResult, error) {
		return verifyAlbums("Nope", "Wrong", "Other", "Bogus", "Zzz"), nil
	})
	spotify := albumProvider(func() ([]domain.SearchResult, error) {
		return verifyAlbums("Alpha", "Bravo", "Charlie", "Delta"), nil
	})
	v := NewIdentityVerifier(anchor, map[domain.ProviderName]ports.ArtistContentProvider{
		domain.ProviderDeezer:  deezer,
		domain.ProviderSpotify: spotify,
	})

	in := map[string]string{"deezer": "wrong-id", "spotify": "right-id", "discogs": "123"}
	out, ok := v.VerifyXref(context.Background(), domain.ResultKindArtist, "mbid-1", in)

	if !ok {
		t.Fatal("first verification must report ok=true (persistable)")
	}
	if _, ok := out["deezer"]; ok {
		t.Errorf("mis-bridged deezer edge should be dropped, got %v", out)
	}
	if out["spotify"] != "right-id" {
		t.Errorf("overlapping spotify edge should be kept, got %v", out)
	}
	if out["discogs"] != "123" {
		t.Errorf("non-catalogue discogs edge should be untouched, got %v", out)
	}
	if _, ok := in["deezer"]; !ok {
		t.Error("VerifyXref mutated the input xref")
	}
}

func TestIdentityVerifier_failOpenTooFewReleaseGroups(t *testing.T) {
	anchor := &fakeMBAnchor{titles: []string{"A", "B"}}
	deezer := albumProvider(func() ([]domain.SearchResult, error) {
		t.Fatal("must not fetch a provider catalogue when there's no anchor to judge against")
		return nil, nil
	})
	v := NewIdentityVerifier(anchor, map[domain.ProviderName]ports.ArtistContentProvider{domain.ProviderDeezer: deezer})

	out, ok := v.VerifyXref(context.Background(), domain.ResultKindArtist, "mbid-2", map[string]string{"deezer": "x"})
	if !ok || out["deezer"] != "x" {
		t.Errorf("fail-open expected (too few release-groups), got %v ok=%v", out, ok)
	}
}

func TestIdentityVerifier_fetchErrorKeepsEdge(t *testing.T) {
	anchor := &fakeMBAnchor{titles: []string{"A", "B", "C", "D", "E"}}
	deezer := albumProvider(func() ([]domain.SearchResult, error) {
		return nil, errors.New("deezer down")
	})
	v := NewIdentityVerifier(anchor, map[domain.ProviderName]ports.ArtistContentProvider{domain.ProviderDeezer: deezer})

	out, ok := v.VerifyXref(context.Background(), domain.ResultKindArtist, "mbid-3", map[string]string{"deezer": "x"})
	if !ok || out["deezer"] != "x" {
		t.Errorf("a fetch error must keep the edge (fail-open), got %v ok=%v", out, ok)
	}
}

func TestIdentityVerifier_memoSkipsSecondVerification(t *testing.T) {
	anchor := &fakeMBAnchor{titles: []string{"A", "B", "C", "D", "E"}}
	deezer := albumProvider(func() ([]domain.SearchResult, error) {
		return verifyAlbums("A", "B", "C", "D"), nil
	})
	v := NewIdentityVerifier(anchor, map[domain.ProviderName]ports.ArtistContentProvider{domain.ProviderDeezer: deezer})

	in := map[string]string{"deezer": "x"}
	_, ok1 := v.VerifyXref(context.Background(), domain.ResultKindArtist, "mbid-4", in)
	out2, ok2 := v.VerifyXref(context.Background(), domain.ResultKindArtist, "mbid-4", in)
	if anchor.calls != 1 {
		t.Errorf("anchor fetched %d times, want 1 (second call memoized)", anchor.calls)
	}
	if !ok1 {
		t.Error("first verification must report ok=true (persistable)")
	}
	if ok2 || out2 != nil {
		t.Errorf("memo hit must return (nil, false), got %v ok=%v", out2, ok2)
	}
}

type recordingIdentityStore struct {
	mu           sync.Mutex
	persisted    []map[string]string
	hadDeadline  bool
	deadlineSeen bool
}

func (f *recordingIdentityStore) PersistBridges(ctx context.Context, _ domain.ResultKind, _ string, xref map[string]string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.persisted = append(f.persisted, xref)
	_, f.hadDeadline = ctx.Deadline()
	f.deadlineSeen = true
	return nil
}

func (f *recordingIdentityStore) LookupByProviderID(_ context.Context, _ domain.ResultKind, _ domain.ProviderKey, _ string) (string, map[string]string, bool) {
	return "", nil, false
}

func (f *recordingIdentityStore) Invalidate(_ context.Context, _ domain.ResultKind, _ domain.ProviderKey, _ string) error {
	return nil
}

func TestStampIdentities_MemoHitDoesNotRePersistRawXref(t *testing.T) {
	anchor := &fakeMBAnchor{titles: []string{"Alpha", "Bravo", "Charlie", "Delta", "Echo", "Foxtrot"}}
	deezer := albumProvider(func() ([]domain.SearchResult, error) {
		return verifyAlbums("Nope", "Wrong", "Other", "Bogus", "Zzz"), nil
	})
	verifier := NewIdentityVerifier(anchor, map[domain.ProviderName]ports.ArtistContentProvider{
		domain.ProviderDeezer: deezer,
	})
	store := &recordingIdentityStore{}
	bridge := &fakeIdentityBridge{byMBID: map[string]map[string]string{
		"mbid-artist": {"deezer": "wrong-id", "discogs": "123"},
	}}
	svc := NewService(nil, NewCircuitBreaker(),
		WithIdentityBridge(bridge),
		WithIdentityStore(store),
		WithIdentityVerifier(verifier),
	)
	groups := func() [][]domain.SearchResult {
		return [][]domain.SearchResult{
			{withMBID(res(domain.ResultKindArtist, "Che", "", domain.ProviderMusicBrainz, nil), "mbid-artist")},
		}
	}

	svc.identity.stamp(context.Background(), groups())
	svc.WaitForBackground()
	svc.identity.stamp(context.Background(), groups())
	svc.WaitForBackground()

	store.mu.Lock()
	defer store.mu.Unlock()
	if len(store.persisted) != 1 {
		t.Fatalf("want exactly 1 persist (memo hit must skip), got %d: %v", len(store.persisted), store.persisted)
	}
	if _, ok := store.persisted[0]["deezer"]; ok {
		t.Errorf("persisted set must not contain the dropped deezer edge, got %v", store.persisted[0])
	}
	if store.persisted[0]["discogs"] != "123" {
		t.Errorf("persisted set lost the untouched discogs edge, got %v", store.persisted[0])
	}
	if !store.hadDeadline {
		t.Error("persist context carried no deadline (identityPersistTimeout not applied)")
	}
}

func TestIdentityVerifier_ForgetReVerifies(t *testing.T) {
	anchor := &fakeMBAnchor{titles: []string{"A", "B", "C", "D", "E"}}
	deezer := albumProvider(func() ([]domain.SearchResult, error) {
		return verifyAlbums("A", "B", "C", "D"), nil
	})
	v := NewIdentityVerifier(anchor, map[domain.ProviderName]ports.ArtistContentProvider{domain.ProviderDeezer: deezer})

	in := map[string]string{"deezer": "x"}
	if _, ok := v.VerifyXref(context.Background(), domain.ResultKindArtist, "mbid-6", in); !ok {
		t.Fatal("first verification must be persistable")
	}
	v.Forget("mbid-6")
	out, ok := v.VerifyXref(context.Background(), domain.ResultKindArtist, "mbid-6", in)
	if !ok || out["deezer"] != "x" {
		t.Errorf("after Forget the next VerifyXref must re-verify and return the xref, got %v ok=%v", out, ok)
	}
	if anchor.calls != 2 {
		t.Errorf("anchor fetched %d times, want 2 (Forget must clear the memo)", anchor.calls)
	}
}

type failingIdentityStore struct {
	mu       sync.Mutex
	attempts int
}

func (f *failingIdentityStore) PersistBridges(context.Context, domain.ResultKind, string, map[string]string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.attempts++
	return errors.New("pg down")
}

func (f *failingIdentityStore) LookupByProviderID(context.Context, domain.ResultKind, domain.ProviderKey, string) (string, map[string]string, bool) {
	return "", nil, false
}

func (f *failingIdentityStore) Invalidate(context.Context, domain.ResultKind, domain.ProviderKey, string) error {
	return nil
}

func TestStampIdentities_PersistFailureUnmarksVerifyMemo(t *testing.T) {
	anchor := &fakeMBAnchor{titles: []string{"Alpha", "Bravo", "Charlie", "Delta", "Echo", "Foxtrot"}}
	verifier := NewIdentityVerifier(anchor, nil)
	store := &failingIdentityStore{}
	bridge := &fakeIdentityBridge{byMBID: map[string]map[string]string{
		"mbid-artist": {"discogs": "123"},
	}}
	svc := NewService(nil, NewCircuitBreaker(),
		WithIdentityBridge(bridge),
		WithIdentityStore(store),
		WithIdentityVerifier(verifier),
	)
	groups := func() [][]domain.SearchResult {
		return [][]domain.SearchResult{
			{withMBID(res(domain.ResultKindArtist, "Che", "", domain.ProviderMusicBrainz, nil), "mbid-artist")},
		}
	}

	svc.identity.stamp(context.Background(), groups())
	svc.WaitForBackground()
	svc.identity.stamp(context.Background(), groups())
	svc.WaitForBackground()

	store.mu.Lock()
	defer store.mu.Unlock()
	if store.attempts != 2 {
		t.Fatalf("persist attempts = %d, want 2 (failed persist must not leave the memo marked)", store.attempts)
	}
}

func TestVerifyMemo_MarkReapsEntriesPastTheirTTL(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	memo := newVerifyMemo(time.Hour, func() time.Time { return now })
	for i := range 500 {
		memo.mark(fmt.Sprintf("mbid-%d", i))
	}

	now = now.Add(time.Hour + time.Second)
	memo.mark("mbid-fresh")

	if len(memo.m) != 1 {
		t.Fatalf("memo holds %d entries, want 1 (a mark past the ttl must reap the expired ones)", len(memo.m))
	}
	if !memo.seen("mbid-fresh") {
		t.Error("the entry being marked was reaped by its own sweep")
	}
}

func TestVerifyMemo_SweepKeepsEntriesInsideTheirTTL(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	memo := newVerifyMemo(time.Hour, func() time.Time { return now })

	memo.mark("mbid-stale")
	now = now.Add(30 * time.Minute)
	memo.mark("mbid-recent")
	now = now.Add(31 * time.Minute)
	memo.mark("mbid-new")

	if memo.seen("mbid-stale") {
		t.Error("mbid-stale outlived its ttl and must no longer be memoized")
	}
	if !memo.seen("mbid-recent") {
		t.Error("mbid-recent is inside its ttl and the sweep dropped it")
	}
	if len(memo.m) != 2 {
		t.Errorf("memo holds %d entries, want 2 (only the expired entry swept): %v", len(memo.m), memo.m)
	}
}

func TestIdentityVerifier_nonArtistKindUntouched(t *testing.T) {
	anchor := &fakeMBAnchor{titles: []string{"A", "B", "C", "D", "E"}}
	v := NewIdentityVerifier(anchor, nil)
	in := map[string]string{"deezer": "x"}
	out, ok := v.VerifyXref(context.Background(), domain.ResultKindAlbum, "mbid-5", in)
	if !ok || out["deezer"] != "x" || anchor.calls != 0 {
		t.Errorf("album-kind identity must be untouched (verification is artist-only)")
	}
}

// Pins which xref keys the verifier checks and which content provider serves
// each: an "itunes" xref edge is verified through the Apple Music provider.
func TestVerifiableEdge(t *testing.T) {
	tests := []struct {
		key    string
		want   domain.ProviderName
		wantOK bool
	}{
		{key: "deezer", want: domain.ProviderDeezer, wantOK: true},
		{key: "spotify", want: domain.ProviderSpotify, wantOK: true},
		{key: "itunes", want: domain.ProviderAppleMusic, wantOK: true},
		{key: "applemusic"},
		{key: "discogs"},
		{key: "soundcloud"},
		{key: "wikidata"},
		{key: "musicbrainz"},
		{key: "Deezer"},
		{key: ""},
	}
	for _, tt := range tests {
		got, ok := verifiableEdge(tt.key)
		if ok != tt.wantOK || (ok && got != tt.want) {
			t.Errorf("verifiableEdge(%q) = (%v, %v), want (%v, %v)", tt.key, got, ok, tt.want, tt.wantOK)
		}
	}
}

func TestIdentityVerifier_ForgetNilSafe(t *testing.T) {
	var v *IdentityVerifier
	v.Forget("any-mbid")
}
