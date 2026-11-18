package unreleased_test

import (
	"altune/go-api/internal/discovery"
	"context"
	"errors"
	"slices"
	"sync"
	"testing"
)

func TestService_Warm_RecordsEachArtistOnce(t *testing.T) {
	store := &discovery.FakeUnreleasedStore{}
	var jobs sync.WaitGroup
	svc := discovery.NewUnreleasedService(nil, &discovery.ArtistCatalogTitles{}, nil, discovery.UnreleasedConfig{}, discovery.WithUnreleasedStore(store), discovery.WithUnreleasedLifecycle(context.Background(), &jobs))

	svc.Warm([]discovery.WarmArtist{
		{Provider: discovery.ProviderDeezer, ExternalID: "27", Name: "Drake"},
		{Provider: discovery.ProviderDeezer, ExternalID: "27", Name: "Drake"},
		{Provider: discovery.ProviderDeezer, ExternalID: "5", Name: "  "},
		{Provider: discovery.ProviderDeezer, ExternalID: "9", Name: "Yeat"},
	})
	jobs.Wait()

	if got := store.Calls(); !slices.Equal(got, []string{"record", "record"}) {
		t.Errorf("store calls = %v, want one record for Drake and one for Yeat", got)
	}
}

func TestService_Warm_StoreErrorIsOnlyLogged(t *testing.T) {
	store := &discovery.FakeUnreleasedStore{RecordErr: errors.New("db down")}
	var jobs sync.WaitGroup
	svc := discovery.NewUnreleasedService(nil, &discovery.ArtistCatalogTitles{}, nil, discovery.UnreleasedConfig{}, discovery.WithUnreleasedStore(store), discovery.WithUnreleasedLifecycle(context.Background(), &jobs))

	svc.Warm([]discovery.WarmArtist{{Provider: discovery.ProviderDeezer, ExternalID: "27", Name: "Drake"}})
	jobs.Wait()

	if got := store.Calls(); len(got) != 1 {
		t.Errorf("store calls = %v, want the one attempted record", got)
	}
}
