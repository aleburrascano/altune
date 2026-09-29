package enrich

import (
	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/discovery/ports"
	"context"
	"errors"
	"testing"
	"time"
)

type fakeEnricher struct {
	lookups    int
	resolves   int
	lookupErr  error
	resolveID  string
	enrichment domain.MBEnrichment
}

func (f *fakeEnricher) ResolveMBID(_ context.Context, _ domain.ResultKind, _, _ string) (string, error) {
	f.resolves++
	return f.resolveID, nil
}

func (f *fakeEnricher) Lookup(_ context.Context, _ domain.ResultKind, _ string) (domain.MBEnrichment, error) {
	f.lookups++
	if f.lookupErr != nil {
		return domain.EmptyEnrichment(), f.lookupErr
	}
	return f.enrichment, nil
}

type fakeArtwork struct {
	calls int
	url   string
	err   error
}

func (f *fakeArtwork) ResolveTagged(_ context.Context, _ domain.ResultKind, _, _, _ string) (string, domain.ProviderKey, error) {
	f.calls++
	return f.url, "", f.err
}

func (f *fakeArtwork) ResolveWithIdentityTagged(_ context.Context, _ domain.ResultKind, _, _ string, _ ports.ArtworkIdentity) (string, domain.ProviderKey, error) {
	f.calls++
	if f.url != "" || f.err != nil {
		return f.url, "", f.err
	}
	return "", "", nil
}

type memEnrichmentCache struct {
	pos map[string]domain.MBEnrichment
	neg map[string]bool
}

func newMemCache() *memEnrichmentCache {
	return &memEnrichmentCache{pos: map[string]domain.MBEnrichment{}, neg: map[string]bool{}}
}

func (c *memEnrichmentCache) Get(_ context.Context, kind domain.ResultKind, mbid string) (domain.MBEnrichment, bool, error) {
	e, ok := c.pos[kind.String()+"|"+mbid]
	return e, ok, nil
}

func (c *memEnrichmentCache) Set(_ context.Context, kind domain.ResultKind, mbid string, e domain.MBEnrichment) error {
	c.pos[kind.String()+"|"+mbid] = e
	return nil
}

func (c *memEnrichmentCache) GetNegative(_ context.Context, kind domain.ResultKind, nameKey string) (bool, error) {
	return c.neg[kind.String()+"|"+nameKey], nil
}

func (c *memEnrichmentCache) SetNegative(_ context.Context, kind domain.ResultKind, nameKey string) error {
	c.neg[kind.String()+"|"+nameKey] = true
	return nil
}

type fakeMBIDMemo struct {
	remembered map[string]string
}

func (m *fakeMBIDMemo) LookupMBID(_ context.Context, _ domain.ResultKind, _ string) (string, bool) {
	return "", false
}

func (m *fakeMBIDMemo) RememberMBID(_ context.Context, kind domain.ResultKind, nameKey, mbid string) error {
	m.remembered[kind.String()+"|"+nameKey] = mbid
	return nil
}

func TestEnrichmentService_ResolveWarmsMBIDMemo(t *testing.T) {
	enr := &fakeEnricher{resolveID: "mbid-warm", enrichment: sampleEnrichment()}
	memo := &fakeMBIDMemo{remembered: map[string]string{}}
	svc := NewEnrichmentService(enr, &fakeArtwork{}, newMemCache(), WithMBIDMemo(memo))

	if _, err := svc.Execute(context.Background(), domain.ResultKindAlbum, "DAMN.", "Kendrick Lamar", ""); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(memo.remembered) != 1 {
		t.Fatalf("expected 1 remembered name→mbid mapping, got %d", len(memo.remembered))
	}
	for _, v := range memo.remembered {
		if v != "mbid-warm" {
			t.Errorf("remembered mbid = %q, want mbid-warm", v)
		}
	}
}

func sampleEnrichment() domain.MBEnrichment {
	e := domain.EmptyEnrichment()
	e.MBID = "mbid-1"
	e.Genres = []string{"hip hop"}
	e.Year = 2017
	return e
}

