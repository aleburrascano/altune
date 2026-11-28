package service_test

import (
	"altune/go-api/internal/search/internal/adapters/memory"
	"altune/go-api/internal/search/internal/domain"
	"altune/go-api/internal/search/internal/ports"
	"altune/go-api/internal/search/internal/service"
	"altune/go-api/internal/search/searchtest"
	"altune/go-api/internal/shared/background"
	"altune/go-api/internal/shared/clock/clocktest"
	"context"
	"errors"
	"slices"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
	"time"
)

func songs(titles ...string) []domain.UnreleasedSong {
	out := make([]domain.UnreleasedSong, len(titles))
	for i, title := range titles {
		out[i] = domain.UnreleasedSong{GeniusID: i + 1, Title: title}
	}
	return out
}

func titlesOf(list []domain.UnreleasedSong) []string {
	out := make([]string, len(list))
	for i, s := range list {
		out[i] = s.Title
	}
	return out
}

func TestService_List_FiltersByExactCatalogMatch(t *testing.T) {
	tests := []struct {
		name    string
		catalog []string
		genius  []string
		want    []string
	}{
		{"released title is hidden", []string{"Magnolia"}, []string{"Magnolia", "Cancun"}, []string{"Cancun"}},
		{"alias parenthetical is a different song", []string{"After Party"}, []string{"After Party (Devil Wears Prada)"}, []string{"After Party (Devil Wears Prada)"}},
		{"demo is a different recording", []string{"So Beautiful", "Beautiful"}, []string{"Beautiful* [Demo]", "Beautiful (Demo)"}, []string{"Beautiful* [Demo]", "Beautiful (Demo)"}},
		{"trailing asterisk is ignored", []string{"Song"}, []string{"Song*"}, nil},
		{"unreleased marker is ignored", []string{"Better Than"}, []string{"Better Than (Unreleased)", "Better Than [unreleased]"}, nil},
		{"version two is not the original", []string{"Starstruck"}, []string{"Starstruck (Version 2)", "Starstruck [V2]"}, []string{"Starstruck (Version 2)", "Starstruck [V2]"}},
		{"case and punctuation do not matter", []string{"Back It Up"}, []string{"BACK IT UP"}, nil},
		{"never fuzzy", []string{"Rental Juice Wrld"}, []string{"Rental"}, []string{"Rental"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := searchtest.MustUnreleased(service.NewUnreleasedService(&memory.GeniusSongCatalog{ArtistID: 7, ArtistFound: true, Songs: songs(tt.genius...)}, &searchtest.ArtistCatalogTitles{Titles: tt.catalog}, service.UnreleasedConfig{MaxPages: 1, MaxDetails: 50}, searchtest.UnreleasedWiring(t, service.NoUnreleasedCache{})))
			got, err := svc.List(context.Background(), domain.ProviderDeezer, "1", "Artist", nil)
			if err != nil {
				t.Fatalf("List: %v", err)
			}
			if !slices.Equal(titlesOf(got), tt.want) {
				t.Errorf("titles = %v, want %v", titlesOf(got), tt.want)
			}
		})
	}
}

func TestService_List_NoGeniusMatchIsAnEmptyList(t *testing.T) {
	svc := searchtest.MustUnreleased(service.NewUnreleasedService(&memory.GeniusSongCatalog{ArtistFound: false}, &searchtest.ArtistCatalogTitles{}, service.UnreleasedConfig{MaxPages: 1, MaxDetails: 50}, searchtest.UnreleasedWiring(t, service.NoUnreleasedCache{})))
	got, err := svc.List(context.Background(), domain.ProviderDeezer, "1", "Nobody", nil)
	if err != nil || got == nil || len(got) != 0 {
		t.Errorf("List = (%v, %v), want empty non-nil list and no error", got, err)
	}
}

func TestService_List_MarksMediaOnlyWithinTheDetailCap(t *testing.T) {
	genius := &memory.GeniusSongCatalog{
		ArtistFound: true,
		Songs:       songs("A", "B", "C"),
		MediaByID:   map[int]bool{1: true, 3: true},
	}
	svc := searchtest.MustUnreleased(service.NewUnreleasedService(genius, &searchtest.ArtistCatalogTitles{}, service.UnreleasedConfig{MaxPages: 1, MaxDetails: 2}, searchtest.UnreleasedWiring(t, service.NoUnreleasedCache{})))
	got, err := svc.List(context.Background(), domain.ProviderDeezer, "1", "Artist", nil)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("got %d songs, want 3", len(got))
	}
	if got[0].HasMedia == nil || !*got[0].HasMedia {
		t.Errorf("first song has_media = %v, want true", got[0].HasMedia)
	}
	if got[1].HasMedia == nil || *got[1].HasMedia {
		t.Errorf("second song has_media = %v, want false", got[1].HasMedia)
	}
	if got[2].HasMedia != nil {
		t.Errorf("third song unknown (past the cap): has_media = %v, want nil", *got[2].HasMedia)
	}
}

func TestService_List_CatalogFailureIsAnError(t *testing.T) {
	boom := errors.New("catalog down")
	svc := searchtest.MustUnreleased(service.NewUnreleasedService(&memory.GeniusSongCatalog{}, &searchtest.ArtistCatalogTitles{Err: boom}, service.UnreleasedConfig{MaxPages: 1}, searchtest.UnreleasedWiring(t, service.NoUnreleasedCache{})))
	if _, err := svc.List(context.Background(), domain.ProviderDeezer, "1", "Artist", nil); !errors.Is(err, boom) {
		t.Errorf("err = %v, want %v", err, boom)
	}
}

type memoryCache struct {
	stored map[string][]domain.UnreleasedSong
	ttls   map[string]time.Duration
}

