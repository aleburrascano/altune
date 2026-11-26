package searchtest

import (
	"altune/go-api/internal/search/internal/adapters/memory"
	"altune/go-api/internal/search/service/unreleased"
)

func MustUnreleased(svc *unreleased.Service, err error) *unreleased.Service {
	if err != nil {
		panic(err)
	}
	return svc
}

type (
	GeniusSongCatalog   = memory.GeniusSongCatalog
	ArtistCatalogTitles = memory.ArtistCatalogTitles
	TrackerSongs        = memory.TrackerSongs
	StoreWrite          = memory.StoreWrite
	StoredArtist        = memory.StoredArtist
	UnreleasedStore     = memory.UnreleasedStore
)
