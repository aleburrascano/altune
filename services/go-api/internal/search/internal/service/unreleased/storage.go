package unreleased

import (
	"altune/go-api/internal/search/internal/core/ports"
	"altune/go-api/internal/search/internal/domain"
	"altune/go-api/internal/shared/logging"
	"altune/go-api/internal/shared/textnorm"
	"context"
	"fmt"
	"log/slog"
	"time"
)

type Storage interface {
	persist(ctx context.Context, key ports.UnreleasedArtistKey, songs []domain.UnreleasedSong, complete bool, startedAt time.Time)
	list(ctx context.Context, s *Service, provider domain.ProviderName, externalID, artistName string, known map[domain.ProviderName]string) ([]domain.UnreleasedSong, error)
	lookup(ctx context.Context, s *Service, provider domain.ProviderName, externalID, artistName string, known map[domain.ProviderName]string) (Lookup, error)
	warm(s *Service, artists []ArtistToRefresh)
	due(ctx context.Context, now time.Time, limit int) ([]ports.UnreleasedDueArtist, error)
	search(ctx context.Context, query string) ([]ports.UnreleasedHit, error)
}

type cachedStorage struct{ cache ports.UnreleasedCache }

func NewCachedStorage(cache ports.UnreleasedCache) (Storage, error) {
	if cache == nil {
		return nil, fmt.Errorf("%w: cache is nil", ErrCollaboratorRequired)
	}
	return cachedStorage{cache: cache}, nil
}

func (cachedStorage) persist(context.Context, ports.UnreleasedArtistKey, []domain.UnreleasedSong, bool, time.Time) {
}

func (c cachedStorage) list(ctx context.Context, s *Service, provider domain.ProviderName, externalID, artistName string, known map[domain.ProviderName]string) ([]domain.UnreleasedSong, error) {
	nameKey := textnorm.NormalizeForIdentity(artistName)
	if songs, ok := c.cache.Get(ctx, provider, externalID, nameKey); ok {
		return songs, nil
	}
	songs, complete, err := s.build(ctx, provider, externalID, artistName, known)
	if err != nil {
		return nil, err
	}
	if err := c.cache.Set(ctx, provider, externalID, nameKey, songs, s.cacheTTL(songs, complete)); err != nil {
		slog.WarnContext(ctx, "unreleased.cache_set_failed", logging.KeyError, err)
	}
	return songs, nil
}

func (c cachedStorage) lookup(ctx context.Context, s *Service, provider domain.ProviderName, externalID, artistName string, known map[domain.ProviderName]string) (Lookup, error) {
	nameKey := textnorm.NormalizeForIdentity(artistName)
	if songs, ok := c.cache.Get(ctx, provider, externalID, nameKey); ok {
		return Lookup{Songs: songs}, nil
	}
	j := s.joinJob(ctx, fmt.Sprintf("%s:%s:%s", provider.Key(), externalID, nameKey), func(jobCtx context.Context) ([]domain.UnreleasedSong, error) {
		return s.List(jobCtx, provider, externalID, artistName, known)
	})
	return s.awaitJob(ctx, j)
}

func (cachedStorage) warm(*Service, []ArtistToRefresh) {}

func (cachedStorage) due(context.Context, time.Time, int) ([]ports.UnreleasedDueArtist, error) {
	return nil, nil
}

func (cachedStorage) search(context.Context, string) ([]ports.UnreleasedHit, error) {
	return []ports.UnreleasedHit{}, nil
}

type storedStorage struct{ store ports.UnreleasedStore }

func NewStoredStorage(store ports.UnreleasedStore) (Storage, error) {
	if store == nil {
		return nil, fmt.Errorf("%w: store is nil", ErrCollaboratorRequired)
	}
	return storedStorage{store: store}, nil
}

func (st storedStorage) persist(ctx context.Context, key ports.UnreleasedArtistKey, songs []domain.UnreleasedSong, complete bool, startedAt time.Time) {
	if err := st.store.Write(ctx, key, songs, complete, startedAt, CurrentBuildVersion); err != nil {
		slog.WarnContext(ctx, "unreleased.store_write_failed", logging.KeyError, err)
	}
}

func (storedStorage) list(ctx context.Context, s *Service, provider domain.ProviderName, externalID, artistName string, known map[domain.ProviderName]string) ([]domain.UnreleasedSong, error) {
	return s.Rebuild(ctx, ports.UnreleasedArtistKey{Provider: provider, ExternalID: externalID}, artistName, known)
}

func (st storedStorage) lookup(ctx context.Context, s *Service, provider domain.ProviderName, externalID, artistName string, known map[domain.ProviderName]string) (Lookup, error) {
	key := ports.UnreleasedArtistKey{Provider: provider, ExternalID: externalID}
	songs, version, built := st.storedSongsOrNotBuilt(ctx, key, artistName, known)
	jobKey := fmt.Sprintf("%s:%s:%s", provider.Key(), externalID, textnorm.NormalizeForIdentity(artistName))
	if built && version >= CurrentBuildVersion {
		return Lookup{Songs: nonNil(songs)}, nil
	}
	j := s.joinJob(ctx, jobKey, func(jobCtx context.Context) ([]domain.UnreleasedSong, error) {
		return s.Rebuild(jobCtx, key, artistName, known)
	})
	if built {
		return Lookup{Songs: nonNil(songs)}, nil
	}
	return s.awaitJob(ctx, j)
}

func (st storedStorage) storedSongsOrNotBuilt(ctx context.Context, key ports.UnreleasedArtistKey, artistName string, known map[domain.ProviderName]string) ([]domain.UnreleasedSong, int, bool) {
	if err := st.store.Record(ctx, key, artistName, known); err != nil {
		slog.WarnContext(ctx, "unreleased.store_record_failed", logging.KeyError, err)
	}
	songs, _, built, version, err := st.store.Read(ctx, key)
	if err != nil {
		slog.WarnContext(ctx, "unreleased.store_read_failed", logging.KeyError, err)
		return nil, 0, false
	}
	return songs, version, built
}

func (st storedStorage) warm(s *Service, artists []ArtistToRefresh) {
	if len(artists) == 0 {
		return
	}
	s.jobs.Track(func() { s.runWarm(st.store, artists) })
}

func (st storedStorage) due(ctx context.Context, now time.Time, limit int) ([]ports.UnreleasedDueArtist, error) {
	return st.store.Due(ctx, now, limit)
}

func (st storedStorage) search(ctx context.Context, query string) ([]ports.UnreleasedHit, error) {
	return st.store.Search(ctx, query, SearchLimit)
}

func nonNil(songs []domain.UnreleasedSong) []domain.UnreleasedSong {
	if songs == nil {
		return []domain.UnreleasedSong{}
	}
	return songs
}