func newMemoryCache() *memoryCache {
	return &memoryCache{stored: map[string][]domain.UnreleasedSong{}, ttls: map[string]time.Duration{}}
}

func (c *memoryCache) Get(_ context.Context, _ domain.ProviderName, id, nameKey string) ([]domain.UnreleasedSong, bool) {
	s, ok := c.stored[id+"|"+nameKey]
	return s, ok
}

func (c *memoryCache) Set(_ context.Context, _ domain.ProviderName, id, nameKey string, s []domain.UnreleasedSong, ttl time.Duration) error {
	c.stored[id+"|"+nameKey] = s
	c.ttls[id+"|"+nameKey] = ttl
	return nil
}

const (
	longTTL  = 168 * time.Hour
	shortTTL = time.Hour
)

var cachedConfig = service.UnreleasedConfig{MaxPages: 1, MaxDetails: 10, TTL: longTTL, EmptyTTL: shortTTL}

func TestService_List_SecondCallIsServedFromCache(t *testing.T) {
	genius := &memory.GeniusSongCatalog{ArtistID: 7, ArtistFound: true, Songs: songs("Leak")}
	svc := searchtest.MustUnreleased(service.NewUnreleasedService(genius, &searchtest.ArtistCatalogTitles{}, cachedConfig, searchtest.UnreleasedWiring(t, newMemoryCache())))
	for range 2 {
		if _, err := svc.List(context.Background(), domain.ProviderDeezer, "1", "Artist", nil); err != nil {
			t.Fatalf("List: %v", err)
		}
	}
	if len(genius.SearchedFor) != 1 {
		t.Errorf("genius searched %d times, want 1", len(genius.SearchedFor))
	}
}

func TestService_List_CacheIsKeyedOnTheNormalizedArtistName(t *testing.T) {
	genius := &memory.GeniusSongCatalog{ArtistID: 7, ArtistFound: true, Songs: songs("Leak")}
	svc := searchtest.MustUnreleased(service.NewUnreleasedService(genius, &searchtest.ArtistCatalogTitles{}, cachedConfig, searchtest.UnreleasedWiring(t, newMemoryCache())))
	for _, name := range []string{"Wrong Name", "Right Name", "  RIGHT name "} {
		if _, err := svc.List(context.Background(), domain.ProviderDeezer, "1", name, nil); err != nil {
			t.Fatalf("List(%q): %v", name, err)
		}
	}
	if want := []string{"Wrong Name", "Right Name"}; !slices.Equal(genius.SearchedFor, want) {
		t.Errorf("genius searched for %v, want %v (a different name must not reuse the entry, the same name in other case must)", genius.SearchedFor, want)
	}
}

func TestService_List_DropsTranslationPagesButKeepsRealUnreleasedMaterial(t *testing.T) {
	tests := []struct {
		name   string
		genius []string
		want   []string
	}{
		{"turkish translation is dropped", []string{"Secondhand ft. Rema (Türkçe Çeviri)"}, nil},
		{"english translation is dropped", []string{"Song [English Translation]"}, nil},
		{"spanish translation is dropped", []string{"Song (Traducción al Español)"}, nil},
		{"russian translation is dropped", []string{"Song (Русский перевод)"}, nil},
		{"demo is kept", []string{"Song (Demo)"}, []string{"Song (Demo)"}},
		{"original version is kept", []string{"Song (Original)"}, []string{"Song (Original)"}},
		{"cover is kept", []string{"Song (Cover)"}, []string{"Song (Cover)"}},
		{"marker outside brackets is kept", []string{"Translation"}, []string{"Translation"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := searchtest.MustUnreleased(service.NewUnreleasedService(&memory.GeniusSongCatalog{ArtistID: 7, ArtistFound: true, Songs: songs(tt.genius...)}, &searchtest.ArtistCatalogTitles{}, service.UnreleasedConfig{MaxPages: 1, MaxDetails: 50}, searchtest.UnreleasedWiring(t, service.NoUnreleasedCache{})))
			got, err := svc.List(context.Background(), domain.ProviderDeezer, "1", "Artist", nil)
			if err != nil {
				t.Fatalf("List: %v", err)
			}
			if !slices.Equal(titlesOf(got), tt.want) {
				t.Errorf("titles = %v, want %v", titlesOf(got), tt.want)
			}
		})
	}
}

