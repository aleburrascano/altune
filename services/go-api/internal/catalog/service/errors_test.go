package service

import (
	"altune/go-api/internal/catalog/catalogtest"
	"altune/go-api/internal/catalog/domain"
	"altune/go-api/internal/catalog/ports"
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

func TestSentinelErrorCodes(t *testing.T) {
	cases := []struct {
		err  interface{ ErrorCode() string }
		want string
	}{
		{ErrTrackNotFound, "catalog.track_not_found"},
		{ErrPlaylistNotFound, "catalog.playlist_not_found"},
		{ErrAudioNotAvailable, "catalog.audio_not_available"},
		{ErrAudioOrphaned, "catalog.audio_orphaned"},
		{ErrCatalogTemporarilyUnavailable, "catalog.temporarily_unavailable"},
	}
	for _, c := range cases {
		if got := c.err.ErrorCode(); got != c.want {
			t.Errorf("ErrorCode: got %q, want %q", got, c.want)
		}
	}
}

func TestHotPaths_ClassifyTransientDBFailures(t *testing.T) {
	userId := testUserId()
	cause := context.DeadlineExceeded
	transient := fmt.Errorf("%w: %w", ports.ErrDBTransient, cause)
	permanent := errors.New("relation \"tracks\" does not exist")

	paths := []struct {
		name string
		call func(repo *catalogtest.TrackRepo) error
	}{
		{"stream", func(repo *catalogtest.TrackRepo) error {
			_, err := NewStreamTrackService(repo, catalogtest.NewAudioStore()).Execute(context.Background(), userId, domain.NewTrackId())
			return err
		}},
		{"recover", func(repo *catalogtest.TrackRepo) error {
			return NewStreamTrackService(repo, catalogtest.NewAudioStore()).RecoverIfMissing(context.Background(), userId, domain.NewTrackId())
		}},
		{"status", func(repo *catalogtest.TrackRepo) error {
			_, err := NewGetTrackStatusService(repo).Execute(context.Background(), userId, domain.NewTrackId())
			return err
		}},
	}
	for _, p := range paths {
		t.Run(p.name+"/transient is a retryable 503", func(t *testing.T) {
			repo := catalogtest.NewTrackRepo()
			repo.ErrOnGetBy = transient
			err := p.call(repo)
			if !errors.Is(err, ErrCatalogTemporarilyUnavailable) {
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
			if errors.Is(err, ErrCatalogTemporarilyUnavailable) {
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
	userId := testUserId()
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
			_, err := NewPlaylistLifecycleService(repo).List(ctx, userId, 10, 0)
			return err
		}},
		{"playlist get", func(e error) error {
			repo := catalogtest.NewPlaylistRepo()
			repo.ErrOnGetWithTracks = e
			_, _, err := NewPlaylistLifecycleService(repo).Get(ctx, userId, playlistId)
			return err
		}},
		{"playlist rename write", func(e error) error {
			repo := catalogtest.NewPlaylistRepo()
			p := seedPlaylist(t, repo, userId, "before")
			repo.ErrOnUpdate = e
			_, _, err := NewPlaylistLifecycleService(repo).Rename(ctx, userId, p.ID, "after")
			return err
		}},
		{"membership add track", func(e error) error {
			plRepo := catalogtest.NewPlaylistRepo()
			p := seedPlaylist(t, plRepo, userId, "mine")
			trRepo := catalogtest.NewTrackRepo()
			track := seedTrack(t, trRepo, userId, "T", "A", "B")
			plRepo.ErrOnAddTrack = e
			return NewPlaylistMembershipService(plRepo, trRepo).AddTrack(ctx, userId, p.ID, track.ID)
		}},
		{"membership reorder", func(e error) error {
			plRepo := catalogtest.NewPlaylistRepo()
			plRepo.ErrOnGetTrackOrder = e
			return NewPlaylistMembershipService(plRepo, catalogtest.NewTrackRepo()).Reorder(ctx, userId, playlistId, nil)
		}},
		{"library albums", func(e error) error {
			_, err := NewLibraryLensService(lensErrRepo{err: e}).Albums(ctx, userId, domain.LibraryQuery{})
			return err
		}},
		{"library artists", func(e error) error {
			_, err := NewLibraryLensService(lensErrRepo{err: e}).Artists(ctx, userId, domain.LibraryQuery{})
			return err
		}},
		{"list featuring", func(e error) error {
			_, err := NewListFeaturingService(featuringErrLister{err: e}).Execute(ctx, userId, fa)
			return err
		}},
	}
	for _, p := range paths {
		t.Run(p.name+"/transient is a retryable 503", func(t *testing.T) {
			err := p.call(transient)
			if !errors.Is(err, ErrCatalogTemporarilyUnavailable) {
				t.Fatalf("err = %v, want ErrCatalogTemporarilyUnavailable", err)
			}
			if !errors.Is(err, cause) {
				t.Errorf("err = %v lost its underlying cause", err)
			}
			assertServiceErrorResponse(t, err, http.StatusServiceUnavailable, "catalog.temporarily_unavailable")
		})
		t.Run(p.name+"/permanent stays a 500", func(t *testing.T) {
			err := p.call(permanent)
			if errors.Is(err, ErrCatalogTemporarilyUnavailable) {
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
