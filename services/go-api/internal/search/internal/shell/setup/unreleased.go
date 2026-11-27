package setup

import (
	"altune/go-api/internal/search/internal/adapters/cache"
	"altune/go-api/internal/search/internal/adapters/connections"
	"altune/go-api/internal/search/internal/adapters/handler"
	"altune/go-api/internal/search/internal/adapters/persistence"
	"altune/go-api/internal/search/internal/core/ports"
	"altune/go-api/internal/search/internal/service/artistpage"
	"altune/go-api/internal/search/internal/service/unreleased"
	"altune/go-api/internal/shared/clock"
	"context"
	"fmt"
)

type unreleasedServices struct {
	lookup  handler.UnreleasedReader
	refresh *unreleased.Service
}

func newUnreleasedServices(ctx context.Context, d connections.Deps, t *providerTable, s connections.Stores, artists *artistpage.GetArtistContentService) (unreleasedServices, error) {
	songs, lists := t.named(rowGenius).unreleasedSongs, t.named(rowTrackers).unreleasedLists
	if songs == nil && lists == nil {
		return unreleasedServices{lookup: handler.NoUnreleased{}}, nil
	}
	var genius ports.GeniusSongCatalog = unreleased.NoGenius{}
	if songs != nil {
		genius = songs
	}
	var trackers ports.TrackerSongs = unreleased.NoTrackers{}
	if lists != nil {
		trackers = lists
	}
	storage, refreshes, err := unreleasedStorage(ctx, d, s)
	if err != nil {
		return unreleasedServices{}, err
	}
	lookup, err := unreleased.NewService(genius, artists, d.Unreleased.Tuning, unreleased.Wiring{
		Trackers: trackers,
		Storage:  storage,
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

func unreleasedStorage(ctx context.Context, d connections.Deps, s connections.Stores) (unreleased.Storage, bool, error) {
	if store := unreleasedStore(ctx, d, s); store != nil {
		storage, err := unreleased.NewStoredStorage(store)
		if err != nil {
			return nil, false, fmt.Errorf("build the unreleased store storage: %w", err)
		}
		return storage, true, nil
	}
	storage, err := unreleased.NewCachedStorage(cache.NewRedisUnreleasedCache(s.Redis, s.CacheOption()))
	if err != nil {
		return nil, false, fmt.Errorf("build the unreleased cache storage: %w", err)
	}
	return storage, false, nil
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