func TestService_List_CacheTTLDependsOnHowTrustworthyTheAnswerIs(t *testing.T) {
	tests := []struct {
		name   string
		genius *memory.GeniusSongCatalog
		want   time.Duration
	}{
		{"complete answer gets the long ttl", &memory.GeniusSongCatalog{ArtistFound: true, Songs: songs("Leak")}, longTTL},
		{"empty answer gets the short ttl", &memory.GeniusSongCatalog{ArtistFound: false}, shortTTL},
		{"failed media lookup gets the short ttl", &memory.GeniusSongCatalog{ArtistFound: true, Songs: songs("Leak"), MediaErr: errors.New("429")}, shortTTL},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cache := newMemoryCache()
			svc := searchtest.MustUnreleased(service.NewUnreleasedService(tt.genius, &searchtest.ArtistCatalogTitles{}, cachedConfig, searchtest.UnreleasedWiring(t, cache)))
			if _, err := svc.List(context.Background(), domain.ProviderDeezer, "1", "Artist", nil); err != nil {
				t.Fatalf("List: %v", err)
			}
			if got := cache.ttls["1|artist"]; got != tt.want {
				t.Errorf("ttl = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestService_List_HidesSongsWithReleasedEvidenceFromGenius(t *testing.T) {
	tests := []struct {
		name    string
		title   string
		catalog []string
		detail  domain.GeniusSongDetail
		want    []string
	}{
		{"song on a catalog tracklist is hidden", "Deep Cut", []string{"Deep Cut"}, domain.GeniusSongDetail{}, nil},
		{"spotify link hides the song", "Leak", nil, domain.GeniusSongDetail{OnStreamingSite: true}, nil},
		{"genius album equal to a catalog album hides the song", "Leak", []string{"Heaven or Hell"}, domain.GeniusSongDetail{AlbumTitle: "HEAVEN OR HELL"}, nil},
		{"genius album unknown to the catalog keeps the song", "Leak", []string{"Heaven or Hell"}, domain.GeniusSongDetail{AlbumTitle: "Fan Compilation"}, []string{"Leak"}},
		{"unreleased album keeps the song even when the catalog has that name", "Leak", []string{"Unreleased"}, domain.GeniusSongDetail{AlbumTitle: "Unreleased"}, []string{"Leak"}},
		{"unreleased album variant keeps the song", "Leak", []string{"Unreleased Songs"}, domain.GeniusSongDetail{AlbumTitle: "Unreleased Songs"}, []string{"Leak"}},
		{"no evidence keeps the song", "Leak", nil, domain.GeniusSongDetail{HasMedia: true}, []string{"Leak"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			genius := &memory.GeniusSongCatalog{ArtistFound: true, Songs: songs(tt.title), DetailByID: map[int]domain.GeniusSongDetail{1: tt.detail}}
			svc := searchtest.MustUnreleased(service.NewUnreleasedService(genius, &searchtest.ArtistCatalogTitles{Titles: tt.catalog}, service.UnreleasedConfig{MaxPages: 1, MaxDetails: 10, DetailConcurrency: 2}, searchtest.UnreleasedWiring(t, service.NoUnreleasedCache{})))
			got, err := svc.List(context.Background(), domain.ProviderDeezer, "1", "Artist", nil)
			if err != nil {
				t.Fatalf("List: %v", err)
			}
			if !slices.Equal(titlesOf(got), tt.want) {
				t.Errorf("titles = %v, want %v", titlesOf(got), tt.want)
			}
		})
	}
}

func TestService_List_LooksUpDetailsOnlyForSongsTheCatalogDidNotHide(t *testing.T) {
	genius := &memory.GeniusSongCatalog{ArtistFound: true, Songs: songs("Released", "Leak")}
	svc := searchtest.MustUnreleased(service.NewUnreleasedService(genius, &searchtest.ArtistCatalogTitles{Titles: []string{"Released"}}, service.UnreleasedConfig{MaxPages: 1, MaxDetails: 10, DetailConcurrency: 2}, searchtest.UnreleasedWiring(t, service.NoUnreleasedCache{})))
	if _, err := svc.List(context.Background(), domain.ProviderDeezer, "1", "Artist", nil); err != nil {
		t.Fatalf("List: %v", err)
	}
	if got := genius.DetailLookups(); !slices.Equal(got, []int{2}) {
		t.Errorf("detail lookups = %v, want only the surviving song [2]", got)
	}
}

func TestService_List_PartialCatalogGetsTheShortTTLAndStillFilters(t *testing.T) {
	cache := newMemoryCache()
	genius := &memory.GeniusSongCatalog{ArtistFound: true, Songs: songs("Released", "Leak")}
	catalog := &searchtest.ArtistCatalogTitles{Titles: []string{"Released"}, Err: domain.ErrPartialCatalog}
	svc := searchtest.MustUnreleased(service.NewUnreleasedService(genius, catalog, cachedConfig, searchtest.UnreleasedWiring(t, cache)))
	got, err := svc.List(context.Background(), domain.ProviderDeezer, "1", "Artist", nil)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if !slices.Equal(titlesOf(got), []string{"Leak"}) {
		t.Errorf("titles = %v, want [Leak]", titlesOf(got))
	}
	if ttl := cache.ttls["1|artist"]; ttl != shortTTL {
		t.Errorf("ttl = %v, want %v", ttl, shortTTL)
	}
}

func TestService_List_HasMediaIsUnknownUntilLookedUp(t *testing.T) {
	genius := &memory.GeniusSongCatalog{
		ArtistFound: true,
		Songs:       songs("A", "B", "C"),
		MediaByID:   map[int]bool{1: true},
		DetailByID:  map[int]domain.GeniusSongDetail{},
	}
	svc := searchtest.MustUnreleased(service.NewUnreleasedService(genius, &searchtest.ArtistCatalogTitles{}, service.UnreleasedConfig{MaxPages: 1, MaxDetails: 2, DetailConcurrency: 2}, searchtest.UnreleasedWiring(t, service.NoUnreleasedCache{})))
	got, err := svc.List(context.Background(), domain.ProviderDeezer, "1", "Artist", nil)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("got %d songs, want 3", len(got))
	}
	if got[0].HasMedia == nil || !*got[0].HasMedia || got[1].HasMedia == nil || *got[1].HasMedia || got[2].HasMedia != nil {
		t.Errorf("has_media = %v %v %v, want true false nil (third song was past the cap)", got[0].HasMedia, got[1].HasMedia, got[2].HasMedia)
	}
}

func TestService_List_FailedDetailLookupLeavesHasMediaUnknown(t *testing.T) {
	genius := &memory.GeniusSongCatalog{ArtistFound: true, Songs: songs("A"), MediaErr: errors.New("429")}
	svc := searchtest.MustUnreleased(service.NewUnreleasedService(genius, &searchtest.ArtistCatalogTitles{}, service.UnreleasedConfig{MaxPages: 1, MaxDetails: 5, DetailConcurrency: 1}, searchtest.UnreleasedWiring(t, service.NoUnreleasedCache{})))
	got, err := svc.List(context.Background(), domain.ProviderDeezer, "1", "Artist", nil)
	if err != nil || len(got) != 1 || got[0].HasMedia != nil {
		t.Errorf("List = (%v, %v), want one song with unknown has_media", got, err)
	}
}

func TestService_List_ReleasedRemixesNeedTheExactRemixOnACatalogRelease(t *testing.T) {
	catalog := []string{"After Party", "After Party (CHOPNOTSLOP REMIX)", "No Idea - CHOPNOTSLOP REMIX", "Up & Down"}
	genius := []string{"After Party (CHOPNOTSLOP REMIX)", "After Party (Crooked Colours Remix)", "No Idea (CHOPNOTSLOP REMIX)", "Up & Down (Physical Version)", "After Party (Demo)"}
	svc := searchtest.MustUnreleased(service.NewUnreleasedService(&memory.GeniusSongCatalog{ArtistFound: true, Songs: songs(genius...)}, &searchtest.ArtistCatalogTitles{Titles: catalog}, service.UnreleasedConfig{MaxPages: 1, MaxDetails: 10, DetailConcurrency: 2}, searchtest.UnreleasedWiring(t, service.NoUnreleasedCache{})))
	got, err := svc.List(context.Background(), domain.ProviderDeezer, "1", "Artist", nil)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	want := []string{"After Party (Crooked Colours Remix)", "Up & Down (Physical Version)", "After Party (Demo)"}
	if !slices.Equal(titlesOf(got), want) {
		t.Errorf("titles = %v, want %v", titlesOf(got), want)
	}
}

type lockedCache struct {
	mu    sync.Mutex
	inner *memoryCache
}

func newLockedCache() *lockedCache { return &lockedCache{inner: newMemoryCache()} }

func (c *lockedCache) Get(ctx context.Context, p domain.ProviderName, id, nameKey string) ([]domain.UnreleasedSong, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.inner.Get(ctx, p, id, nameKey)
}

func (c *lockedCache) Set(ctx context.Context, p domain.ProviderName, id, nameKey string, s []domain.UnreleasedSong, ttl time.Duration) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.inner.Set(ctx, p, id, nameKey, s, ttl)
}

func (c *lockedCache) entries() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.inner.stored)
}

