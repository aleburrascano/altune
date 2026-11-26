package unreleased_test

import (
	"altune/go-api/internal/search"
	"altune/go-api/internal/search/searchtest"
	"context"
	"errors"
	"strings"
	"testing"
)

func TestNewUnreleasedService_RefusesAMissingCollaborator(t *testing.T) {
	genius := search.NoUnreleasedGenius{}
	catalog := &search.ArtistCatalogTitles{}
	cases := map[string]func() (*search.UnreleasedService, error){
		"genius": func() (*search.UnreleasedService, error) {
			return search.NewUnreleasedService(nil, catalog, search.NoUnreleasedCache{}, search.UnreleasedConfig{})
		},
		"catalog": func() (*search.UnreleasedService, error) {
			return search.NewUnreleasedService(genius, nil, search.NoUnreleasedCache{}, search.UnreleasedConfig{})
		},
		"cache": func() (*search.UnreleasedService, error) {
			return search.NewUnreleasedService(genius, catalog, nil, search.UnreleasedConfig{})
		},
	}
	for missing, build := range cases {
		t.Run(missing, func(t *testing.T) {
			svc, err := build()
			if !errors.Is(err, search.ErrUnreleasedCollaboratorRequired) || svc != nil || !strings.Contains(err.Error(), missing) {
				t.Errorf("built (%v, %v), want a nil service and an error naming %s", svc, err, missing)
			}
		})
	}
}

func TestUnreleasedService_ANullGeniusIsASourceThatIsNotConfigured(t *testing.T) {
	svc := searchtest.MustUnreleased(search.NewUnreleasedService(
		search.NoUnreleasedGenius{}, &search.ArtistCatalogTitles{}, search.NoUnreleasedCache{}, search.UnreleasedConfig{}))

	songs, err := svc.List(context.Background(), search.ProviderDeezer, "1", "Artist", nil)

	if err != nil || len(songs) != 0 {
		t.Errorf("List = %v, %v; want no songs and no error, the same as a source that was never configured", songs, err)
	}
}
