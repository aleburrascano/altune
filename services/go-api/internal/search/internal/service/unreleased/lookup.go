package unreleased

import (
	"altune/go-api/internal/search/internal/core/ports"
	"altune/go-api/internal/search/internal/domain"
	"altune/go-api/internal/shared/background"
	"altune/go-api/internal/shared/logging"
	"context"
	"errors"
	"log/slog"
	"strings"
	"time"
)

var errJobPanicked = errors.New("unreleased job panicked")

type Lookup struct {
	Songs   []domain.UnreleasedSong
	Pending bool
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
	return s.storage.lookup(ctx, s, provider, externalID, artistName, known)
}

const warmTimeout = 5 * time.Second

type ArtistToRefresh struct {
	Provider   domain.ProviderName
	ExternalID string
	Name       string
	Known      map[domain.ProviderName]string
}

func (s *Service) Warm(ctx context.Context, artists []ArtistToRefresh) {
	s.storage.warm(ctx, s, artists)
}

func (s *Service) cancelOnShutdown(ctx context.Context, cancel context.CancelFunc) (stop func()) {
	quit := make(chan struct{})
	background.Go(ctx, "unreleased.shutdown_watch", func() {
		select {
		case <-s.shutdown:
			cancel()
		case <-quit:
		}
	})
	return func() { close(quit) }
}

func (s *Service) runWarm(requestCtx context.Context, store ports.UnreleasedStore, artists []ArtistToRefresh) {
	ctx, cancel := background.Detach(requestCtx, warmTimeout)
	defer cancel()
	defer s.cancelOnShutdown(ctx, cancel)()
	defer background.RecoverContext(ctx, "unreleased.warm_panic")
	seen := make(map[ports.UnreleasedArtistKey]bool, len(artists))
	for _, a := range artists {
		key := ports.UnreleasedArtistKey{Provider: a.Provider, ExternalID: a.ExternalID}
		name := strings.TrimSpace(a.Name)
		if name == "" || seen[key] {
			continue
		}
		seen[key] = true
		if err := store.Record(ctx, key, name, a.Known); err != nil {
			slog.WarnContext(ctx, "unreleased.warm_record_failed", logging.KeyError, err)
		}
	}
}

func (s *Service) awaitJob(ctx context.Context, j *job) (Lookup, error) {
	var deadline <-chan time.Time
	if s.cfg.Wait > 0 {
		fires, stop := s.newTimer(s.cfg.Wait)
		defer stop()
		deadline = fires
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

func (s *Service) joinJob(ctx context.Context, key string, run func(context.Context) ([]domain.UnreleasedSong, error)) *job {
	s.mu.Lock()
	defer s.mu.Unlock()
	if running, ok := s.inflight[key]; ok {
		return running
	}
	j := &job{done: make(chan struct{})}
	s.inflight[key] = j
	s.jobs.Track(func() { s.runJob(ctx, key, j, run) })
	return j
}

func (s *Service) runJob(requestCtx context.Context, key string, j *job, run func(context.Context) ([]domain.UnreleasedSong, error)) {
	ctx, cancel := background.DetachCancelable(requestCtx)
	defer cancel()
	defer s.cancelOnShutdown(ctx, cancel)()
	if s.cfg.JobTimeout > 0 {
		var cancelTimeout context.CancelFunc
		ctx, cancelTimeout = context.WithTimeout(ctx, s.cfg.JobTimeout)
		defer cancelTimeout()
	}
	defer func() {
		s.mu.Lock()
		delete(s.inflight, key)
		s.mu.Unlock()
		close(j.done)
	}()
	defer background.RecoverContext(ctx, "unreleased.job_panic", "key", key)
	j.err = errJobPanicked
	j.songs, j.err = run(ctx)
	if j.err != nil {
		slog.WarnContext(ctx, "unreleased.job_failed", "key", key, logging.KeyError, j.err)
	}
}