func backgroundService(t *testing.T, ctx context.Context, genius *memory.GeniusSongCatalog, cache *lockedCache, wait time.Duration) (*service.UnreleasedService, *background.Tracker) {
	var jobs background.Tracker
	cfg := service.UnreleasedConfig{MaxPages: 1, MaxDetails: 10, DetailConcurrency: 2, TTL: longTTL, EmptyTTL: shortTTL, Wait: wait}
	return searchtest.MustUnreleased(service.NewUnreleasedService(genius, &searchtest.ArtistCatalogTitles{}, cfg, searchtest.UnreleasedWiring(t, cache, searchtest.UnreleasedLifecycle(ctx, &jobs)))), &jobs
}

func TestService_Lookup_SlowJobIsPendingThenTheNextCallIsCached(t *testing.T) {
	gate := make(chan struct{})
	genius := &memory.GeniusSongCatalog{ArtistFound: true, Songs: songs("Leak"), Gate: gate}
	svc, jobs := backgroundService(t, context.Background(), genius, newLockedCache(), 20*time.Millisecond)

	first, err := svc.Lookup(context.Background(), domain.ProviderDeezer, "1", "Artist", nil)
	if err != nil || !first.Pending || first.Songs == nil || len(first.Songs) != 0 {
		t.Fatalf("first Lookup = (%+v, %v), want pending with an empty non-nil list", first, err)
	}
	close(gate)
	jobs.Wait()

	second, err := svc.Lookup(context.Background(), domain.ProviderDeezer, "1", "Artist", nil)
	if err != nil || second.Pending || !slices.Equal(titlesOf(second.Songs), []string{"Leak"}) {
		t.Errorf("second Lookup = (%+v, %v), want the cached item and not pending", second, err)
	}
	if len(genius.SearchedFor) != 1 {
		t.Errorf("genius searched %d times, want 1", len(genius.SearchedFor))
	}
}

func TestService_Lookup_ConcurrentCallsShareOneJob(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		gate := make(chan struct{})
		genius := &memory.GeniusSongCatalog{ArtistFound: true, Songs: songs("Leak"), Gate: gate}
		svc, jobs := backgroundService(t, context.Background(), genius, newLockedCache(), 0)

		const callers = 8
		results := make(chan service.Lookup, callers)
		for range callers {
			go func() {
				found, _ := svc.Lookup(context.Background(), domain.ProviderDeezer, "1", "Artist", nil)
				results <- found
			}()
		}
		synctest.Wait()
		close(gate)
		for range callers {
			if got := <-results; !slices.Equal(titlesOf(got.Songs), []string{"Leak"}) {
				t.Errorf("a caller got %v, want [Leak]", titlesOf(got.Songs))
			}
		}
		jobs.Wait()
		if len(genius.SearchedFor) != 1 {
			t.Errorf("genius searched %d times, want one job for the key", len(genius.SearchedFor))
		}
	})
}

func TestService_Lookup_FailedJobLeavesNoCacheEntry(t *testing.T) {
	boom := errors.New("genius down")
	genius := &memory.GeniusSongCatalog{ArtistFound: true, Err: boom}
	cache := newLockedCache()
	svc, jobs := backgroundService(t, context.Background(), genius, cache, 0)

	if _, err := svc.Lookup(context.Background(), domain.ProviderDeezer, "1", "Artist", nil); !errors.Is(err, boom) {
		t.Errorf("err = %v, want %v", err, boom)
	}
	jobs.Wait()
	if cache.entries() != 0 {
		t.Errorf("cache has %d entries after a failed job, want none", cache.entries())
	}
}

