package setup

import (
	"altune/go-api/internal/search/internal/adapters/cache"
	"altune/go-api/internal/search/internal/adapters/connections"
	"altune/go-api/internal/search/internal/adapters/persistence"
	"altune/go-api/internal/search/internal/core/ports"
	"altune/go-api/internal/search/service/artistpage"
	"altune/go-api/internal/search/service/unreleased"
)

type unreleasedServices struct {
	lookup  *unreleased.Service
	refresh *unreleased.Service
}

func newUnreleasedServices(d connections.Deps, t *providerTable, s connections.Stores, artists *artistpage.GetArtistContentService) unreleasedServices {
	songs, lists := t.named(rowGenius).unreleasedSongs, t.named(rowTrackers).unreleasedLists
	if songs == nil && lists == nil {
		return unreleasedServices{}
	}
	opts := []unreleased.Option{unreleased.WithLifecycle(d.Lifecycle, d.Background), unreleased.WithTrackers(lists)}
	store := unreleasedStore(d, s)
	if store == nil {
		lookup := unreleased.NewService(songs, artists, cache.NewRedisUnreleasedCache(s.Redis, s.CacheOption()), d.Unreleased.Tuning, opts...)
		return unreleasedServices{lookup: lookup}
	}
	opts = append(opts, unreleased.WithStore(store))
	lookup := unreleased.NewService(songs, artists, nil, d.Unreleased.Tuning, opts...)
	return unreleasedServices{lookup: lookup, refresh: lookup}
}

func unreleasedStore(d connections.Deps, s connections.Stores) ports.UnreleasedStore {
	if d.Unreleased.ServeFromCache {
		return nil
	}
	if s.Pool == nil {
		d.Logger.WarnContext(d.Lifecycle, "unreleased.store_unavailable_using_cache", "reason", "no database pool")
		return nil
	}
	return persistence.NewPostgresUnreleasedStore(s.Pool)
}
