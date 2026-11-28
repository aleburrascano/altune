package setup

import (
	"altune/go-api/internal/search/internal/adapters/cache"
	"altune/go-api/internal/search/internal/adapters/connections"
	"altune/go-api/internal/search/internal/adapters/handler"
	"altune/go-api/internal/search/internal/adapters/persistence"
	"altune/go-api/internal/search/internal/ports"
	"altune/go-api/internal/search/internal/service"
	"altune/go-api/internal/shared/clock"
	"context"
	"fmt"
)

type unreleasedServices struct {
	lookup  handler.UnreleasedReader
	refresh *service.UnreleasedService
}

func newUnreleasedServices(ctx context.Context, d connections.Deps, t *providerTable, s connections.Stores, artists *service.GetArtistContentService) (unreleasedServices, error) {
	songs, lists := t.named(rowGenius).unreleasedSongs, t.named(rowTrackers).unreleasedLists
	if songs == nil && lists == nil {
		return unreleasedServices{lookup: handler.NoUnreleased{}}, nil
	}
	var genius ports.GeniusSongCatalog = service.NoGenius{}
	if songs != nil {
		genius = songs
	}
	var trackers ports.TrackerSongs = service.NoTrackers{}
	if lists != nil {
		trackers = lists
	}
	storage, backend, refreshes, err := unreleasedStorage(ctx, d, s)
	if err != nil {
		return unreleasedServices{}, err
	}
	lookup, err := service.NewUnreleasedService(genius, artists, d.Unreleased.Tuning, service.Wiring{
		Trackers: trackers,
		Storage:  storage,
		Backend:  backend,
		Shutdown: d.Shutdown,
		Jobs:     d.Background,
		Timer:    clock.NewTimer,
		Now:      clock.Now,
	})
	if err != nil {
		return unreleasedServices{}, fmt.Errorf("build the unreleased service: %w", err)
	}
	if refreshes {
		return unreleasedServices{lookup: lookup, refresh: lookup}, nil
	}
	return unreleasedServices{lookup: lookup}, nil
}

func unreleasedStorage(ctx context.Context, d connections.Deps, s connections.Stores) (service.Storage, service.Background, bool, error) {
	if store := unreleasedStore(ctx, d, s); store != nil {
		storage, err := service.NewStoredStorage(store)
		if err != nil {
			return nil, nil, false, fmt.Errorf("build the unreleased store storage: %w", err)
		}
		return storage, storage, true, nil
	}
	storage, err := service.NewCachedStorage(cache.NewRedisUnreleasedCache(s.Redis, s.CacheOption()))
	if err != nil {
		return nil, nil, false, fmt.Errorf("build the unreleased cache storage: %w", err)
	}
	return storage, service.NoBackground{}, false, nil
}

func unreleasedStore(ctx context.Context, d connections.Deps, s connections.Stores) ports.UnreleasedStore {
	if d.Unreleased.ServeFromCache {
		return nil
	}
	if s.Pool == nil {
		d.Logger.WarnContext(ctx, "unreleased.store_unavailable_using_cache", "reason", "no database pool")
		return nil
	}
	return persistence.NewPostgresUnreleasedStore(s.Pool)
}