func TestService_Lookup_ServerShutdownStopsTheJob(t *testing.T) {
	lifecycle, shutdown := context.WithCancel(context.Background())
	genius := &memory.GeniusSongCatalog{ArtistFound: true, Gate: make(chan struct{})}
	svc, jobs := backgroundService(t, lifecycle, genius, newLockedCache(), 10*time.Millisecond)

	if _, err := svc.Lookup(context.Background(), domain.ProviderDeezer, "1", "Artist", nil); err != nil {
		t.Fatalf("Lookup: %v", err)
	}
	shutdown()
	stopped := make(chan struct{})
	go func() { jobs.Wait(); close(stopped) }()
	select {
	case <-stopped:
	case <-time.After(2 * time.Second):
		t.Fatal("the job outlived the lifecycle context")
	}
}

func TestService_List_SongWithoutOwnArtTakesItsAlbumCover(t *testing.T) {
	list := songs("A", "B")
	list[1].CoverURL = "https://images.genius.com/own.jpg"
	genius := &memory.GeniusSongCatalog{
		ArtistFound: true,
		Songs:       list,
		DetailByID: map[int]domain.GeniusSongDetail{
			1: {AlbumCoverURL: "https://images.genius.com/album.jpg"},
			2: {AlbumCoverURL: "https://images.genius.com/album.jpg"},
		},
	}
	svc := searchtest.MustUnreleased(service.NewUnreleasedService(genius, &searchtest.ArtistCatalogTitles{}, service.UnreleasedConfig{MaxPages: 1, MaxDetails: 2, DetailConcurrency: 2}, searchtest.UnreleasedWiring(t, service.NoUnreleasedCache{})))
	got, err := svc.List(context.Background(), domain.ProviderDeezer, "1", "Artist", nil)
	if err != nil || len(got) != 2 {
		t.Fatalf("List = (%d songs, %v), want 2 songs", len(got), err)
	}
	if got[0].CoverURL != "https://images.genius.com/album.jpg" || got[1].CoverURL != "https://images.genius.com/own.jpg" {
		t.Errorf("covers = %q %q, want album cover then own art", got[0].CoverURL, got[1].CoverURL)
	}
}

var drakeKey = ports.UnreleasedArtistKey{Provider: domain.ProviderDeezer, ExternalID: "27"}

func storeService(t *testing.T, genius *memory.GeniusSongCatalog, store *searchtest.UnreleasedStore) *service.UnreleasedService {
	cfg := service.UnreleasedConfig{MaxPages: 1, MaxDetails: 10, DetailConcurrency: 2, Wait: time.Second}
	return searchtest.MustUnreleased(service.NewUnreleasedService(genius, &searchtest.ArtistCatalogTitles{}, cfg, searchtest.UnreleasedWiring(t, service.NoUnreleasedCache{}, searchtest.UnreleasedStored(store))))
}

func writtenBuildVersion(t *testing.T) int {
	t.Helper()
	store := &searchtest.UnreleasedStore{Artists: map[ports.UnreleasedArtistKey]memory.StoredArtist{drakeKey: {}}}
	genius := &memory.GeniusSongCatalog{ArtistFound: true, Songs: songs("Leak")}
	if _, err := storeService(t, genius, store).Rebuild(context.Background(), drakeKey, "Drake", nil); err != nil {
		t.Fatalf("Rebuild: %v", err)
	}
	writes := store.Writes()
	if len(writes) != 1 || writes[0].BuildVersion <= 0 {
		t.Fatalf("writes = %+v, want one write stamped with a positive build version", writes)
	}
	return writes[0].BuildVersion
}

func TestService_Lookup_StoreAnswersABuiltArtistWithoutCallingGenius(t *testing.T) {
	store := &searchtest.UnreleasedStore{Artists: map[ports.UnreleasedArtistKey]memory.StoredArtist{
		drakeKey: {Songs: songs("A", "B", "C"), Complete: true, Built: true, BuildVersion: writtenBuildVersion(t)},
	}}
	genius := &memory.GeniusSongCatalog{ArtistFound: true, Songs: songs("Other")}
	got, err := storeService(t, genius, store).Lookup(context.Background(), domain.ProviderDeezer, "27", "Drake", nil)
	if err != nil || got.Pending || !slices.Equal(titlesOf(got.Songs), []string{"A", "B", "C"}) {
		t.Fatalf("Lookup = (%+v, %v), want the 3 stored songs, not pending", got, err)
	}
	if len(genius.SearchedFor) != 0 || len(genius.DetailLookups()) != 0 {
		t.Errorf("genius called: searched %v, details %v", genius.SearchedFor, genius.DetailLookups())
	}
}

func TestService_Lookup_StoreRecordsTheArtistBeforeBuildingAndWritesTheFinishedBuild(t *testing.T) {
	store := &searchtest.UnreleasedStore{}
	genius := &memory.GeniusSongCatalog{ArtistFound: true, Songs: songs("Leak")}
	before := time.Now()
	got, err := storeService(t, genius, store).Lookup(context.Background(), domain.ProviderDeezer, "27", "Drake", nil)
	if err != nil || !slices.Equal(titlesOf(got.Songs), []string{"Leak"}) {
		t.Fatalf("Lookup = (%+v, %v), want the built song", got, err)
	}
	if calls := store.Calls(); !slices.Equal(calls, []string{"record", "read", "write"}) {
		t.Fatalf("store calls = %v, want record, read, write", calls)
	}
	writes := store.Writes()
	if len(writes) != 1 || writes[0].Key != drakeKey || !writes[0].Complete || writes[0].StartedAt.Before(before) ||
		!slices.Equal(titlesOf(writes[0].Songs), []string{"Leak"}) {
		t.Errorf("writes = %+v, want one complete write of Leak for %v started after %v", writes, drakeKey, before)
	}
}

