package searchtest

import (
	"altune/go-api/internal/search/internal/adapters/memory"
	"altune/go-api/internal/search/internal/ports"
	"altune/go-api/internal/search/internal/service"
	"altune/go-api/internal/shared/background"
	"altune/go-api/internal/shared/clock"
	"context"
	"testing"
)

type UnreleasedPart func(*service.Wiring)

func UnreleasedWiring(t testing.TB, cache ports.UnreleasedCache, parts ...UnreleasedPart) service.Wiring {
	t.Helper()
	storage, err := service.NewCachedStorage(cache)
	if err != nil {
		panic(err)
	}
	w := service.Wiring{
		Trackers: service.NoTrackers{},
		Storage:  storage,
		Backend:  service.NoBackground{},
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
	return func(w *service.Wiring) {
		storage, err := service.NewStoredStorage(store)
		if err != nil {
			panic(err)
		}
		w.Storage = storage
		w.Backend = storage
	}
}

func UnreleasedLifecycle(ctx context.Context, jobs *background.Tracker) UnreleasedPart {
	return func(w *service.Wiring) { w.Shutdown, w.Jobs = ctx.Done(), jobs }
}

func UnreleasedTrackers(trackers ports.TrackerSongs) UnreleasedPart {
	return func(w *service.Wiring) { w.Trackers = trackers }
}

func MustUnreleased(svc *service.UnreleasedService, err error) *service.UnreleasedService {
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
