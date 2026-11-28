package service_test

import (
	"altune/go-api/internal/search/internal/domain"
	"altune/go-api/internal/search/internal/service"
	"altune/go-api/internal/search/searchtest"
	"altune/go-api/internal/shared/background"
	"context"
	"errors"
	"slices"
	"testing"
)

func TestService_Warm_RecordsEachArtistOnce(t *testing.T) {
	store := &searchtest.UnreleasedStore{}
	var jobs background.Tracker
	svc := searchtest.MustUnreleased(service.NewUnreleasedService(service.NoGenius{}, &searchtest.ArtistCatalogTitles{}, service.UnreleasedConfig{}, searchtest.UnreleasedWiring(t, service.NoUnreleasedCache{}, searchtest.UnreleasedStored(store), searchtest.UnreleasedLifecycle(context.Background(), &jobs))))

	svc.Warm(t.Context(), []service.ArtistToRefresh{
		{Provider: domain.ProviderDeezer, ExternalID: "27", Name: "Drake"},
		{Provider: domain.ProviderDeezer, ExternalID: "27", Name: "Drake"},
		{Provider: domain.ProviderDeezer, ExternalID: "5", Name: "  "},
		{Provider: domain.ProviderDeezer, ExternalID: "9", Name: "Yeat"},
	})
	jobs.Wait()

	if got := store.Calls(); !slices.Equal(got, []string{"record", "record"}) {
		t.Errorf("store calls = %v, want one record for Drake and one for Yeat", got)
	}
}

func TestService_Warm_StoreErrorIsOnlyLogged(t *testing.T) {
	store := &searchtest.UnreleasedStore{RecordErr: errors.New("db down")}
	var jobs background.Tracker
	svc := searchtest.MustUnreleased(service.NewUnreleasedService(service.NoGenius{}, &searchtest.ArtistCatalogTitles{}, service.UnreleasedConfig{}, searchtest.UnreleasedWiring(t, service.NoUnreleasedCache{}, searchtest.UnreleasedStored(store), searchtest.UnreleasedLifecycle(context.Background(), &jobs))))

	svc.Warm(t.Context(), []service.ArtistToRefresh{{Provider: domain.ProviderDeezer, ExternalID: "27", Name: "Drake"}})
	jobs.Wait()

	if got := store.Calls(); len(got) != 1 {
		t.Errorf("store calls = %v, want the one attempted record", got)
	}
}
