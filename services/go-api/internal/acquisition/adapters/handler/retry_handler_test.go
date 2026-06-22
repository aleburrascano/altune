package handler

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"altune/go-api/internal/auth"
	catdomain "altune/go-api/internal/catalog/domain"
	"altune/go-api/internal/shared"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

// --- test constants ---

var (
	retryTestUserUUID = uuid.MustParse("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa")
	retryTestUserId   = shared.NewUserId(retryTestUserUUID)
)

// --- fake token verifier ---

type retryFakeTokenVerifier struct {
	userId shared.UserId
}

func (v *retryFakeTokenVerifier) Verify(_ context.Context, _ string) (shared.UserId, error) {
	return v.userId, nil
}

// --- fake track repo ---

type retryFakeTrackRepo struct {
	tracks map[string]*catdomain.Track
	getErr error
}

func newRetryFakeTrackRepo() *retryFakeTrackRepo {
	return &retryFakeTrackRepo{tracks: make(map[string]*catdomain.Track)}
}

func (r *retryFakeTrackRepo) Add(_ context.Context, track *catdomain.Track) (bool, error) {
	r.tracks[track.ID.String()] = track
	return true, nil
}

func (r *retryFakeTrackRepo) GetByID(_ context.Context, id catdomain.TrackId, userId shared.UserId) (*catdomain.Track, error) {
	if r.getErr != nil {
		return nil, r.getErr
	}
	for _, t := range r.tracks {
		if t.ID == id && t.UserId == userId {
			return t, nil
		}
	}
	return nil, nil
}

func (r *retryFakeTrackRepo) ListForUser(_ context.Context, _ shared.UserId, _, _ int) ([]*catdomain.Track, int, error) {
	return nil, 0, nil
}

func (r *retryFakeTrackRepo) Update(_ context.Context, track *catdomain.Track) error {
	r.tracks[track.ID.String()] = track
	return nil
}

func (r *retryFakeTrackRepo) Delete(_ context.Context, id catdomain.TrackId, _ shared.UserId) (bool, error) {
	key := id.String()
	if _, ok := r.tracks[key]; ok {
		delete(r.tracks, key)
		return true, nil
	}
	return false, nil
}

func (r *retryFakeTrackRepo) GetByDedupKey(_ context.Context, _ shared.UserId, _ string) (*catdomain.Track, error) {
	return nil, nil
}

func (r *retryFakeTrackRepo) seed(t *catdomain.Track) {
	r.tracks[t.ID.String()] = t
}

// --- fake scheduler ---

type retryFakeScheduler struct {
	scheduled []catdomain.TrackId
}

func (s *retryFakeScheduler) Schedule(_ shared.UserId, trackId catdomain.TrackId, _ string) {
	s.scheduled = append(s.scheduled, trackId)
}

// --- helpers ---

func retryServe(t *testing.T, router chi.Router, method, path string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, nil)
	req.Header.Set("Authorization", "Bearer fake-token")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

func retryAssertStatus(t *testing.T, rec *httptest.ResponseRecorder, want int) {
	t.Helper()
	if rec.Code != want {
		t.Errorf("status = %d, want %d (body: %s)", rec.Code, want, rec.Body.String())
	}
}

// suppress unused import warning
var _ = io.NopCloser

func makeRetryTrack(userId shared.UserId, title, artist, album string) *catdomain.Track {
	t, _ := catdomain.NewTrack(userId, title, artist, album)
	return t
}

func makeFailedTrack(userId shared.UserId, title, artist, album string) *catdomain.Track {
	t := makeRetryTrack(userId, title, artist, album)
	_ = t.MarkFailed("download error")
	return t
}

func makeReadyRetryTrack(userId shared.UserId, title, artist, album, audioRef string) *catdomain.Track {
	t := makeRetryTrack(userId, title, artist, album)
	_ = t.MarkReady(audioRef)
	return t
}

func buildRetryRouter(trackRepo *retryFakeTrackRepo, scheduler *retryFakeScheduler) chi.Router {
	h := NewRetryHandler(trackRepo, scheduler)
	r := chi.NewRouter()
	r.Use(auth.Middleware(&retryFakeTokenVerifier{userId: retryTestUserId}))
	r.Post("/tracks/{trackId}/retry", h.HandleRetryAcquisition)
	return r
}

// ==================== Tests ====================

func TestHandleRetryAcquisition(t *testing.T) {
	tests := []struct {
		name           string
		setup          func(*retryFakeTrackRepo) string
		wantStatus     int
		wantScheduled  bool
	}{
		{
			name: "failed track returns 202 and schedules retry",
			setup: func(repo *retryFakeTrackRepo) string {
				track := makeFailedTrack(retryTestUserId, "Failed Song", "Artist", "Album")
				repo.seed(track)
				return track.ID.UUID().String()
			},
			wantStatus:    http.StatusAccepted,
			wantScheduled: true,
		},
		{
			name: "not found returns 404",
			setup: func(repo *retryFakeTrackRepo) string {
				return uuid.New().String()
			},
			wantStatus: http.StatusNotFound,
		},
		{
			name: "pending track (not failed) returns 409",
			setup: func(repo *retryFakeTrackRepo) string {
				track := makeRetryTrack(retryTestUserId, "Pending Song", "Artist", "Album")
				repo.seed(track)
				return track.ID.UUID().String()
			},
			wantStatus: http.StatusConflict,
		},
		{
			name: "ready track (not failed) returns 409",
			setup: func(repo *retryFakeTrackRepo) string {
				track := makeReadyRetryTrack(retryTestUserId, "Ready Song", "Artist", "Album", "audio/ready.opus")
				repo.seed(track)
				return track.ID.UUID().String()
			},
			wantStatus: http.StatusConflict,
		},
		{
			name: "invalid track ID returns 400",
			setup: func(repo *retryFakeTrackRepo) string {
				return "not-a-uuid"
			},
			wantStatus: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Arrange
			repo := newRetryFakeTrackRepo()
			scheduler := &retryFakeScheduler{}
			trackId := tt.setup(repo)
			router := buildRetryRouter(repo, scheduler)

			// Act
			rec := retryServe(t, router, http.MethodPost, "/tracks/"+trackId+"/retry")

			// Assert
			retryAssertStatus(t, rec, tt.wantStatus)

			if tt.wantScheduled && len(scheduler.scheduled) == 0 {
				t.Error("expected scheduler to be called, but no track was scheduled")
			}
			if !tt.wantScheduled && len(scheduler.scheduled) > 0 {
				t.Errorf("expected no scheduling, but %d tracks were scheduled", len(scheduler.scheduled))
			}
		})
	}
}

func TestHandleRetryAcquisition_NoAuth(t *testing.T) {
	// Arrange
	repo := newRetryFakeTrackRepo()
	scheduler := &retryFakeScheduler{}
	router := buildRetryRouter(repo, scheduler)

	// Act
	req := httptest.NewRequest(http.MethodPost, "/tracks/"+uuid.New().String()+"/retry", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	// Assert
	retryAssertStatus(t, rec, http.StatusUnauthorized)
}
