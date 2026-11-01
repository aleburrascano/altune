package unreleased

import (
	"altune/go-api/internal/discovery/discoverytest"
	"altune/go-api/internal/discovery/domain"
	"context"
	"errors"
	"slices"
	"sync"
	"testing"
)

func TestService_Warm_RecordsEachArtistOnce(t *testing.T) {
	store := &discoverytest.UnreleasedStore{}
	var jobs sync.WaitGroup
	svc := NewService(nil, &discoverytest.ArtistCatalogTitles{}, nil, Config{}, WithStore(store), WithLifecycle(context.Background(), &jobs))

	svc.Warm([]WarmArtist{
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
	store := &discoverytest.UnreleasedStore{RecordErr: errors.New("db down")}
	var jobs sync.WaitGroup
	svc := NewService(nil, &discoverytest.ArtistCatalogTitles{}, nil, Config{}, WithStore(store), WithLifecycle(context.Background(), &jobs))

	svc.Warm([]WarmArtist{{Provider: domain.ProviderDeezer, ExternalID: "27", Name: "Drake"}})
	jobs.Wait()

	if got := store.Calls(); len(got) != 1 {
		t.Errorf("store calls = %v, want the one attempted record", got)
	}
}