func TestService_Lookup_StoreRecordAndReadFailuresFallBackToALiveBuild(t *testing.T) {
	logs := captureLogs(t)
	store := &searchtest.UnreleasedStore{RecordErr: errors.New("record down"), ReadErr: errors.New("read down")}
	genius := &memory.GeniusSongCatalog{ArtistFound: true, Songs: songs("Leak")}
	got, err := storeService(t, genius, store).Lookup(context.Background(), domain.ProviderDeezer, "27", "Drake", nil)
	if err != nil || got.Pending || !slices.Equal(titlesOf(got.Songs), []string{"Leak"}) {
		t.Fatalf("Lookup = (%+v, %v), want the live-built song", got, err)
	}
	for _, event := range []string{"unreleased.store_record_failed", "unreleased.store_read_failed"} {
		if !strings.Contains(logs.String(), event) {
			t.Errorf("log missing %s: %s", event, logs.String())
		}
	}
}

func TestService_List_StoreAlwaysBuildsEvenWhenTheArtistIsBuilt(t *testing.T) {
	store := &searchtest.UnreleasedStore{Artists: map[ports.UnreleasedArtistKey]memory.StoredArtist{
		drakeKey: {Songs: songs("Old"), Complete: true, Built: true},
	}}
	genius := &memory.GeniusSongCatalog{ArtistFound: true, Songs: songs("New")}
	got, err := storeService(t, genius, store).List(context.Background(), domain.ProviderDeezer, "27", "Drake", nil)
	if err != nil || !slices.Equal(titlesOf(got), []string{"New"}) {
		t.Fatalf("List = (%v, %v), want the fresh build", titlesOf(got), err)
	}
	if calls := store.Calls(); !slices.Equal(calls, []string{"write"}) {
		t.Errorf("store calls = %v, want only the write", calls)
	}
}

func TestService_Rebuild_StoreWritesIncompleteWhenTheCatalogIsPartial(t *testing.T) {
	store := &searchtest.UnreleasedStore{Artists: map[ports.UnreleasedArtistKey]memory.StoredArtist{drakeKey: {}}}
	genius := &memory.GeniusSongCatalog{ArtistFound: true, Songs: songs("Leak")}
	catalog := &searchtest.ArtistCatalogTitles{Err: domain.ErrPartialCatalog}
	svc := searchtest.MustUnreleased(service.NewUnreleasedService(genius, catalog, service.UnreleasedConfig{MaxPages: 1, MaxDetails: 10}, searchtest.UnreleasedWiring(t, service.NoUnreleasedCache{}, searchtest.UnreleasedStored(store))))
	if _, err := svc.Rebuild(context.Background(), drakeKey, "Drake", nil); err != nil {
		t.Fatalf("Rebuild: %v", err)
	}
	if writes := store.Writes(); len(writes) != 1 || writes[0].Complete {
		t.Errorf("writes = %+v, want one write with complete false", writes)
	}
}

func TestService_Rebuild_StoreWriteFailureLogsAndStillReturnsTheSongs(t *testing.T) {
	logs := captureLogs(t)
	store := &searchtest.UnreleasedStore{WriteErr: errors.New("db down")}
	genius := &memory.GeniusSongCatalog{ArtistFound: true, Songs: songs("Leak")}
	got, err := storeService(t, genius, store).Rebuild(context.Background(), drakeKey, "Drake", nil)
	if err != nil || !slices.Equal(titlesOf(got), []string{"Leak"}) {
		t.Fatalf("Rebuild = (%v, %v), want the built song and no error", titlesOf(got), err)
	}
	if !strings.Contains(logs.String(), "unreleased.store_write_failed") {
		t.Errorf("log missing unreleased.store_write_failed: %s", logs.String())
	}
}

func TestService_Lookup_StoreWriteFailureStillAnswersTheBuiltSongs(t *testing.T) {
	store := &searchtest.UnreleasedStore{WriteErr: errors.New("db down")}
	genius := &memory.GeniusSongCatalog{ArtistFound: true, Songs: songs("Leak")}
	got, err := storeService(t, genius, store).Lookup(context.Background(), domain.ProviderDeezer, "27", "Drake", nil)
	if err != nil || !slices.Equal(titlesOf(got.Songs), []string{"Leak"}) {
		t.Errorf("Lookup = (%+v, %v), want the built song", got, err)
	}
}

func TestService_Lookup_BuildVersionOlderStoredArtistIsServedAtOnceAndRebuiltOnce(t *testing.T) {
	store := &searchtest.UnreleasedStore{Artists: map[ports.UnreleasedArtistKey]memory.StoredArtist{
		drakeKey: {Songs: songs("Old"), Complete: true, Built: true, BuildVersion: 0},
	}}
	genius := &memory.GeniusSongCatalog{ArtistFound: true, Songs: songs("New"), Gate: make(chan struct{})}
	var jobs background.Tracker
	cfg := service.UnreleasedConfig{MaxPages: 1, MaxDetails: 10, DetailConcurrency: 2, Wait: time.Second}
	svc := searchtest.MustUnreleased(service.NewUnreleasedService(genius, &searchtest.ArtistCatalogTitles{}, cfg, searchtest.UnreleasedWiring(t, service.NoUnreleasedCache{}, searchtest.UnreleasedStored(store), searchtest.UnreleasedLifecycle(context.Background(), &jobs))))
	for range 2 {
		got, err := svc.Lookup(context.Background(), domain.ProviderDeezer, "27", "Drake", nil)
		if err != nil || got.Pending || !slices.Equal(titlesOf(got.Songs), []string{"Old"}) {
			t.Fatalf("Lookup = (%+v, %v), want the stored song, not pending", got, err)
		}
	}
	close(genius.Gate)
	jobs.Wait()
	if len(genius.SearchedFor) != 1 {
		t.Errorf("rebuild jobs started = %d, want 1", len(genius.SearchedFor))
	}
	writes := store.Writes()
	current := writtenBuildVersion(t)
	if len(writes) != 1 || writes[0].BuildVersion != current || !slices.Equal(titlesOf(writes[0].Songs), []string{"New"}) {
		t.Errorf("writes = %+v, want one write of New at build version %d", writes, current)
	}
}