func TestEnrichmentService_PassedMBID_CachesWholeValue(t *testing.T) {
	enr := &fakeEnricher{enrichment: sampleEnrichment()}
	art := &fakeArtwork{url: "https://caa/1200.jpg"}
	cache := newMemCache()
	svc := NewEnrichmentService(enr, art, cache)

	got, err := svc.Execute(context.Background(), domain.ResultKindAlbum, "DAMN.", "Kendrick Lamar", "mbid-1")
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if got.ArtworkURL != "https://caa/1200.jpg" || got.Year != 2017 {
		t.Errorf("merged result wrong: %#v", got)
	}

	got2, _ := svc.Execute(context.Background(), domain.ResultKindAlbum, "DAMN.", "Kendrick Lamar", "mbid-1")
	if got2.ArtworkURL != "https://caa/1200.jpg" {
		t.Errorf("cached value lost artwork: %#v", got2)
	}
	if enr.lookups != 1 {
		t.Errorf("lookups = %d, want 1 (second served from cache)", enr.lookups)
	}
	if art.calls != 1 {
		t.Errorf("artwork calls = %d, want 1 (cached value includes artwork_url)", art.calls)
	}
}

func TestEnrichmentService_LookupError_DegradesToEmpty(t *testing.T) {
	enr := &fakeEnricher{lookupErr: errors.New("mb 503")}
	cache := newMemCache()
	svc := NewEnrichmentService(enr, &fakeArtwork{}, cache)

	got, err := svc.Execute(context.Background(), domain.ResultKindArtist, "X", "", "mbid-err")
	if !errors.Is(err, ErrDegraded) {
		t.Fatalf("a lookup error must surface as ErrDegraded, got %v", err)
	}
	if !got.IsZero() {
		t.Errorf("want empty enrichment on lookup error, got %#v", got)
	}
	if _, found, _ := cache.Get(context.Background(), domain.ResultKindArtist, "mbid-err"); found {
		t.Error("lookup error must not be cached")
	}
}

func TestEnrichmentService_PassedMBID_404DegradesToEmpty(t *testing.T) {
	enr := &fakeEnricher{lookupErr: errors.New("musicbrainz returned 404")}
	svc := NewEnrichmentService(enr, nil, nil)

	got, err := svc.Execute(context.Background(), domain.ResultKindAlbum, "Stale", "", "stale-mbid")
	if !errors.Is(err, ErrDegraded) || !got.IsZero() {
		t.Errorf("404 lookup must degrade to empty+ErrDegraded, got %#v err=%v", got, err)
	}
}

func TestEnrichmentService_ArtworkMerged(t *testing.T) {
	enr := &fakeEnricher{enrichment: domain.EmptyEnrichment()}
	art := &fakeArtwork{url: "https://caa/front-1200.jpg"}
	svc := NewEnrichmentService(enr, art, nil)

	got, _ := svc.Execute(context.Background(), domain.ResultKindAlbum, "T", "A", "mbid-art")
	if got.ArtworkURL != "https://caa/front-1200.jpg" {
		t.Errorf("artwork_url = %q, want chain result", got.ArtworkURL)
	}
}

func TestEnrichmentService_CoverlessEntryFromADownChainIsNotCached(t *testing.T) {
	enr := &fakeEnricher{enrichment: sampleEnrichment()}
	art := &fakeArtwork{err: ports.ErrArtworkDegraded}
	cache := newMemCache()
	svc := NewEnrichmentService(enr, art, cache)

	got, err := svc.Execute(context.Background(), domain.ResultKindAlbum, "DAMN.", "Kendrick Lamar", "mbid-1")
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if got.Year != 2017 {
		t.Errorf("want the MB data returned despite the artwork outage, got %#v", got)
	}
	if _, found, _ := cache.Get(context.Background(), domain.ResultKindAlbum, "mbid-1"); found {
		t.Error("a coverless entry from a failing artwork chain must not be cached")
	}

	art.url, art.err = "https://caa/1200.jpg", nil
	recovered, _ := svc.Execute(context.Background(), domain.ResultKindAlbum, "DAMN.", "Kendrick Lamar", "mbid-1")

	if recovered.ArtworkURL != "https://caa/1200.jpg" {
		t.Errorf("artwork_url after recovery = %q, want the resolved cover", recovered.ArtworkURL)
	}
}

