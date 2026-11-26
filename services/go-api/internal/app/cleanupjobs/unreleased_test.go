package cleanupjobs_test

import (
	"altune/go-api/internal/app/cleanupjobs"
	"altune/go-api/internal/search"
	"altune/go-api/internal/search/searchtest"
	"context"
	"slices"
	"testing"
	"time"
)

type limitRecordingStore struct {
	search.FakeUnreleasedStore
	limits []int
	asOf   []time.Time
}

func (l *limitRecordingStore) Due(_ context.Context, now time.Time, limit int) ([]search.UnreleasedDueArtist, error) {
	l.limits = append(l.limits, limit)
	l.asOf = append(l.asOf, now)
	return nil, nil
}

func TestRefreshDueUnreleased_AsksForWhatIsDueAtTheClocksInstantInUTC(t *testing.T) {
	store := &limitRecordingStore{}
	svc := searchtest.MustUnreleased(search.NewUnreleasedService(search.NoUnreleasedGenius{}, &search.ArtistCatalogTitles{}, search.UnreleasedConfig{}, searchtest.UnreleasedWiring(search.NoUnreleasedCache{}, searchtest.UnreleasedStored(store))))

	if err := cleanupjobs.RefreshDueUnreleased(svc, atTestNow)(context.Background()); err != nil {
		t.Fatalf("run: %v", err)
	}

	if len(store.asOf) != 1 || !store.asOf[0].Equal(testNow) || store.asOf[0].Location() != time.UTC {
		t.Errorf("Due was asked as of %v, want %v in UTC", store.asOf, testNow.UTC())
	}
}

func TestRefreshDueUnreleased_EachRunAsksTheStoreForAtMostTwoArtists(t *testing.T) {
	store := &limitRecordingStore{}
	svc := searchtest.MustUnreleased(search.NewUnreleasedService(search.NoUnreleasedGenius{}, &search.ArtistCatalogTitles{}, search.UnreleasedConfig{}, searchtest.UnreleasedWiring(search.NoUnreleasedCache{}, searchtest.UnreleasedStored(store))))
	if err := cleanupjobs.RefreshDueUnreleased(svc, atTestNow)(context.Background()); err != nil {
		t.Fatalf("run: %v", err)
	}
	if !slices.Equal(store.limits, []int{2}) {
		t.Errorf("Due limits = %v, want [2]", store.limits)
	}
}
