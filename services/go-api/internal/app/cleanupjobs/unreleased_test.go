package cleanupjobs_test

import (
	"altune/go-api/internal/app/cleanupjobs"
	"altune/go-api/internal/search"
	"altune/go-api/internal/shared/background"
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

func storedUnreleased(t *testing.T, store search.UnreleasedStore) *search.UnreleasedService {
	t.Helper()
	storage, err := search.NewUnreleasedStoredStorage(store)
	if err != nil {
		t.Fatalf("NewUnreleasedStoredStorage: %v", err)
	}
	svc, err := search.NewUnreleasedService(search.NoUnreleasedGenius{}, &search.ArtistCatalogTitles{}, search.UnreleasedConfig{}, search.UnreleasedWiring{
		Trackers:  search.NoUnreleasedTrackers{},
		Storage:   storage,
		Lifecycle: context.Background(),
		Jobs:      &background.Tracker{},
	})
	if err != nil {
		t.Fatalf("NewUnreleasedService: %v", err)
	}
	return svc
}

func TestRefreshDueUnreleased_AsksForWhatIsDueAtTheClocksInstantInUTC(t *testing.T) {
	store := &limitRecordingStore{}
	svc := storedUnreleased(t, store)

	if err := cleanupjobs.RefreshDueUnreleased(svc, atTestNow)(context.Background()); err != nil {
		t.Fatalf("run: %v", err)
	}

	if len(store.asOf) != 1 || !store.asOf[0].Equal(testNow) || store.asOf[0].Location() != time.UTC {
		t.Errorf("Due was asked as of %v, want %v in UTC", store.asOf, testNow.UTC())
	}
}

func TestRefreshDueUnreleased_EachRunAsksTheStoreForAtMostTwoArtists(t *testing.T) {
	store := &limitRecordingStore{}
	svc := storedUnreleased(t, store)
	if err := cleanupjobs.RefreshDueUnreleased(svc, atTestNow)(context.Background()); err != nil {
		t.Fatalf("run: %v", err)
	}
	if !slices.Equal(store.limits, []int{2}) {
		t.Errorf("Due limits = %v, want [2]", store.limits)
	}
}
