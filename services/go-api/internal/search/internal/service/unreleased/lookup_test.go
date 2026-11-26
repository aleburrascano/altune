package unreleased_test

import (
	"altune/go-api/internal/search"
	"altune/go-api/internal/search/searchtest"
	"altune/go-api/internal/shared/background"
	"context"
	"errors"
	"slices"
	"testing"
)

func TestService_Warm_RecordsEachArtistOnce(t *testing.T) {
	store := &search.FakeUnreleasedStore{}
	var jobs background.Tracker
	svc := searchtest.MustUnreleased(search.NewUnreleasedService(search.NoUnreleasedGenius{}, &search.ArtistCatalogTitles{}, search.UnreleasedConfig{}, searchtest.UnreleasedWiring(search.NoUnreleasedCache{}, searchtest.UnreleasedStored(store), searchtest.UnreleasedLifecycle(context.Background(), &jobs))))

	svc.Warm([]search.ArtistToRefresh{
		{Provider: search.ProviderDeezer, ExternalID: "27", Name: "Drake"},
		{Provider: search.ProviderDeezer, ExternalID: "27", Name: "Drake"},
		{Provider: search.ProviderDeezer, ExternalID: "5", Name: "  "},
		{Provider: search.ProviderDeezer, ExternalID: "9", Name: "Yeat"},
	})
	jobs.Wait()

	if got := store.Calls(); !slices.Equal(got, []string{"record", "record"}) {
		t.Errorf("store calls = %v, want one record for Drake and one for Yeat", got)
	}
}

func TestService_Warm_StoreErrorIsOnlyLogged(t *testing.T) {
	store := &search.FakeUnreleasedStore{RecordErr: errors.New("db down")}
	var jobs background.Tracker
	svc := searchtest.MustUnreleased(search.NewUnreleasedService(search.NoUnreleasedGenius{}, &search.ArtistCatalogTitles{}, search.UnreleasedConfig{}, searchtest.UnreleasedWiring(search.NoUnreleasedCache{}, searchtest.UnreleasedStored(store), searchtest.UnreleasedLifecycle(context.Background(), &jobs))))

	svc.Warm([]search.ArtistToRefresh{{Provider: search.ProviderDeezer, ExternalID: "27", Name: "Drake"}})
	jobs.Wait()

	if got := store.Calls(); len(got) != 1 {
		t.Errorf("store calls = %v, want the one attempted record", got)
	}
}