func TestService_Lookup_BuildVersionCurrentStoredArtistStartsNoRebuild(t *testing.T) {
	store := &searchtest.UnreleasedStore{Artists: map[ports.UnreleasedArtistKey]memory.StoredArtist{
		drakeKey: {Songs: songs("A"), Complete: true, Built: true, BuildVersion: writtenBuildVersion(t)},
	}}
	genius := &memory.GeniusSongCatalog{ArtistFound: true, Songs: songs("Other")}
	var jobs background.Tracker
	cfg := service.UnreleasedConfig{MaxPages: 1, MaxDetails: 10, DetailConcurrency: 2, Wait: time.Second}
	svc := searchtest.MustUnreleased(service.NewUnreleasedService(genius, &searchtest.ArtistCatalogTitles{}, cfg, searchtest.UnreleasedWiring(t, service.NoUnreleasedCache{}, searchtest.UnreleasedStored(store), searchtest.UnreleasedLifecycle(context.Background(), &jobs))))
	got, err := svc.Lookup(context.Background(), domain.ProviderDeezer, "27", "Drake", nil)
	jobs.Wait()
	if err != nil || !slices.Equal(titlesOf(got.Songs), []string{"A"}) || len(genius.SearchedFor) != 0 || len(store.Writes()) != 0 {
		t.Errorf("Lookup = (%+v, %v), searched %v, writes %v; want A and no rebuild", got, err, genius.SearchedFor, store.Writes())
	}
}

type panickingDetailCatalog struct {
	*memory.GeniusSongCatalog
}

func (panickingDetailCatalog) SongDetail(context.Context, int) (domain.GeniusSongDetail, error) {
	panic("genius parser exploded")
}

func TestService_List_PanickingDetailLookupIsAnIncompleteAnswerNotACrash(t *testing.T) {
	genius := panickingDetailCatalog{&memory.GeniusSongCatalog{ArtistFound: true, Songs: songs("Leak")}}
	cache := newMemoryCache()
	svc := searchtest.MustUnreleased(service.NewUnreleasedService(genius, &searchtest.ArtistCatalogTitles{}, cachedConfig, searchtest.UnreleasedWiring(t, cache)))

	got, err := svc.List(context.Background(), domain.ProviderDeezer, "1", "Artist", nil)

	if err != nil || len(got) != 1 || got[0].HasMedia != nil {
		t.Fatalf("List = (%v, %v), want the song kept with unknown has_media", got, err)
	}
	if ttl := cache.ttls["1|artist"]; ttl != shortTTL {
		t.Errorf("ttl = %v, want the short ttl of an incomplete answer", ttl)
	}
}

func TestNewUnreleasedService_RefusesAMissingCollaborator(t *testing.T) {
	genius := service.NoGenius{}
	catalog := &searchtest.ArtistCatalogTitles{}
	wiringWithout := func(drop func(*service.Wiring)) service.Wiring {
		w := searchtest.UnreleasedWiring(t, service.NoUnreleasedCache{})
		drop(&w)
		return w
	}
	cases := map[string]func() (*service.UnreleasedService, error){
		"genius": func() (*service.UnreleasedService, error) {
			return service.NewUnreleasedService(nil, catalog, service.UnreleasedConfig{}, searchtest.UnreleasedWiring(t, service.NoUnreleasedCache{}))
		},
		"catalog": func() (*service.UnreleasedService, error) {
			return service.NewUnreleasedService(genius, nil, service.UnreleasedConfig{}, searchtest.UnreleasedWiring(t, service.NoUnreleasedCache{}))
		},
		"trackers": func() (*service.UnreleasedService, error) {
			return service.NewUnreleasedService(genius, catalog, service.UnreleasedConfig{}, wiringWithout(func(w *service.Wiring) { w.Trackers = nil }))
		},
		"storage": func() (*service.UnreleasedService, error) {
			return service.NewUnreleasedService(genius, catalog, service.UnreleasedConfig{}, wiringWithout(func(w *service.Wiring) { w.Storage = nil }))
		},
		"backend": func() (*service.UnreleasedService, error) {
			return service.NewUnreleasedService(genius, catalog, service.UnreleasedConfig{}, wiringWithout(func(w *service.Wiring) { w.Backend = nil }))
		},
		"jobs": func() (*service.UnreleasedService, error) {
			return service.NewUnreleasedService(genius, catalog, service.UnreleasedConfig{}, wiringWithout(func(w *service.Wiring) { w.Jobs = nil }))
		},
		"now": func() (*service.UnreleasedService, error) {
			return service.NewUnreleasedService(genius, catalog, service.UnreleasedConfig{}, wiringWithout(func(w *service.Wiring) { w.Now = nil }))
		},
		"timer": func() (*service.UnreleasedService, error) {
			return service.NewUnreleasedService(genius, catalog, service.UnreleasedConfig{Wait: time.Second}, wiringWithout(func(w *service.Wiring) { w.Timer = nil }))
		},
	}
	for missing, build := range cases {
		t.Run(missing, func(t *testing.T) {
			svc, err := build()
			if !errors.Is(err, domain.ErrUnreleasedCollaboratorRequired) || svc != nil || !strings.Contains(err.Error(), missing) {
				t.Errorf("built (%v, %v), want a nil service and an error naming %s", svc, err, missing)
			}
		})
	}
}

