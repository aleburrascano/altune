package service_test

import (
	"altune/go-api/internal/library/internal/adapters/handler"
	"altune/go-api/internal/library/internal/adapters/memory"
	"altune/go-api/internal/library/internal/adapters/storage"
	"altune/go-api/internal/library/internal/domain"
	"altune/go-api/internal/library/internal/ports"
	"altune/go-api/internal/library/internal/service"
	"altune/go-api/internal/library/librarytest"
	"altune/go-api/internal/shared/clock"
	"altune/go-api/internal/shared/events"
	"altune/go-api/internal/shared/httpserver"
	"altune/go-api/internal/shared/ids"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

type stubAdder struct{}

func (stubAdder) Add(context.Context, *domain.Track) (*domain.Track, bool, error) {
	return nil, false, nil
}

type stubGetter struct{ track *domain.Track }

func (g stubGetter) GetByID(context.Context, domain.TrackId, ids.UserId) (*domain.Track, error) {
	return g.track, nil
}

type stubBatchGetter struct{}

func (stubBatchGetter) ListByIDs(context.Context, ids.UserId, []domain.TrackId) ([]*domain.Track, error) {
	return nil, nil
}

type stubLister struct{}

func (stubLister) ListForUser(context.Context, ids.UserId, int, int) ([]*domain.Track, int, error) {
	return nil, 0, nil
}

type stubCounter struct{ held int }

func (c stubCounter) CountForUser(context.Context, ids.UserId, int) (int, error) {
	return c.held, nil
}

type stubUpdater struct{}

func (stubUpdater) Update(context.Context, *domain.Track, int) error { return nil }

type stubNumberSetter struct{}

func (stubNumberSetter) SetTrackNumber(context.Context, domain.TrackId, ids.UserId, int) (bool, error) {
	return true, nil
}

type stubDeleter struct{ deleted bool }

func (d stubDeleter) Delete(context.Context, domain.TrackId, ids.UserId) (bool, *string, error) {
	return d.deleted, nil, nil
}

type stubAudioRefLookup struct{}

func (stubAudioRefLookup) AudioRefInUse(context.Context, string, domain.TrackId) (bool, error) {
	return false, nil
}

func TestServicesDependOnNarrowTrackPorts(t *testing.T) {
	ctx := context.Background()
	userId := librarytest.UserId()
	store := memory.NewAudioStore()
	want := librarytest.RestoreTrack(librarytest.TrackSnapshot{Title: "Narrow"})

	got, err := service.NewGetTrackStatusService(stubGetter{track: want}).Execute(ctx, userId, librarytest.NewTrackId())
	if err != nil || got != want {
		t.Fatalf("GetTrackStatus via getter-only port = (%v, %v), want the stub's track", got, err)
	}
	if err := service.NewDeleteTrackService(struct {
		stubDeleter
		stubAudioRefLookup
	}{stubDeleter: stubDeleter{deleted: true}}, store, events.NoopPublisher(), &memory.Metrics{}, memory.NewOrphanedAudioQueue(memory.NewTrackRepo(), clock.Now)).Execute(ctx, userId, librarytest.NewTrackId()); err != nil {
		t.Fatalf("DeleteTrack via deleter-only port: %v", err)
	}
	if _, err := service.NewSetTrackNumberService(struct {
		stubGetter
		stubNumberSetter
	}{}).Execute(ctx, userId, librarytest.NewTrackId(), 7); err != nil {
		t.Fatalf("SetTrackNumber via setter-only port: %v", err)
	}

	_ = service.MustNewAddTrackService(struct {
		stubAdder
		stubCounter
		stubUpdater
	}{}, handler.NewWireEvents(events.NoopPublisher()), &memory.Scheduler{}, time.Now, ids.Real)
	_ = service.MustNewAudioURLService(stubBatchGetter{}, storage.NoAudioLinks{}, time.Now, &librarytest.Metrics{})
	_ = service.MustNewBackfillFeaturedService(stubLister{}, nil, nil, time.Now)
	_ = service.NewStreamTrackService(struct {
		stubGetter
		stubUpdater
	}{}, store, ports.NoopAcquisitionScheduler(), &librarytest.Metrics{}, service.AlwaysEnabled, clock.Now)
	_ = service.MustNewPlaylistMembershipService(memory.NewPlaylistRepo(), struct {
		stubGetter
		stubBatchGetter
	}{}, events.NoopPublisher(), time.Now)
}

func TestHotPaths_ClassifyTransientDBFailures(t *testing.T) {
	userId := librarytest.UserId()
	cause := context.DeadlineExceeded
	transient := fmt.Errorf("%w: %w", domain.ErrDBTransient, cause)
	permanent := errors.New("relation \"tracks\" does not exist")

	paths := []struct {
		name string
		call func(repo *memory.TrackRepo) error
	}{
		{"stream", func(repo *memory.TrackRepo) error {
			_, err := service.NewStreamTrackService(repo, memory.NewAudioStore(), ports.NoopAcquisitionScheduler(), &librarytest.Metrics{}, service.AlwaysEnabled, clock.Now).Execute(context.Background(), userId, librarytest.NewTrackId())
			return err
		}},
		{"recover", func(repo *memory.TrackRepo) error {
			return service.NewStreamTrackService(repo, memory.NewAudioStore(), ports.NoopAcquisitionScheduler(), &librarytest.Metrics{}, service.AlwaysEnabled, clock.Now).RecoverIfMissing(context.Background(), userId, librarytest.NewTrackId())
		}},
		{"status", func(repo *memory.TrackRepo) error {
			_, err := service.NewGetTrackStatusService(repo).Execute(context.Background(), userId, librarytest.NewTrackId())
			return err
		}},
	}
	for _, p := range paths {
		t.Run(p.name+"/transient is a retryable 503", func(t *testing.T) {
			repo := memory.NewTrackRepo()
			repo.ErrOnGetBy = transient
			err := p.call(repo)
			if !errors.Is(err, domain.ErrCatalogTemporarilyUnavailable) {
				t.Fatalf("err = %v, want ErrCatalogTemporarilyUnavailable", err)
			}
			assertServiceErrorResponse(t, err, http.StatusServiceUnavailable, "catalog.temporarily_unavailable")
		})
		t.Run(p.name+"/permanent stays a 500", func(t *testing.T) {
			repo := memory.NewTrackRepo()
			repo.ErrOnGetBy = permanent
			err := p.call(repo)
			if errors.Is(err, domain.ErrCatalogTemporarilyUnavailable) {
				t.Fatalf("permanent failure classified transient: %v", err)
			}
			assertServiceErrorResponse(t, err, http.StatusInternalServerError, "internal")
		})
	}
}

type lensErrRepo struct {
	ports.LibraryViewRepository
	err error
}

func (l lensErrRepo) ListAlbumsForUser(context.Context, ids.UserId, domain.LibraryQuery) ([]domain.AlbumGroup, error) {
	return nil, l.err
}

func (l lensErrRepo) ListArtistsForUser(context.Context, ids.UserId, domain.LibraryQuery) ([]domain.ArtistGroup, error) {
	return nil, l.err
}

func TestPlaylistAndLibraryPaths_ClassifyTransientDBFailures(t *testing.T) {
	ctx := context.Background()
	userId := librarytest.UserId()
	playlistId := librarytest.NewPlaylistId()
	cause := context.DeadlineExceeded
	transient := fmt.Errorf("%w: %w", domain.ErrDBTransient, cause)
	permanent := errors.New("relation \"playlists\" does not exist")
	fa := domain.NewFeaturedArtistIdentityOnly("SZA", "", 0)

	paths := []struct {
		name string
		call func(injected error) error
	}{
		{"playlist list", func(e error) error {
			repo := memory.NewPlaylistRepo()
			repo.ErrOnList = e
			_, err := service.MustNewPlaylistLifecycleService(repo, events.NoopPublisher(), time.Now, ids.Real).List(ctx, userId, 10, 0)
			return err
		}},
		{"playlist get", func(e error) error {
			repo := memory.NewPlaylistRepo()
			repo.ErrOnGetWithTracks = e
			_, _, err := service.MustNewPlaylistLifecycleService(repo, events.NoopPublisher(), time.Now, ids.Real).Get(ctx, userId, playlistId)
			return err
		}},
		{"playlist rename write", func(e error) error {
			repo := memory.NewPlaylistRepo()
			p := librarytest.SeedPlaylist(t, repo, userId, "before")
			repo.ErrOnUpdate = e
			_, _, err := service.MustNewPlaylistLifecycleService(repo, events.NoopPublisher(), time.Now, ids.Real).Rename(ctx, userId, p.ID, "after")
			return err
		}},
		{"membership add track", func(e error) error {
			plRepo := memory.NewPlaylistRepo()
			p := librarytest.SeedPlaylist(t, plRepo, userId, "mine")
			trRepo := memory.NewTrackRepo()
			track := librarytest.SeedTrack(t, trRepo, userId, "T", "A", "B")
			plRepo.ErrOnAddTrack = e
			return service.MustNewPlaylistMembershipService(plRepo, trRepo, events.NoopPublisher(), time.Now).AddTrack(ctx, userId, p.ID, track.ID())
		}},
		{"membership reorder", func(e error) error {
			plRepo := memory.NewPlaylistRepo()
			plRepo.ErrOnGetTrackOrder = e
			return service.MustNewPlaylistMembershipService(plRepo, memory.NewTrackRepo(), events.NoopPublisher(), time.Now).Reorder(ctx, userId, playlistId, nil)
		}},
		{"library albums", func(e error) error {
			_, err := service.NewLibraryLensService(lensErrRepo{err: e}).Albums(ctx, userId, domain.LibraryQuery{})
			return err
		}},
		{"library artists", func(e error) error {
			_, err := service.NewLibraryLensService(lensErrRepo{err: e}).Artists(ctx, userId, domain.LibraryQuery{})
			return err
		}},
		{"list featuring", func(e error) error {
			_, err := service.NewListFeaturingService(featuringErrLister{err: e}).Execute(ctx, userId, fa)
			return err
		}},
	}
	for _, p := range paths {
		t.Run(p.name+"/transient is a retryable 503", func(t *testing.T) {
			err := p.call(transient)
			if !errors.Is(err, domain.ErrCatalogTemporarilyUnavailable) {
				t.Fatalf("err = %v, want ErrCatalogTemporarilyUnavailable", err)
			}
			assertServiceErrorResponse(t, err, http.StatusServiceUnavailable, "catalog.temporarily_unavailable")
		})
		t.Run(p.name+"/permanent stays a 500", func(t *testing.T) {
			err := p.call(permanent)
			if errors.Is(err, domain.ErrCatalogTemporarilyUnavailable) {
				t.Fatalf("permanent failure classified transient: %v", err)
			}
			if !errors.Is(err, permanent) {
				t.Errorf("err = %v lost the original error", err)
			}
			assertServiceErrorResponse(t, err, http.StatusInternalServerError, "internal")
		})
	}
}

func assertServiceErrorResponse(t *testing.T, err error, wantStatus int, wantCode string) {
	t.Helper()
	rec := httptest.NewRecorder()
	handler.HandleError(rec, httptest.NewRequest(http.MethodGet, "/", nil), err)
	if rec.Code != wantStatus {
		t.Errorf("status = %d, want %d", rec.Code, wantStatus)
	}
	var body httpserver.Problem
	if decodeErr := json.NewDecoder(rec.Body).Decode(&body); decodeErr != nil {
		t.Fatalf("decode body: %v", decodeErr)
	}
	if body.Code != wantCode {
		t.Errorf("code = %q, want %q", body.Code, wantCode)
	}
}
