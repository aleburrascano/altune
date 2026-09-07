package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"altune/go-api/internal/auth"
	catdomain "altune/go-api/internal/catalog/domain"
	"altune/go-api/internal/shared"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

var (
	reacquireTestUserUUID = uuid.MustParse("bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb")
	reacquireTestUserId   = shared.NewUserId(reacquireTestUserUUID)
)

var reacquireVerifyAsTestUser = auth.VerifierFunc(func(context.Context, string) (shared.UserId, error) {
	return reacquireTestUserId, nil
})

type reacquireFakeScheduler struct {
	replaced []catdomain.TrackId
}

func (s *reacquireFakeScheduler) ScheduleReplace(_ shared.UserId, trackId catdomain.TrackId) {
	s.replaced = append(s.replaced, trackId)
}

func makeReacquireTrack(userId shared.UserId, title, artist, album string) *catdomain.Track {
	t, _ := catdomain.NewTrack(userId, title, artist, album)
	return t
}

func makeStreamableReacquireTrack(userId shared.UserId, title, artist, album, audioRef string) *catdomain.Track {
	t := makeReacquireTrack(userId, title, artist, album)
	_ = t.MarkReady(audioRef)
	return t
}

func buildReacquireRouter(trackRepo *retryFakeTrackRepo, scheduler *reacquireFakeScheduler) chi.Router {
	h := NewReacquireHandler(trackRepo, scheduler)
	r := chi.NewRouter()
	r.Use(auth.Middleware(reacquireVerifyAsTestUser))
	r.Post("/tracks/{trackId}/reacquire", h.HandleReacquire)
	return r
}

func TestHandleReacquire(t *testing.T) {
	tests := []struct {
		name         string
		setup        func(*retryFakeTrackRepo) string
		wantStatus   int
		wantReplaced bool
	}{
		{
			name: "streamable track returns 202 and schedules replace",
			setup: func(repo *retryFakeTrackRepo) string {
				track := makeStreamableReacquireTrack(reacquireTestUserId, "Ready Song", "Artist", "Album", "audio/ready.opus")
				repo.seed(track)
				return track.ID.UUID().String()
			},
			wantStatus:   http.StatusAccepted,
			wantReplaced: true,
		},
		{
			name: "pending track (no audio) returns 409",
			setup: func(repo *retryFakeTrackRepo) string {
				track := makeReacquireTrack(reacquireTestUserId, "Pending Song", "Artist", "Album")
				repo.seed(track)
				return track.ID.UUID().String()
			},
			wantStatus: http.StatusConflict,
		},
		{
			name: "failed track (no audio) returns 409",
			setup: func(repo *retryFakeTrackRepo) string {
				track := makeReacquireTrack(reacquireTestUserId, "Failed Song", "Artist", "Album")
				_ = track.MarkFailed("download error")
				repo.seed(track)
				return track.ID.UUID().String()
			},
			wantStatus: http.StatusConflict,
		},
		{
			name: "not found returns 404",
			setup: func(repo *retryFakeTrackRepo) string {
				return uuid.New().String()
			},
			wantStatus: http.StatusNotFound,
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
			repo := newRetryFakeTrackRepo()
			scheduler := &reacquireFakeScheduler{}
			trackId := tt.setup(repo)
			router := buildReacquireRouter(repo, scheduler)

			rec := retryServe(t, router, http.MethodPost, "/tracks/"+trackId+"/reacquire")

			retryAssertStatus(t, rec, tt.wantStatus)

			if tt.wantReplaced && len(scheduler.replaced) == 0 {
				t.Error("expected scheduler to be called, but no track was scheduled for replace")
			}
			if !tt.wantReplaced && len(scheduler.replaced) > 0 {
				t.Errorf("expected no scheduling, but %d tracks were scheduled for replace", len(scheduler.replaced))
			}
		})
	}
}

func TestHandleReacquire_Cooldown(t *testing.T) {
	repo := newRetryFakeTrackRepo()
	scheduler := &reacquireFakeScheduler{}
	track := makeStreamableReacquireTrack(reacquireTestUserId, "Ready Song", "Artist", "Album", "audio/ready.opus")
	repo.seed(track)
	router := buildReacquireRouter(repo, scheduler)
	path := "/tracks/" + track.ID.UUID().String() + "/reacquire"

	first := retryServe(t, router, http.MethodPost, path)
	retryAssertStatus(t, first, http.StatusAccepted)

	second := retryServe(t, router, http.MethodPost, path)
	retryAssertStatus(t, second, http.StatusTooManyRequests)

	if len(scheduler.replaced) != 1 {
		t.Errorf("expected exactly one scheduled replace, got %d", len(scheduler.replaced))
	}
}

func TestHandleReacquire_NoAuth(t *testing.T) {
	repo := newRetryFakeTrackRepo()
	scheduler := &reacquireFakeScheduler{}
	router := buildReacquireRouter(repo, scheduler)

	req := httptest.NewRequest(http.MethodPost, "/tracks/"+uuid.New().String()+"/reacquire", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	retryAssertStatus(t, rec, http.StatusUnauthorized)
}
