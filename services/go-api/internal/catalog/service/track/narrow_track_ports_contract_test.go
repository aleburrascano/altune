package track_test

import (
	"altune/go-api/internal/catalog/catalogtest"
	"altune/go-api/internal/catalog/domain"
	"altune/go-api/internal/catalog/ports"
	audioservice "altune/go-api/internal/catalog/service/audio"
	featuredservice "altune/go-api/internal/catalog/service/featured"
	libraryservice "altune/go-api/internal/catalog/service/library"
	playlistservice "altune/go-api/internal/catalog/service/playlist"
	trackservice "altune/go-api/internal/catalog/service/track"
	"altune/go-api/internal/shared"
	"altune/go-api/internal/shared/httputil"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

type stubAdder struct{}

func (stubAdder) Add(context.Context, *domain.Track) (*domain.Track, bool, error) {
	return nil, false, nil
}

type stubGetter struct{ track *domain.Track }

func (g stubGetter) GetByID(context.Context, domain.TrackId, shared.UserId) (*domain.Track, error) {
	return g.track, nil
}

type stubBatchGetter struct{}

func (stubBatchGetter) ListByIDs(context.Context, shared.UserId, []domain.TrackId) ([]*domain.Track, error) {
	return nil, nil
}

type stubLister struct{}

func (stubLister) ListForUser(context.Context, shared.UserId, int, int) ([]*domain.Track, int, error) {
	return nil, 0, nil
}

type stubCounter struct{ held int }

func (c stubCounter) CountForUser(context.Context, shared.UserId, int) (int, error) {
	return c.held, nil
}

type stubUpdater struct{}

func (stubUpdater) Update(context.Context, *domain.Track, int) error { return nil }

type stubNumberSetter struct{}

func (stubNumberSetter) SetTrackNumber(context.Context, domain.TrackId, shared.UserId, int) (bool, error) {
	return true, nil
}

type stubDeleter struct{ deleted bool }

func (d stubDeleter) Delete(context.Context, domain.TrackId, shared.UserId) (bool, *string, error) {
	return d.deleted, nil, nil
}

type stubAudioRefLookup struct{}

func (stubAudioRefLookup) AudioRefInUse(context.Context, string, domain.TrackId) (bool, error) {
	return false, nil
}

func TestServicesDependOnNarrowTrackPorts(t *testing.T) {
	ctx := context.Background()
	userId := catalogtest.UserId()
	store := catalogtest.NewAudioStore()
	want := &domain.Track{Title: "Narrow"}

	got, err := trackservice.NewGetTrackStatusService(stubGetter{track: want}).Execute(ctx, userId, domain.NewTrackId())
	if err != nil || got != want {
		t.Fatalf("GetTrackStatus via getter-only port = (%v, %v), want the stub's track", got, err)
	}
	if err := trackservice.NewDeleteTrackService(struct {
		stubDeleter
		stubAudioRefLookup
	}{stubDeleter: stubDeleter{deleted: true}}, store).Execute(ctx, userId, domain.NewTrackId()); err != nil {
		t.Fatalf("DeleteTrack via deleter-only port: %v", err)
	}
	if _, err := trackservice.NewSetTrackNumberService(struct {
		stubGetter
		stubNumberSetter
	}{}).Execute(ctx, userId, domain.NewTrackId(), 7); err != nil {
		t.Fatalf("SetTrackNumber via setter-only port: %v", err)
	}

	_ = trackservice.NewAddTrackService(struct {
		stubAdder
		stubCounter
		stubUpdater
	}{})
	_ = audioservice.NewAudioURLService(stubBatchGetter{}, store)
	_ = featuredservice.NewBackfillFeaturedService(stubLister{}, nil, nil)
	_ = audioservice.NewStreamTrackService(struct {
		stubGetter
		stubUpdater
	}{}, store)
	_ = playlistservice.NewPlaylistMembershipService(catalogtest.NewPlaylistRepo(), struct {
		stubGetter
		stubBatchGetter
	}{})
}

type featuringErrLister struct {
	ports.FeaturedArtistRepository
	err error
}

func (f featuringErrLister) ListTracksFeaturing(_ context.Context, _ shared.UserId, _ domain.FeaturedArtist) ([]*domain.Track, error) {
	return nil, f.err
}

func TestHotPaths_ClassifyTransientDBFailures(t *testing.T) {
	userId := catalogtest.UserId()
	cause := context.DeadlineExceeded
	transient := fmt.Errorf("%w: %w", ports.ErrDBTransient, cause)
	permanent := errors.New("relation \"tracks\" does not exist")

	paths := []struct {
		name string
		call func(repo *catalogtest.TrackRepo) error
	}{
		{"stream", func(repo *catalogtest.TrackRepo) error {
			_, err := audioservice.NewStreamTrackService(repo, catalogtest.NewAudioStore()).Execute(context.Background(), userId, domain.NewTrackId())
			return err
		}},
		{"recover", func(repo *catalogtest.TrackRepo) error {
			return audioservice.NewStreamTrackService(repo, catalogtest.NewAudioStore()).RecoverIfMissing(context.Background(), userId, domain.NewTrackId())
		}},
		{"status", func(repo *catalogtest.TrackRepo) error {
			_, err := trackservice.NewGetTrackStatusService(repo).Execute(context.Background(), userId, domain.NewTrackId())
			return err
		}},
	}
	for _, p := range paths {
		t.Run(p.name+"/transient is a retryable 503", func(t *testing.T) {
			repo := catalogtest.NewTrackRepo()
			repo.ErrOnGetBy = transient
			err := p.call(repo)
			if !errors.Is(err, domain.ErrCatalogTemporarilyUnavailable) {
				t.Fatalf("err = %v, want ErrCatalogTemporarilyUnavailable", err)
			}
			if !errors.Is(err, cause) {
				t.Errorf("err = %v lost its underlying cause", err)
			}
			assertServiceErrorResponse(t, err, http.StatusServiceUnavailable, "catalog.temporarily_unavailable")
		})
		t.Run(p.name+"/permanent stays a 500", func(t *testing.T) {
			repo := catalogtest.NewTrackRepo()
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
	ports.LibraryLensRepository
	err error
}

func (l lensErrRepo) ListAlbumsForUser(context.Context, shared.UserId, domain.LibraryQuery) ([]domain.AlbumGroup, error) {
	return nil, l.err
}

func (l lensErrRepo) ListArtistsForUser(context.Context, shared.UserId, domain.LibraryQuery) ([]domain.ArtistGroup, error) {
	return nil, l.err
}

func TestPlaylistAndLibraryPaths_ClassifyTransientDBFailures(t *testing.T) {
	ctx := context.Background()
	userId := catalogtest.UserId()
	playlistId := domain.NewPlaylistId()
	cause := context.DeadlineExceeded
	transient := fmt.Errorf("%w: %w", ports.ErrDBTransient, cause)
	permanent := errors.New("relation \"playlists\" does not exist")
	fa := domain.NewFeaturedArtistIdentityOnly("SZA", "", 0)

	paths := []struct {
		name string
		call func(injected error) error
	}{
		{"playlist list", func(e error) error {
			repo := catalogtest.NewPlaylistRepo()
			repo.ErrOnList = e
			_, err := playlistservice.NewPlaylistLifecycleService(repo).List(ctx, userId, 10, 0)
			return err
		}},
		{"playlist get", func(e error) error {
			repo := catalogtest.NewPlaylistRepo()
			repo.ErrOnGetWithTracks = e
			_, _, err := playlistservice.NewPlaylistLifecycleService(repo).Get(ctx, userId, playlistId)
			return err
		}},
		{"playlist rename write", func(e error) error {
			repo := catalogtest.NewPlaylistRepo()
			p := catalogtest.SeedPlaylist(t, repo, userId, "before")
			repo.ErrOnUpdate = e
			_, _, err := playlistservice.NewPlaylistLifecycleService(repo).Rename(ctx, userId, p.ID, "after")
			return err
		}},
		{"membership add track", func(e error) error {
			plRepo := catalogtest.NewPlaylistRepo()
			p := catalogtest.SeedPlaylist(t, plRepo, userId, "mine")
			trRepo := catalogtest.NewTrackRepo()
			track := catalogtest.SeedTrack(t, trRepo, userId, "T", "A", "B")
			plRepo.ErrOnAddTrack = e
			return playlistservice.NewPlaylistMembershipService(plRepo, trRepo).AddTrack(ctx, userId, p.ID, track.ID)
		}},
		{"membership reorder", func(e error) error {
			plRepo := catalogtest.NewPlaylistRepo()
			plRepo.ErrOnGetTrackOrder = e
			return playlistservice.NewPlaylistMembershipService(plRepo, catalogtest.NewTrackRepo()).Reorder(ctx, userId, playlistId, nil)
		}},
		{"library albums", func(e error) error {
			_, err := libraryservice.NewLibraryLensService(lensErrRepo{err: e}).Albums(ctx, userId, domain.LibraryQuery{})
			return err
		}},
		{"library artists", func(e error) error {
			_, err := libraryservice.NewLibraryLensService(lensErrRepo{err: e}).Artists(ctx, userId, domain.LibraryQuery{})
			return err
		}},
		{"list featuring", func(e error) error {
			_, err := featuredservice.NewListFeaturingService(featuringErrLister{err: e}).Execute(ctx, userId, fa)
			return err
		}},
	}
	for _, p := range paths {
		t.Run(p.name+"/transient is a retryable 503", func(t *testing.T) {
			err := p.call(transient)
			if !errors.Is(err, domain.ErrCatalogTemporarilyUnavailable) {
				t.Fatalf("err = %v, want ErrCatalogTemporarilyUnavailable", err)
			}
			if !errors.Is(err, cause) {
				t.Errorf("err = %v lost its underlying cause", err)
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