func TestEnrichmentService_Unresolved_NegativeCached(t *testing.T) {
	enr := &fakeEnricher{resolveID: ""}
	cache := newMemCache()
	svc := NewEnrichmentService(enr, &fakeArtwork{}, cache)

	got, _ := svc.Execute(context.Background(), domain.ResultKindAlbum, "Unknown", "Nobody", "")
	if !got.IsZero() {
		t.Errorf("want empty on no resolve, got %#v", got)
	}
	_, _ = svc.Execute(context.Background(), domain.ResultKindAlbum, "Unknown", "Nobody", "")
	if enr.resolves != 1 {
		t.Errorf("resolves = %d, want 1 (miss negatively cached)", enr.resolves)
	}
	if enr.lookups != 0 {
		t.Errorf("lookups = %d, want 0 (never resolved an mbid)", enr.lookups)
	}
}

type nameVsIdentityArtwork struct {
	nameCalls     int
	identityCalls int
	nameURL       string
	identityURL   string
	identityErr   error
}

func (f *nameVsIdentityArtwork) ResolveTagged(_ context.Context, _ domain.ResultKind, _, _, _ string) (string, domain.ProviderKey, error) {
	f.nameCalls++
	return f.nameURL, "", nil
}

func (f *nameVsIdentityArtwork) ResolveWithIdentityTagged(_ context.Context, _ domain.ResultKind, _, _ string, id ports.ArtworkIdentity) (string, domain.ProviderKey, error) {
	f.identityCalls++
	if id.MBID == "" {
		return "", "", nil
	}
	if f.identityURL == "" && f.identityErr != nil {
		return "", "", f.identityErr
	}
	return f.identityURL, "", nil
}

func TestEnrichmentService_CallerSuppliedMBID_DoesNotMergeNameDerivedArtwork(t *testing.T) {
	enr := &fakeEnricher{enrichment: sampleEnrichment()}
	art := &nameVsIdentityArtwork{nameURL: "https://poisoned/from-unrelated-title.jpg"}
	cache := newMemCache()
	svc := NewEnrichmentService(enr, art, cache)

	got, err := svc.Execute(context.Background(), domain.ResultKindAlbum,
		"Some Other Album", "Other Artist", "mbid-1")
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if got.ArtworkURL == art.nameURL {
		t.Errorf("artwork_url = %q, a caller-supplied mbid must not pick up name-derived art from an unverified title/subtitle", got.ArtworkURL)
	}
	if art.nameCalls != 0 {
		t.Errorf("name-based artwork resolver called %d times, want 0 on the caller-supplied mbid path", art.nameCalls)
	}
	if art.identityCalls != 1 {
		t.Errorf("identity-based artwork resolver called %d times, want 1", art.identityCalls)
	}

	cached, found, _ := cache.Get(context.Background(), domain.ResultKindAlbum, "mbid-1")
	if !found {
		t.Fatalf("expected the mbid-keyed entry to be cached")
	}
	if cached.ArtworkURL == art.nameURL {
		t.Errorf("cached artwork_url = %q, the poisoned cover must not reach the shared cache entry", cached.ArtworkURL)
	}
}

func TestEnrichmentService_CallerSuppliedMBID_UsesIdentityArtworkWhenAvailable(t *testing.T) {
	enr := &fakeEnricher{enrichment: sampleEnrichment()}
	art := &nameVsIdentityArtwork{
		nameURL:     "https://poisoned/from-unrelated-title.jpg",
		identityURL: "https://caa/real-cover.jpg",
	}
	svc := NewEnrichmentService(enr, art, newMemCache())

	got, err := svc.Execute(context.Background(), domain.ResultKindAlbum,
		"Some Other Album", "Other Artist", "mbid-1")
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if got.ArtworkURL != art.identityURL {
		t.Errorf("artwork_url = %q, want the identity-resolved cover %q", got.ArtworkURL, art.identityURL)
	}
}

