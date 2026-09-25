package service

import (
	"altune/go-api/internal/catalog/catalogtest"
	"altune/go-api/internal/catalog/domain"
	"altune/go-api/internal/catalog/ports"
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

// TestHotPaths_ClassifyTransientDBFailures proves the stream, recover and status
// paths turn a GetByID failure the adapter flagged ports.ErrDBTransient into a
// retryable 503 through the shared HTTP error mapping, while an unclassified
// repository failure still surfaces as a 500.
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
