package track_test

import (
	"altune/go-api/internal/catalog"
	"altune/go-api/internal/shared/httputil"
	shared "altune/go-api/internal/shared/kernel"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

type stubAdder struct{}

func (stubAdder) Add(context.Context, *catalog.Track) (*catalog.Track, bool, error) {
	return nil, false, nil
}

type stubGetter struct{ track *catalog.Track }

func (g stubGetter) GetByID(context.Context, catalog.TrackId, shared.UserId) (*catalog.Track, error) {
	return g.track, nil
}

type stubBatchGetter struct{}

func (stubBatchGetter) ListByIDs(context.Context, shared.UserId, []catalog.TrackId) ([]*catalog.Track, error) {
	return nil, nil
}

type stubLister struct{}

func (stubLister) ListForUser(context.Context, shared.UserId, int, int) ([]*catalog.Track, int, error) {
	return nil, 0, nil
}

type stubCounter struct{ held int }

func (c stubCounter) CountForUser(context.Context, shared.UserId, int) (int, error) {
	return c.held, nil
}

type stubUpdater struct{}

func (stubUpdater) Update(context.Context, *catalog.Track, int) error { return nil }

type stubNumberSetter struct{}

func (stubNumberSetter) SetTrackNumber(context.Context, catalog.TrackId, shared.UserId, int) (bool, error) {
	return true, nil
}

type stubDeleter struct{ deleted bool }

func (d stubDeleter) Delete(context.Context, catalog.TrackId, shared.UserId) (bool, *string, error) {
	return d.deleted, nil, nil
}

type stubAudioRefLookup struct{}

func (stubAudioRefLookup) AudioRefInUse(context.Context, string, catalog.TrackId) (bool, error) {
	return false, nil
}

func TestServicesDependOnNarrowTrackPorts(t *testing.T) {
	ctx := context.Background()
	userId := catalog.FixtureUserId()
	store := catalog.NewInMemoryAudioStore()
	want := &catalog.Track{Title: "Narrow"}

	got, err := catalog.NewGetTrackStatusService(stubGetter{track: want}).Execute(ctx, userId, catalog.NewTrackId())
	if err != nil || got != want {
		t.Fatalf("GetTrackStatus via getter-only port = (%v, %v), want the stub's track", got, err)
	}
	if err := catalog.NewDeleteTrackService(struct {
		stubDeleter
		stubAudioRefLookup
	}{stubDeleter: stubDeleter{deleted: true}}, store).Execute(ctx, userId, catalog.NewTrackId()); err != nil {
		t.Fatalf("DeleteTrack via deleter-only port: %v", err)
	}
	if _, err := catalog.NewSetTrackNumberService(struct {
		stubGetter
		stubNumberSetter
	}{}).Execute(ctx, userId, catalog.NewTrackId(), 7); err != nil {
		t.Fatalf("SetTrackNumber via setter-only port: %v", err)
	}

	_ = catalog.NewAddTrackService(struct {
		stubAdder
		stubCounter
		stubUpdater
	}{})
	_ = catalog.NewAudioURLService(stubBatchGetter{}, store)
	_ = catalog.NewBackfillFeaturedService(stubLister{}, nil, nil)
	_ = catalog.NewStreamTrackService(struct {
		stubGetter
		stubUpdater
	}{}, store)
	_ = catalog.NewPlaylistMembershipService(catalog.NewInMemoryPlaylistRepo(), struct {
		stubGetter
		stubBatchGetter
	}{})
}

type featuringErrLister struct {
	catalog.FeaturedArtistRepository
	err error
}

func (f featuringErrLister) ListTracksFeaturing(_ context.Context, _ shared.UserId, _ catalog.FeaturedArtist) ([]*catalog.Track, error) {
	return nil, f.err
}

func TestHotPaths_ClassifyTransientDBFailures(t *testing.T) {
	userId := catalog.FixtureUserId()
	cause := context.DeadlineExceeded
	transient := fmt.Errorf("%w: %w", catalog.ErrDBTransient, cause)
	permanent := errors.New("relation \"tracks\" does not exist")

	paths := []struct {
		name string
		call func(repo *catalog.InMemoryTrackRepo) error
	}{
		{"stream", func(repo *catalog.InMemoryTrackRepo) error {
			_, err := catalog.NewStreamTrackService(repo, catalog.NewInMemoryAudioStore()).Execute(context.Background(), userId, catalog.NewTrackId())
			return err
		}},
		{"recover", func(repo *catalog.InMemoryTrackRepo) error {
			return catalog.NewStreamTrackService(repo, catalog.NewInMemoryAudioStore()).RecoverIfMissing(context.Background(), userId, catalog.NewTrackId())
		}},
		{"status", func(repo *catalog.InMemoryTrackRepo) error {
			_, err := catalog.NewGetTrackStatusService(repo).Execute(context.Background(), userId, catalog.NewTrackId())
			return err
		}},
	}
	for _, p := range paths {
		t.Run(p.name+"/transient is a retryable 503", func(t *testing.T) {
			repo := catalog.NewInMemoryTrackRepo()
			repo.ErrOnGetBy = transient
			err := p.call(repo)
			if !errors.Is(err, catalog.ErrCatalogTemporarilyUnavailable) {
				t.Fatalf("err = %v, want ErrCatalogTemporarilyUnavailable", err)
			}
			assertServiceErrorResponse(t, err, http.StatusServiceUnavailable, "catalog.temporarily_unavailable")
		})
		t.Run(p.name+"/permanent stays a 500", func(t *testing.T) {
			repo := catalog.NewInMemoryTrackRepo()
			repo.ErrOnGetBy = permanent
			err := p.call(repo)
			if errors.Is(err, catalog.ErrCatalogTemporarilyUnavailable) {
				t.Fatalf("permanent failure classified transient: %v", err)
			}
			assertServiceErrorResponse(t, err, http.StatusInternalServerError, "internal")
		})
	}
}

type lensErrRepo struct {
	catalog.LibraryLensRepository
	err error
}

func (l lensErrRepo) ListAlbumsForUser(context.Context, shared.UserId, catalog.LibraryQuery) ([]catalog.AlbumGroup, error) {
	return nil, l.err
}

func (l lensErrRepo) ListArtistsForUser(context.Context, shared.UserId, catalog.LibraryQuery) ([]catalog.ArtistGroup, error) {
	return nil, l.err
}

func TestPlaylistAndLibraryPaths_ClassifyTransientDBFailures(t *testing.T) {
	ctx := context.Background()
	userId := catalog.FixtureUserId()
	playlistId := catalog.NewPlaylistId()
	cause := context.DeadlineExceeded
	transient := fmt.Errorf("%w: %w", catalog.ErrDBTransient, cause)
	permanent := errors.New("relation \"playlists\" does not exist")
	fa := catalog.NewFeaturedArtistIdentityOnly("SZA", "", 0)

	paths := []struct {
		name string
		call func(injected error) error
	}{
		{"playlist list", func(e error) error {
			repo := catalog.NewInMemoryPlaylistRepo()
			repo.ErrOnList = e
			_, err := catalog.NewPlaylistLifecycleService(repo).List(ctx, userId, 10, 0)
			return err
		}},
		{"playlist get", func(e error) error {
			repo := catalog.NewInMemoryPlaylistRepo()
			repo.ErrOnGetWithTracks = e
			_, _, err := catalog.NewPlaylistLifecycleService(repo).Get(ctx, userId, playlistId)
			return err
		}},
		{"playlist rename write", func(e error) error {
			repo := catalog.NewInMemoryPlaylistRepo()
			p := catalog.SeedPlaylist(t, repo, userId, "before")
			repo.ErrOnUpdate = e
			_, _, err := catalog.NewPlaylistLifecycleService(repo).Rename(ctx, userId, p.ID, "after")
			return err
		}},
		{"membership add track", func(e error) error {
			plRepo := catalog.NewInMemoryPlaylistRepo()
			p := catalog.SeedPlaylist(t, plRepo, userId, "mine")
			trRepo := catalog.NewInMemoryTrackRepo()
			track := catalog.SeedTrack(t, trRepo, userId, "T", "A", "B")
			plRepo.ErrOnAddTrack = e
			return catalog.NewPlaylistMembershipService(plRepo, trRepo).AddTrack(ctx, userId, p.ID, track.ID)
		}},
		{"membership reorder", func(e error) error {
			plRepo := catalog.NewInMemoryPlaylistRepo()
			plRepo.ErrOnGetTrackOrder = e
			return catalog.NewPlaylistMembershipService(plRepo, catalog.NewInMemoryTrackRepo()).Reorder(ctx, userId, playlistId, nil)
		}},
		{"library albums", func(e error) error {
			_, err := catalog.NewLibraryLensService(lensErrRepo{err: e}).Albums(ctx, userId, catalog.LibraryQuery{})
			return err
		}},
		{"library artists", func(e error) error {
			_, err := catalog.NewLibraryLensService(lensErrRepo{err: e}).Artists(ctx, userId, catalog.LibraryQuery{})
			return err
		}},
		{"list featuring", func(e error) error {
			_, err := catalog.NewListFeaturingService(featuringErrLister{err: e}).Execute(ctx, userId, fa)
			return err
		}},
	}
	for _, p := range paths {
		t.Run(p.name+"/transient is a retryable 503", func(t *testing.T) {
			err := p.call(transient)
			if !errors.Is(err, catalog.ErrCatalogTemporarilyUnavailable) {
				t.Fatalf("err = %v, want ErrCatalogTemporarilyUnavailable", err)
			}
			assertServiceErrorResponse(t, err, http.StatusServiceUnavailable, "catalog.temporarily_unavailable")
		})
		t.Run(p.name+"/permanent stays a 500", func(t *testing.T) {
			err := p.call(permanent)
			if errors.Is(err, catalog.ErrCatalogTemporarilyUnavailable) {
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
	httputil.HandleServiceError(rec, httptest.NewRequest(http.MethodGet, "/", nil), err)
	if rec.Code != wantStatus {
		t.Errorf("status = %d, want %d", rec.Code, wantStatus)
	}
	var body httputil.ErrorResponse
	if decodeErr := json.NewDecoder(rec.Body).Decode(&body); decodeErr != nil {
		t.Fatalf("decode body: %v", decodeErr)
	}
	if body.Code != wantCode {
		t.Errorf("code = %q, want %q", body.Code, wantCode)
	}
}
