package unreleased

import (
	"altune/go-api/internal/discovery/discoverytest"
	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/discovery/ports"
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"
)

type dueStore struct {
	discoverytest.UnreleasedStore
	due      []ports.UnreleasedDueArtist
	err      error
	gotLimit int
	gotNow   time.Time
}

func (d *dueStore) Due(_ context.Context, now time.Time, limit int) ([]ports.UnreleasedDueArtist, error) {
	d.gotNow, d.gotLimit = now, limit
	return d.due, d.err
}

type failingForName struct {
	discoverytest.GeniusSongCatalog
	failName string
}

func (f *failingForName) FindArtistID(ctx context.Context, name string, known []string) (int, bool, error) {
	id, found, err := f.GeniusSongCatalog.FindArtistID(ctx, name, known)
	if name == f.failName {
		return 0, false, errors.New("genius down")
	}
	return id, found, err
}

func dueArtist(id, name string) ports.UnreleasedDueArtist {
	return ports.UnreleasedDueArtist{Key: ports.UnreleasedArtistKey{Provider: domain.ProviderDeezer, ExternalID: id}, Name: name}
}

func refreshService(genius ports.GeniusSongCatalog, store *dueStore) *Service {
	cfg := Config{MaxPages: 1, MaxDetails: 10, DetailConcurrency: 2, Wait: time.Second}
	return NewService(genius, &discoverytest.ArtistCatalogTitles{}, nil, cfg, WithStore(store))
}

func TestService_RefreshDue_RebuildsExactlyTheArtistsDueReturns(t *testing.T) {
	store := &dueStore{due: []ports.UnreleasedDueArtist{dueArtist("1", "Drake"), dueArtist("2", "Future")}}
	genius := &discoverytest.GeniusSongCatalog{ArtistFound: true, Songs: songs("Leak")}
	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	if err := refreshService(genius, store).RefreshDue(context.Background(), now, 2); err != nil {
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
	store := &dueStore{due: []ports.UnreleasedDueArtist{dueArtist("1", "Drake"), dueArtist("2", "Future")}}
	genius := &failingForName{GeniusSongCatalog: discoverytest.GeniusSongCatalog{ArtistFound: true, Songs: songs("Leak")}, failName: "Drake"}
	if err := refreshService(genius, store).RefreshDue(context.Background(), time.Now(), 2); err != nil {
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
	store := &dueStore{err: errors.New("db down"), due: []ports.UnreleasedDueArtist{dueArtist("1", "Drake")}}
	genius := &discoverytest.GeniusSongCatalog{ArtistFound: true}
	if err := refreshService(genius, store).RefreshDue(context.Background(), time.Now(), 2); err == nil {
		t.Fatal("RefreshDue returned nil, want the Due error")
	}
	if len(genius.SearchedFor) != 0 {
		t.Errorf("searched %v, want no builds", genius.SearchedFor)
	}
}
