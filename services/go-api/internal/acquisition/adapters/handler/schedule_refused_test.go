package handler

import (
	"altune/go-api/internal/acquisition/service"
	catdomain "altune/go-api/internal/catalog/domain"
	"altune/go-api/internal/shared"
	"encoding/json"
	"net/http"
	"testing"
)

func newTrackOrFatal(t *testing.T, userId shared.UserId) *catdomain.Track {
	t.Helper()
	track, err := catdomain.NewTrack(userId, "Song", "Artist", "Album")
	if err != nil {
		t.Fatalf("new track: %v", err)
	}
	return track
}

// A shed job must not answer 202: the caller sees a 503 with the queue-full
// code, and because nothing was queued the cooldown is not burned, so an
// immediate retry once the queue drains is accepted rather than 429'd.

func TestHandleRetryAcquisition_QueueFullSurfaces503AndKeepsCooldown(t *testing.T) {
	repo := newRetryFakeTrackRepo()
	scheduler := &retryFakeScheduler{err: service.ErrAcquisitionQueueFull}
	track := newTrackOrFatal(t, retryTestUserId)
	_ = track.MarkFailed("download error")
	repo.seed(track)
	router := buildRetryRouter(repo, scheduler)
	path := "/tracks/" + track.ID.UUID().String() + "/retry"

	rec := retryServe(t, router, http.MethodPost, path)
	assertQueueFull(t, rec.Code, rec.Body.Bytes())

	scheduler.err = nil
	retryAssertStatus(t, retryServe(t, router, http.MethodPost, path), http.StatusAccepted)
	if len(scheduler.scheduled) != 1 {
		t.Errorf("scheduled = %d, want 1 (only the post-drain retry queued)", len(scheduler.scheduled))
	}
}

func TestHandleReacquire_QueueFullSurfaces503AndKeepsCooldown(t *testing.T) {
	repo := newRetryFakeTrackRepo()
	scheduler := &reacquireFakeScheduler{err: service.ErrAcquisitionQueueFull}
	track := newTrackOrFatal(t, reacquireTestUserId)
	_ = track.MarkReady("audio/ready.opus")
	repo.seed(track)
	router := buildReacquireRouter(repo, scheduler)
	path := "/tracks/" + track.ID.UUID().String() + "/reacquire"

	rec := retryServe(t, router, http.MethodPost, path)
	assertQueueFull(t, rec.Code, rec.Body.Bytes())

	scheduler.err = nil
	retryAssertStatus(t, retryServe(t, router, http.MethodPost, path), http.StatusAccepted)
	if len(scheduler.replaced) != 1 {
		t.Errorf("replaced = %d, want 1 (only the post-drain reacquire queued)", len(scheduler.replaced))
	}
}

func assertQueueFull(t *testing.T, status int, body []byte) {
	t.Helper()
	if status != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503 on a shed job (body: %s)", status, body)
	}
	var env struct {
		Code string `json:"code"`
	}
	if err := json.Unmarshal(body, &env); err != nil {
		t.Fatalf("decode body: %v (raw: %s)", err, body)
	}
	if env.Code != "acquisition.queue_full" {
		t.Errorf("code = %q, want acquisition.queue_full", env.Code)
	}
}
