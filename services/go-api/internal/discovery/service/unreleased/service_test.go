package unreleased

import (
	"altune/go-api/internal/discovery/discoverytest"
	"altune/go-api/internal/discovery/domain"
	"context"
	"errors"
	"slices"
	"testing"
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
			svc := NewService(
				&discoverytest.GeniusSongCatalog{ArtistID: 7, ArtistFound: true, Songs: songs(tt.genius...)},
				&discoverytest.ArtistCatalogTitles{Titles: tt.catalog},
				nil,
				Config{MaxPages: 1, MaxDetails: 50},
			)
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
	svc := NewService(
		&discoverytest.GeniusSongCatalog{ArtistFound: false},
		&discoverytest.ArtistCatalogTitles{},
		nil,
		Config{MaxPages: 1, MaxDetails: 50},
	)
	got, err := svc.List(context.Background(), domain.ProviderDeezer, "1", "Nobody", nil)
	if err != nil || got == nil || len(got) != 0 {
		t.Errorf("List = (%v, %v), want empty non-nil list and no error", got, err)
	}
}

func TestService_List_MarksMediaOnlyWithinTheDetailCap(t *testing.T) {
	genius := &discoverytest.GeniusSongCatalog{
		ArtistFound: true,
		Songs:       songs("A", "B", "C"),
		MediaByID:   map[int]bool{1: true, 3: true},
	}
	svc := NewService(genius, &discoverytest.ArtistCatalogTitles{}, nil, Config{MaxPages: 1, MaxDetails: 2})
	got, err := svc.List(context.Background(), domain.ProviderDeezer, "1", "Artist", nil)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("got %d songs, want 3", len(got))
	}
	if !got[0].HasMedia || got[1].HasMedia || got[2].HasMedia {
		t.Errorf("has_media = %v %v %v, want true false false (third song is past the cap)", got[0].HasMedia, got[1].HasMedia, got[2].HasMedia)
	}
}

func TestService_List_CatalogFailureIsAnError(t *testing.T) {
	boom := errors.New("catalog down")
	svc := NewService(&discoverytest.GeniusSongCatalog{}, &discoverytest.ArtistCatalogTitles{Err: boom}, nil, Config{MaxPages: 1})
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

var cachedConfig = Config{MaxPages: 1, MaxDetails: 10, TTL: longTTL, EmptyTTL: shortTTL}

func TestService_List_SecondCallIsServedFromCache(t *testing.T) {
	genius := &discoverytest.GeniusSongCatalog{ArtistID: 7, ArtistFound: true, Songs: songs("Leak")}
	svc := NewService(genius, &discoverytest.ArtistCatalogTitles{}, newMemoryCache(), cachedConfig)
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
	genius := &discoverytest.GeniusSongCatalog{ArtistID: 7, ArtistFound: true, Songs: songs("Leak")}
	svc := NewService(genius, &discoverytest.ArtistCatalogTitles{}, newMemoryCache(), cachedConfig)
	for _, name := range []string{"Wrong Name", "Right Name", "  RIGHT name "} {
		if _, err := svc.List(context.Background(), domain.ProviderDeezer, "1", name, nil); err != nil {
			t.Fatalf("List(%q): %v", name, err)
		}
	}
	if want := []string{"Wrong Name", "Right Name"}; !slices.Equal(genius.SearchedFor, want) {
		t.Errorf("genius searched for %v, want %v (a different name must not reuse the entry, the same name in other case must)", genius.SearchedFor, want)
	}
}

func TestService_List_CacheTTLDependsOnHowTrustworthyTheAnswerIs(t *testing.T) {
	tests := []struct {
		name   string
		genius *discoverytest.GeniusSongCatalog
		want   time.Duration
	}{
		{"complete answer gets the long ttl", &discoverytest.GeniusSongCatalog{ArtistFound: true, Songs: songs("Leak")}, longTTL},
		{"empty answer gets the short ttl", &discoverytest.GeniusSongCatalog{ArtistFound: false}, shortTTL},
		{"failed media lookup gets the short ttl", &discoverytest.GeniusSongCatalog{ArtistFound: true, Songs: songs("Leak"), MediaErr: errors.New("429")}, shortTTL},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cache := newMemoryCache()
			svc := NewService(tt.genius, &discoverytest.ArtistCatalogTitles{}, cache, cachedConfig)
			if _, err := svc.List(context.Background(), domain.ProviderDeezer, "1", "Artist", nil); err != nil {
				t.Fatalf("List: %v", err)
			}
			if got := cache.ttls["1|artist"]; got != tt.want {
				t.Errorf("ttl = %v, want %v", got, tt.want)
			}
		})
	}
}
