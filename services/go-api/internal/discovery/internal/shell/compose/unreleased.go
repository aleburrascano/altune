package compose

import (
	"altune/go-api/internal/discovery/internal/adapters/cache"
	"altune/go-api/internal/discovery/internal/adapters/handles"
	"altune/go-api/internal/discovery/internal/adapters/persistence"
	"altune/go-api/internal/discovery/internal/core/ports"
	"altune/go-api/internal/discovery/service/content"
	"altune/go-api/internal/discovery/service/unreleased"
)

type unreleasedServices struct {
	lookup  *unreleased.Service
	refresh *unreleased.Service
}

func newUnreleasedServices(d handles.Deps, t *providerTable, s handles.Stores, artists *content.GetArtistContentService) unreleasedServices {
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

func unreleasedStore(d handles.Deps, s handles.Stores) ports.UnreleasedStore {
	if d.Unreleased.ServeFromCache {
		return nil
	}
	if s.Pool == nil {
		d.Logger.WarnContext(d.Lifecycle, "unreleased.store_unavailable_using_cache", "reason", "no database pool")
		return nil
	}
	return persistence.NewPgxUnreleasedStore(s.Pool)
}
