package unreleased

import (
	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/discovery/ports"
	"altune/go-api/internal/shared/runloop"
	"altune/go-api/internal/shared/textnorm"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"
)

var errJobPanicked = errors.New("unreleased job panicked")

type Lookup struct {
	Songs   []domain.UnreleasedSong
	Pending bool
}

type Option func(*Service)

func WithLifecycle(ctx context.Context, jobs *sync.WaitGroup) Option {
	return func(s *Service) {
		s.lifecycle = ctx
		s.jobs = jobs
	}
}

type job struct {
	done  chan struct{}
	songs []domain.UnreleasedSong
	err   error
}

func (s *Service) Lookup(ctx context.Context, provider domain.ProviderName, externalID, artistName string, known map[domain.ProviderName]string) (Lookup, error) {
	artistName = strings.TrimSpace(artistName)
	if artistName == "" {
		return Lookup{Songs: []domain.UnreleasedSong{}}, nil
	}
	if s.store != nil {
		return s.lookupStored(ctx, provider, externalID, artistName, known)
	}
	nameKey := textnorm.NormalizeForIdentity(artistName)
	if s.cache != nil {
		if songs, ok := s.cache.Get(ctx, provider, externalID, nameKey); ok {
			return Lookup{Songs: songs}, nil
		}
	}
	j := s.joinJob(fmt.Sprintf("%s:%s:%s", provider.Key(), externalID, nameKey), func(jobCtx context.Context) ([]domain.UnreleasedSong, error) {
		return s.List(jobCtx, provider, externalID, artistName, known)
	})
	return s.awaitJob(ctx, j)
}

func (s *Service) lookupStored(ctx context.Context, provider domain.ProviderName, externalID, artistName string, known map[domain.ProviderName]string) (Lookup, error) {
	key := ports.UnreleasedArtistKey{Provider: provider, ExternalID: externalID}
	if songs, built := s.storedSongs(ctx, key, artistName, known); built {
		return Lookup{Songs: nonNil(songs)}, nil
	}
	jobKey := fmt.Sprintf("%s:%s:%s", provider.Key(), externalID, textnorm.NormalizeForIdentity(artistName))
	j := s.joinJob(jobKey, func(jobCtx context.Context) ([]domain.UnreleasedSong, error) {
		return s.Rebuild(jobCtx, key, artistName, known)
	})
	return s.awaitJob(ctx, j)
}

// storedSongs records the artist, then reads it. A store failure is logged and
// reads as not built, so the request falls through to a live build.
func (s *Service) storedSongs(ctx context.Context, key ports.UnreleasedArtistKey, artistName string, known map[domain.ProviderName]string) ([]domain.UnreleasedSong, bool) {
	if err := s.store.Record(ctx, key, artistName, known); err != nil {
		slog.WarnContext(ctx, "unreleased.store_record_failed", "error", err)
	}
	songs, _, built, err := s.store.Read(ctx, key)
	if err != nil {
		slog.WarnContext(ctx, "unreleased.store_read_failed", "error", err)
		return nil, false
	}
	return songs, built
}

func nonNil(songs []domain.UnreleasedSong) []domain.UnreleasedSong {
	if songs == nil {
		return []domain.UnreleasedSong{}
	}
	return songs
}

func (s *Service) awaitJob(ctx context.Context, j *job) (Lookup, error) {
	var deadline <-chan time.Time
	if s.cfg.Wait > 0 {
		timer := time.NewTimer(s.cfg.Wait)
		defer timer.Stop()
		deadline = timer.C
	}
	select {
	case <-j.done:
		return Lookup{Songs: j.songs}, j.err
	case <-deadline:
		return Lookup{Songs: []domain.UnreleasedSong{}, Pending: true}, nil
	case <-ctx.Done():
		return Lookup{}, ctx.Err()
	}
}

func (s *Service) joinJob(key string, run func(context.Context) ([]domain.UnreleasedSong, error)) *job {
	s.mu.Lock()
	defer s.mu.Unlock()
	if running, ok := s.inflight[key]; ok {
		return running
	}
	j := &job{done: make(chan struct{})}
	s.inflight[key] = j
	s.jobs.Add(1)
	go s.runJob(key, j, run)
	return j
}

func (s *Service) runJob(key string, j *job, run func(context.Context) ([]domain.UnreleasedSong, error)) {
	defer s.jobs.Done()
	ctx := s.lifecycle
	if s.cfg.JobTimeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, s.cfg.JobTimeout)
		defer cancel()
	}
	defer func() {
		s.mu.Lock()
		delete(s.inflight, key)
		s.mu.Unlock()
		close(j.done)
	}()
	defer runloop.RecoverContext(ctx, "unreleased.job_panic", "key", key)
	j.err = errJobPanicked
	j.songs, j.err = run(ctx)
	if j.err != nil {
		slog.WarnContext(ctx, "unreleased.job_failed", "key", key, "error", j.err)
	}
}
