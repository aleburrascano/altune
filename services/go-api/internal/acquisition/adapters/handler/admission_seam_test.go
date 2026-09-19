package handler

import (
	"altune/go-api/internal/auth"
	"context"
	"net/http"
	"testing"

	catdomain "altune/go-api/internal/catalog/domain"

	"github.com/go-chi/chi/v5"
)

// admitAlways stands in for the cooldown admissions: it admits every command,
// including the pending track both real admissions refuse, so a 202 here can
// only come from the handler holding this stub.
type admitAlways struct{}

func (admitAlways) Admit(_ context.Context, _ *catdomain.Track, schedule func() error) error {
	return schedule()
}

func TestHandleRetryAcquisition_UsesTheInjectedAdmission(t *testing.T) {
	repo := newRetryFakeTrackRepo()
	track := newTrackOrFatal(t, retryTestUserId)
	repo.seed(track)
	scheduler := &retryFakeScheduler{}
	h := NewRetryHandler(repo, scheduler, admitAlways{})
	router := chi.NewRouter()
	router.Use(auth.Middleware(retryVerifyAsTestUser))
	router.Post("/tracks/{trackId}/retry", h.HandleRetryAcquisition)

	rec := retryServe(t, router, http.MethodPost, "/tracks/"+track.ID.UUID().String()+"/retry")

	retryAssertStatus(t, rec, http.StatusAccepted)
	if len(scheduler.scheduled) != 1 {
		t.Errorf("scheduled = %d tracks, want 1", len(scheduler.scheduled))
	}
}

func TestHandleReacquire_UsesTheInjectedAdmission(t *testing.T) {
	repo := newRetryFakeTrackRepo()
	track := newTrackOrFatal(t, reacquireTestUserId)
	repo.seed(track)
	scheduler := &reacquireFakeScheduler{}
	h := NewReacquireHandler(repo, scheduler, admitAlways{})
	router := chi.NewRouter()
	router.Use(auth.Middleware(reacquireVerifyAsTestUser))
	router.Post("/tracks/{trackId}/reacquire", h.HandleReacquire)

	rec := retryServe(t, router, http.MethodPost, "/tracks/"+track.ID.UUID().String()+"/reacquire")

	retryAssertStatus(t, rec, http.StatusAccepted)
	if len(scheduler.replaced) != 1 {
		t.Errorf("replaced = %d tracks, want 1", len(scheduler.replaced))
	}
}