func TestEnrichmentService_ResolvedFromTitle_FallsBackToNameSearchWhenIdentityMisses(t *testing.T) {
	enr := &fakeEnricher{resolveID: "mbid-title-1", enrichment: sampleEnrichment()}
	art := &nameVsIdentityArtwork{nameURL: "https://caa/name-derived.jpg"}
	cache := newMemCache()
	svc := NewEnrichmentService(enr, art, cache)

	got, err := svc.Execute(context.Background(), domain.ResultKindAlbum, "DAMN.", "Kendrick Lamar", "")
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if got.ArtworkURL != art.nameURL {
		t.Errorf("artwork_url = %q, want the name search's cover %q once the identity walk missed", got.ArtworkURL, art.nameURL)
	}
	if art.identityCalls != 1 {
		t.Errorf("identity resolver called %d times, want 1 (tried first even on the trusted, title-resolved path)", art.identityCalls)
	}
	if art.nameCalls != 1 {
		t.Errorf("name resolver called %d times, want 1 (the fallback this path is allowed to take)", art.nameCalls)
	}
}

func TestEnrichmentService_NameVerifiedMissWithIdentityError_CachesVerifiedMiss(t *testing.T) {
	enr := &fakeEnricher{resolveID: "mbid-verified-miss", enrichment: sampleEnrichment()}
	art := &nameVsIdentityArtwork{identityErr: errors.New("identity provider 503")}
	cache := newMemCache()
	svc := NewEnrichmentService(enr, art, cache)

	got, err := svc.Execute(context.Background(), domain.ResultKindAlbum, "DAMN.", "Kendrick Lamar", "")
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if got.ArtworkURL != "" {
		t.Errorf("artwork_url = %q, want empty (the name walk verified no art)", got.ArtworkURL)
	}

	cached, found, _ := cache.Get(context.Background(), domain.ResultKindAlbum, "mbid-verified-miss")
	if !found {
		t.Fatalf("a name-verified miss must be cached even though the identity walk errored first")
	}
	if cached.ArtworkURL != "" {
		t.Errorf("cached artwork_url = %q, want empty", cached.ArtworkURL)
	}
}

type deadlineCapturingArtwork struct {
	identityDeadline time.Time
	nameDeadline     time.Time
}

func (f *deadlineCapturingArtwork) ResolveWithIdentityTagged(ctx context.Context, _ domain.ResultKind, _, _ string, _ ports.ArtworkIdentity) (string, domain.ProviderKey, error) {
	f.identityDeadline, _ = ctx.Deadline()
	return "", "", nil
}

func (f *deadlineCapturingArtwork) ResolveTagged(ctx context.Context, _ domain.ResultKind, _, _, _ string) (string, domain.ProviderKey, error) {
	f.nameDeadline, _ = ctx.Deadline()
	return "", "", nil
}

func TestEnrichmentService_TitleResolvedLookup_SharesOneArtworkDeadline(t *testing.T) {
	enr := &fakeEnricher{resolveID: "mbid-deadline", enrichment: sampleEnrichment()}
	art := &deadlineCapturingArtwork{}
	svc := NewEnrichmentService(enr, art, newMemCache())

	if _, err := svc.Execute(context.Background(), domain.ResultKindAlbum, "DAMN.", "Kendrick Lamar", ""); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if art.identityDeadline.IsZero() || art.nameDeadline.IsZero() {
		t.Fatalf("expected both artwork calls to run under a deadline, got identity=%v name=%v", art.identityDeadline, art.nameDeadline)
	}
	if !art.identityDeadline.Equal(art.nameDeadline) {
		t.Errorf("identity call deadline %v != name-search call deadline %v; a title-resolved lookup must share one artwork budget instead of granting each call its own 20s", art.identityDeadline, art.nameDeadline)
	}
}
