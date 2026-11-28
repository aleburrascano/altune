package service_test

import (
	"altune/go-api/internal/search"
	"altune/go-api/internal/search/internal/adapters/memory"
	"altune/go-api/internal/search/internal/ports"
	"altune/go-api/internal/search/searchtest"
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"
)

type dueStore struct {
	searchtest.UnreleasedStore
	due      []search.UnreleasedDueArtist
	err      error
	gotLimit int
	gotNow   time.Time
}

func (d *dueStore) Due(_ context.Context, now time.Time, limit int) ([]search.UnreleasedDueArtist, error) {
	d.gotNow, d.gotLimit = now, limit
	return d.due, d.err
}

type failingForName struct {
	memory.GeniusSongCatalog
	failName string
}

func (f *failingForName) FindArtistID(ctx context.Context, name string, known []string) (int, bool, error) {
	id, found, err := f.GeniusSongCatalog.FindArtistID(ctx, name, known)
	if name == f.failName {
		return 0, false, errors.New("genius down")
	}
	return id, found, err
}

func dueArtist(id, name string) search.UnreleasedDueArtist {
	return search.UnreleasedDueArtist{Key: ports.UnreleasedArtistKey{Provider: search.ProviderDeezer, ExternalID: id}, Name: name}
}

func recordedDueStore(due ...search.UnreleasedDueArtist) *dueStore {
	store := &dueStore{due: due}
	store.Artists = map[ports.UnreleasedArtistKey]memory.StoredArtist{}
	for _, d := range due {
		store.Artists[d.Key] = memory.StoredArtist{}
	}
	return store
}

func refreshService(t *testing.T, genius ports.GeniusSongCatalog, store *dueStore) *search.UnreleasedService {
	cfg := search.UnreleasedConfig{MaxPages: 1, MaxDetails: 10, DetailConcurrency: 2, Wait: time.Second}
	return searchtest.MustUnreleased(search.NewUnreleasedService(genius, &searchtest.ArtistCatalogTitles{}, cfg, searchtest.UnreleasedWiring(t, search.NoUnreleasedCache{}, searchtest.UnreleasedStored(store))))
}

func TestService_RefreshDue_RebuildsExactlyTheArtistsDueReturns(t *testing.T) {
	store := recordedDueStore(dueArtist("1", "Drake"), dueArtist("2", "Future"))
	genius := &memory.GeniusSongCatalog{ArtistFound: true, Songs: songs("Leak")}
	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	if err := refreshService(t, genius, store).RefreshDue(context.Background(), now, 2); err != nil {
		t.Fatalf("RefreshDue: %v", err)
	}
	if store.gotLimit != 2 || !store.gotNow.Equal(now) {
		t.Errorf("Due called with (%v, %d), want (%v, 2)", store.gotNow, store.gotLimit, now)
	}
	if !slices.Equal(genius.SearchedFor, []string{"Drake", "Future"}) {
		t.Errorf("rebuilt %v, want Drake then Future", genius.SearchedFor)
	}
	if got := len(store.Writes()); got != 2 {
		t.Errorf("writes = %d, want 2", got)
	}
}

func TestService_RefreshDue_AFailedRebuildDoesNotStopTheNextArtist(t *testing.T) {
	logs := captureLogs(t)
	store := recordedDueStore(dueArtist("1", "Drake"), dueArtist("2", "Future"))
	genius := &failingForName{GeniusSongCatalog: memory.GeniusSongCatalog{ArtistFound: true, Songs: songs("Leak")}, failName: "Drake"}
	if err := refreshService(t, genius, store).RefreshDue(context.Background(), time.Now(), 2); err != nil {
		t.Fatalf("RefreshDue: %v", err)
	}
	writes := store.Writes()
	if len(writes) != 1 || writes[0].Key.ExternalID != "2" {
		t.Errorf("writes = %+v, want only Future's", writes)
	}
	if !strings.Contains(logs.String(), "unreleased.refresh_build_failed") {
		t.Errorf("log missing unreleased.refresh_build_failed: %s", logs.String())
	}
}

func TestService_RefreshDue_ADueQueryFailureIsReturnedAndBuildsNothing(t *testing.T) {
	store := &dueStore{err: errors.New("db down"), due: []search.UnreleasedDueArtist{dueArtist("1", "Drake")}}
	genius := &memory.GeniusSongCatalog{ArtistFound: true}
	if err := refreshService(t, genius, store).RefreshDue(context.Background(), time.Now(), 2); err == nil {
		t.Fatal("RefreshDue returned nil, want the Due error")
	}
	if len(genius.SearchedFor) != 0 {
		t.Errorf("searched %v, want no builds", genius.SearchedFor)
	}
}

func TestService_RefreshDue_NeverRebuildsMoreThanTheLimitEvenIfTheStoreReturnsMore(t *testing.T) {
	store := &dueStore{due: []search.UnreleasedDueArtist{dueArtist("1", "Drake"), dueArtist("2", "Future"), dueArtist("3", "Kanye")}}
	genius := &memory.GeniusSongCatalog{ArtistFound: true, Songs: songs("Leak")}
	if err := refreshService(t, genius, store).RefreshDue(context.Background(), time.Now(), 2); err != nil {
		t.Fatalf("RefreshDue: %v", err)
	}
	if !slices.Equal(genius.SearchedFor, []string{"Drake", "Future"}) {
		t.Errorf("rebuilt %v, want only the first 2", genius.SearchedFor)
	}
}
