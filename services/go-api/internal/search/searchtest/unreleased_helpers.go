package searchtest

import (
	"altune/go-api/internal/search/internal/adapters/memory"
	"altune/go-api/internal/search/internal/core/ports"
	"altune/go-api/internal/search/internal/service/unreleased"
	"altune/go-api/internal/shared/background"
	"altune/go-api/internal/shared/clock"
	"context"
	"testing"
)

type UnreleasedPart func(*unreleased.Wiring)

func UnreleasedWiring(t testing.TB, cache ports.UnreleasedCache, parts ...UnreleasedPart) unreleased.Wiring {
	t.Helper()
	storage, err := unreleased.NewCachedStorage(cache)
	if err != nil {
		panic(err)
	}
	w := unreleased.Wiring{
		Trackers: unreleased.NoTrackers{},
		Storage:  storage,
		Shutdown: t.Context().Done(),
		Jobs:     &background.Tracker{},
		Timer:    clock.NewTimer,
		Now:      clock.Now,
	}
	for _, part := range parts {
		part(&w)
	}
	return w
}

func UnreleasedStored(store ports.UnreleasedStore) UnreleasedPart {
	return func(w *unreleased.Wiring) {
		storage, err := unreleased.NewStoredStorage(store)
		if err != nil {
			panic(err)
		}
		w.Storage = storage
	}
}

func UnreleasedLifecycle(ctx context.Context, jobs *background.Tracker) UnreleasedPart {
	return func(w *unreleased.Wiring) { w.Shutdown, w.Jobs = ctx.Done(), jobs }
}

func UnreleasedTrackers(trackers ports.TrackerSongs) UnreleasedPart {
	return func(w *unreleased.Wiring) { w.Trackers = trackers }
}

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