func TestUnreleasedStorage_RefusesAMissingBackend(t *testing.T) {
	cached, cachedErr := service.NewCachedStorage(nil)
	stored, storedErr := service.NewStoredStorage(nil)

	if cached != nil || !errors.Is(cachedErr, domain.ErrUnreleasedCollaboratorRequired) {
		t.Errorf("cached storage = (%v, %v), want a refusal", cached, cachedErr)
	}
	if stored != nil || !errors.Is(storedErr, domain.ErrUnreleasedCollaboratorRequired) {
		t.Errorf("stored storage = (%v, %v), want a refusal", stored, storedErr)
	}
}

func TestUnreleasedService_ANullGeniusIsASourceThatIsNotConfigured(t *testing.T) {
	svc := searchtest.MustUnreleased(service.NewUnreleasedService(service.NoGenius{}, &searchtest.ArtistCatalogTitles{}, service.UnreleasedConfig{}, searchtest.UnreleasedWiring(t, service.NoUnreleasedCache{})))

	songs, err := svc.List(context.Background(), domain.ProviderDeezer, "1", "Artist", nil)

	if err != nil || len(songs) != 0 {
		t.Errorf("List = %v, %v; want no songs and no error, the same as a source that was never configured", songs, err)
	}
}

type jobContextKey struct{}

type contextProbeGenius struct {
	seen    chan context.Context
	release chan struct{}
}

func (g *contextProbeGenius) FindArtistID(ctx context.Context, _ string, _ []string) (int, bool, error) {
	g.seen <- ctx
	select {
	case <-g.release:
	case <-ctx.Done():
	}
	return 0, false, nil
}

func (g *contextProbeGenius) ArtistSongs(context.Context, int, int) ([]domain.UnreleasedSong, error) {
	return nil, nil
}

func (g *contextProbeGenius) SongDetail(context.Context, int) (domain.GeniusSongDetail, error) {
	return domain.GeniusSongDetail{}, nil
}

func TestService_Lookup_JobKeepsTheRequestValuesButOutlivesTheRequest(t *testing.T) {
	genius := &contextProbeGenius{seen: make(chan context.Context, 1), release: make(chan struct{})}
	lifecycle, stopLifecycle := context.WithCancel(t.Context())
	defer stopLifecycle()
	var jobs background.Tracker
	cfg := service.UnreleasedConfig{MaxPages: 1, MaxDetails: 10, DetailConcurrency: 2, TTL: longTTL, EmptyTTL: shortTTL}
	svc := searchtest.MustUnreleased(service.NewUnreleasedService(genius, &searchtest.ArtistCatalogTitles{}, cfg,
		searchtest.UnreleasedWiring(t, newLockedCache(), searchtest.UnreleasedLifecycle(lifecycle, &jobs))))
	request, cancelRequest := context.WithCancel(context.WithValue(t.Context(), jobContextKey{}, "correlation-1"))
	lookedUp := make(chan error, 1)
	go func() {
		_, err := svc.Lookup(request, domain.ProviderDeezer, "1", "Artist", nil)
		lookedUp <- err
	}()

	jobCtx := <-genius.seen
	cancelRequest()
	if err := <-lookedUp; err == nil {
		t.Fatal("Lookup returned no error after its request was cancelled")
	}

	if got := jobCtx.Value(jobContextKey{}); got != "correlation-1" {
		t.Errorf("job context value = %v, want the request's correlation value", got)
	}
	if err := jobCtx.Err(); err != nil {
		t.Errorf("job context ended with %v when only the request was cancelled", err)
	}
	stopLifecycle()
	<-jobCtx.Done()
	jobs.Wait()
}

type startRecordingStore struct {
	*memory.UnreleasedStore
	started time.Time
}

func (s *startRecordingStore) Write(ctx context.Context, key ports.UnreleasedArtistKey, songs []domain.UnreleasedSong, complete bool, startedAt time.Time, version int) error {
	s.started = startedAt
	return s.UnreleasedStore.Write(ctx, key, songs, complete, startedAt, version)
}

func TestService_Rebuild_StampsTheBuildStartFromTheInjectedClock(t *testing.T) {
	start := time.Date(2026, time.March, 4, 10, 0, 0, 0, time.UTC)
	manual := clocktest.NewManual(start)
	store := &startRecordingStore{UnreleasedStore: &memory.UnreleasedStore{}}
	genius := &memory.GeniusSongCatalog{ArtistFound: true, Songs: songs("Leak")}
	wiring := searchtest.UnreleasedWiring(t, service.NoUnreleasedCache{}, searchtest.UnreleasedStored(store),
		func(w *service.Wiring) { w.Now = manual.Func() })
	svc := searchtest.MustUnreleased(service.NewUnreleasedService(genius, &searchtest.ArtistCatalogTitles{}, service.UnreleasedConfig{MaxPages: 1, MaxDetails: 10, DetailConcurrency: 2}, wiring))

	if _, err := svc.Rebuild(t.Context(), drakeKey, "Drake", nil); err != nil {
		t.Fatalf("Rebuild: %v", err)
	}

	if !store.started.Equal(start) {
		t.Errorf("build start written = %v, want %v from the injected clock", store.started, start)
	}
}
