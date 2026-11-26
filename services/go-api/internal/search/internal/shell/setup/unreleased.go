package setup

import (
	"altune/go-api/internal/search/internal/adapters/cache"
	"altune/go-api/internal/search/internal/adapters/connections"
	"altune/go-api/internal/search/internal/adapters/persistence"
	"altune/go-api/internal/search/internal/core/ports"
	"altune/go-api/internal/search/service/artistpage"
	"altune/go-api/internal/search/service/unreleased"
	"fmt"
)

type unreleasedServices struct {
	lookup  *unreleased.Service
	refresh *unreleased.Service
}

func newUnreleasedServices(d connections.Deps, t *providerTable, s connections.Stores, artists *artistpage.GetArtistContentService) (unreleasedServices, error) {
	songs, lists := t.named(rowGenius).unreleasedSongs, t.named(rowTrackers).unreleasedLists
	if songs == nil && lists == nil {
		return unreleasedServices{}, nil
	}
	var genius ports.GeniusSongCatalog = unreleased.NoGenius{}
	if songs != nil {
		genius = songs
	}
	opts := []unreleased.Option{unreleased.WithLifecycle(d.Lifecycle, d.Background), unreleased.WithTrackers(lists)}
	store := unreleasedStore(d, s)
	if store == nil {
		lookup, err := unreleased.NewService(genius, artists, cache.NewRedisUnreleasedCache(s.Redis, s.CacheOption()), d.Unreleased.Tuning, opts...)
		if err != nil {
			return unreleasedServices{}, fmt.Errorf("build the unreleased service: %w", err)
		}
		return unreleasedServices{lookup: lookup}, nil
	}
	opts = append(opts, unreleased.WithStore(store))
	lookup, err := unreleased.NewService(genius, artists, unreleased.NoCache{}, d.Unreleased.Tuning, opts...)
	if err != nil {
		return unreleasedServices{}, fmt.Errorf("build the unreleased service: %w", err)
	}
	return unreleasedServices{lookup: lookup, refresh: lookup}, nil
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
