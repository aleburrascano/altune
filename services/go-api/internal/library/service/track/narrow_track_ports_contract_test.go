package track_test

import (
	"altune/go-api/internal/library"
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

func (stubAdder) Add(context.Context, *library.Track) (*library.Track, bool, error) {
	return nil, false, nil
}

type stubGetter struct{ track *library.Track }

func (g stubGetter) GetByID(context.Context, library.TrackId, ids.UserId) (*library.Track, error) {
	return g.track, nil
}

type stubBatchGetter struct{}

func (stubBatchGetter) ListByIDs(context.Context, ids.UserId, []library.TrackId) ([]*library.Track, error) {
	return nil, nil
}

type stubLister struct{}

func (stubLister) ListForUser(context.Context, ids.UserId, int, int) ([]*library.Track, int, error) {
	return nil, 0, nil
}

type stubCounter struct{ held int }

func (c stubCounter) CountForUser(context.Context, ids.UserId, int) (int, error) {
	return c.held, nil
}

type stubUpdater struct{}

func (stubUpdater) Update(context.Context, *library.Track, int) error { return nil }

type stubNumberSetter struct{}

func (stubNumberSetter) SetTrackNumber(context.Context, library.TrackId, ids.UserId, int) (bool, error) {
	return true, nil
}

type stubDeleter struct{ deleted bool }

func (d stubDeleter) Delete(context.Context, library.TrackId, ids.UserId) (bool, *string, error) {
	return d.deleted, nil, nil
}

type stubAudioRefLookup struct{}

func (stubAudioRefLookup) AudioRefInUse(context.Context, string, library.TrackId) (bool, error) {
	return false, nil
}

func TestServicesDependOnNarrowTrackPorts(t *testing.T) {
	ctx := context.Background()
	userId := library.FixtureUserId()
	store := library.NewInMemoryAudioStore()
	want := &library.Track{Title: "Narrow"}

	got, err := library.NewGetTrackStatusService(stubGetter{track: want}).Execute(ctx, userId, library.NewTrackId())
	if err != nil || got != want {
		t.Fatalf("GetTrackStatus via getter-only port = (%v, %v), want the stub's track", got, err)
	}
	if err := library.NewDeleteTrackService(struct {
		stubDeleter
		stubAudioRefLookup
	}{stubDeleter: stubDeleter{deleted: true}}, store, events.NoopPublisher(), &library.InMemoryAudioStoreMetrics{}, library.NewInMemoryOrphanedAudioQueue(library.NewInMemoryTrackRepo())).Execute(ctx, userId, library.NewTrackId()); err != nil {
		t.Fatalf("DeleteTrack via deleter-only port: %v", err)
	}
	if _, err := library.NewSetTrackNumberService(struct {
		stubGetter
		stubNumberSetter
	}{}).Execute(ctx, userId, library.NewTrackId(), 7); err != nil {
		t.Fatalf("SetTrackNumber via setter-only port: %v", err)
	}

	_ = library.MustNewAddTrackService(struct {
		stubAdder
		stubCounter
		stubUpdater
	}{}, events.NoopPublisher(), &library.InMemoryScheduler{}, time.Now)
	_ = library.MustNewAudioURLService(stubBatchGetter{}, store, time.Now)
	_ = library.MustNewBackfillFeaturedService(stubLister{}, nil, nil, time.Now)
	_ = library.NewStreamTrackService(struct {
		stubGetter
		stubUpdater
	}{}, store)
	_ = library.MustNewPlaylistMembershipService(library.NewInMemoryPlaylistRepo(), struct {
		stubGetter
		stubBatchGetter
	}{}, events.NoopPublisher(), time.Now)
}

type featuringErrLister struct {
	library.FeaturedArtistRepository
	err error
}

func (f featuringErrLister) ListTracksFeaturing(_ context.Context, _ ids.UserId, _ library.FeaturedArtist) ([]*library.Track, error) {
	return nil, f.err
}

func TestHotPaths_ClassifyTransientDBFailures(t *testing.T) {
	userId := library.FixtureUserId()
	cause := context.DeadlineExceeded
	transient := fmt.Errorf("%w: %w", library.ErrDBTransient, cause)
	permanent := errors.New("relation \"tracks\" does not exist")

	paths := []struct {
		name string
		call func(repo *library.InMemoryTrackRepo) error
	}{
		{"stream", func(repo *library.InMemoryTrackRepo) error {
			_, err := library.NewStreamTrackService(repo, library.NewInMemoryAudioStore()).Execute(context.Background(), userId, library.NewTrackId())
			return err
		}},
		{"recover", func(repo *library.InMemoryTrackRepo) error {
			return library.NewStreamTrackService(repo, library.NewInMemoryAudioStore()).RecoverIfMissing(context.Background(), userId, library.NewTrackId())
		}},
		{"status", func(repo *library.InMemoryTrackRepo) error {
			_, err := library.NewGetTrackStatusService(repo).Execute(context.Background(), userId, library.NewTrackId())
			return err
		}},
	}
	for _, p := range paths {
		t.Run(p.name+"/transient is a retryable 503", func(t *testing.T) {
			repo := library.NewInMemoryTrackRepo()
			repo.ErrOnGetBy = transient
			err := p.call(repo)
			if !errors.Is(err, library.ErrCatalogTemporarilyUnavailable) {
				t.Fatalf("err = %v, want ErrCatalogTemporarilyUnavailable", err)
			}
			assertServiceErrorResponse(t, err, http.StatusServiceUnavailable, "catalog.temporarily_unavailable")
		})
		t.Run(p.name+"/permanent stays a 500", func(t *testing.T) {
			repo := library.NewInMemoryTrackRepo()
			repo.ErrOnGetBy = permanent
			err := p.call(repo)
			if errors.Is(err, library.ErrCatalogTemporarilyUnavailable) {
				t.Fatalf("permanent failure classified transient: %v", err)
			}
			assertServiceErrorResponse(t, err, http.StatusInternalServerError, "internal")
		})
	}
}

type lensErrRepo struct {
	library.LibraryViewRepository
	err error
}

func (l lensErrRepo) ListAlbumsForUser(context.Context, ids.UserId, library.LibraryQuery) ([]library.AlbumGroup, error) {
	return nil, l.err
}

func (l lensErrRepo) ListArtistsForUser(context.Context, ids.UserId, library.LibraryQuery) ([]library.ArtistGroup, error) {
	return nil, l.err
}

func TestPlaylistAndLibraryPaths_ClassifyTransientDBFailures(t *testing.T) {
	ctx := context.Background()
	userId := library.FixtureUserId()
	playlistId := library.NewPlaylistId()
	cause := context.DeadlineExceeded
	transient := fmt.Errorf("%w: %w", library.ErrDBTransient, cause)
	permanent := errors.New("relation \"playlists\" does not exist")
	fa := library.NewFeaturedArtistIdentityOnly("SZA", "", 0)

	paths := []struct {
		name string
		call func(injected error) error
	}{
		{"playlist list", func(e error) error {
			repo := library.NewInMemoryPlaylistRepo()
			repo.ErrOnList = e
			_, err := library.MustNewPlaylistLifecycleService(repo, events.NoopPublisher(), time.Now).List(ctx, userId, 10, 0)
			return err
		}},
		{"playlist get", func(e error) error {
			repo := library.NewInMemoryPlaylistRepo()
			repo.ErrOnGetWithTracks = e
			_, _, err := library.MustNewPlaylistLifecycleService(repo, events.NoopPublisher(), time.Now).Get(ctx, userId, playlistId)
			return err
		}},
		{"playlist rename write", func(e error) error {
			repo := library.NewInMemoryPlaylistRepo()
			p := library.SeedPlaylist(t, repo, userId, "before")
			repo.ErrOnUpdate = e
			_, _, err := library.MustNewPlaylistLifecycleService(repo, events.NoopPublisher(), time.Now).Rename(ctx, userId, p.ID, "after")
			return err
		}},
		{"membership add track", func(e error) error {
			plRepo := library.NewInMemoryPlaylistRepo()
			p := library.SeedPlaylist(t, plRepo, userId, "mine")
			trRepo := library.NewInMemoryTrackRepo()
			track := library.SeedTrack(t, trRepo, userId, "T", "A", "B")
			plRepo.ErrOnAddTrack = e
			return library.MustNewPlaylistMembershipService(plRepo, trRepo, events.NoopPublisher(), time.Now).AddTrack(ctx, userId, p.ID, track.ID)
		}},
		{"membership reorder", func(e error) error {
			plRepo := library.NewInMemoryPlaylistRepo()
			plRepo.ErrOnGetTrackOrder = e
			return library.MustNewPlaylistMembershipService(plRepo, library.NewInMemoryTrackRepo(), events.NoopPublisher(), time.Now).Reorder(ctx, userId, playlistId, nil)
		}},
		{"library albums", func(e error) error {
			_, err := library.NewLibraryLensService(lensErrRepo{err: e}).Albums(ctx, userId, library.LibraryQuery{})
			return err
		}},
		{"library artists", func(e error) error {
			_, err := library.NewLibraryLensService(lensErrRepo{err: e}).Artists(ctx, userId, library.LibraryQuery{})
			return err
		}},
		{"list featuring", func(e error) error {
			_, err := library.NewListFeaturingService(featuringErrLister{err: e}).Execute(ctx, userId, fa)
			return err
		}},
	}
	for _, p := range paths {
		t.Run(p.name+"/transient is a retryable 503", func(t *testing.T) {
			err := p.call(transient)
			if !errors.Is(err, library.ErrCatalogTemporarilyUnavailable) {
				t.Fatalf("err = %v, want ErrCatalogTemporarilyUnavailable", err)
			}
			assertServiceErrorResponse(t, err, http.StatusServiceUnavailable, "catalog.temporarily_unavailable")
		})
		t.Run(p.name+"/permanent stays a 500", func(t *testing.T) {
			err := p.call(permanent)
			if errors.Is(err, library.ErrCatalogTemporarilyUnavailable) {
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
	httpserver.HandleServiceError(rec, httptest.NewRequest(http.MethodGet, "/", nil), err)
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
