package handler

import (
	"altune/go-api/internal/acquisition/service"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"
)

// A cooldown refusal that carries no Retry-After leaves the client guessing the
// window, so both kinds assert the header alongside the 429 and its code.

func TestHandleRetryAcquisition_CooldownSendsRetryAfter(t *testing.T) {
	repo := newRetryFakeTrackRepo()
	track := newTrackOrFatal(t, retryTestUserId)
	_ = track.MarkFailed("download error")
	repo.seed(track)
	router := buildRetryRouter(repo, &retryFakeScheduler{})
	path := "/tracks/" + track.ID.UUID().String() + "/retry"

	retryAssertStatus(t, retryServe(t, router, http.MethodPost, path), http.StatusAccepted)

	assertCooldownRefusal(t, retryServe(t, router, http.MethodPost, path), service.RetryCooldown)
}

func TestHandleReacquire_CooldownSendsRetryAfter(t *testing.T) {
	repo := newRetryFakeTrackRepo()
	track := newTrackOrFatal(t, reacquireTestUserId)
	_ = track.MarkReady("audio/ready.opus")
	repo.seed(track)
	router := buildReacquireRouter(repo, &reacquireFakeScheduler{})
	path := "/tracks/" + track.ID.UUID().String() + "/reacquire"

	retryAssertStatus(t, retryServe(t, router, http.MethodPost, path), http.StatusAccepted)

	assertCooldownRefusal(t, retryServe(t, router, http.MethodPost, path), service.ReacquireCooldown)
}

func assertCooldownRefusal(t *testing.T, rec *httptest.ResponseRecorder, window time.Duration) {
	t.Helper()
	assertCooldownActive(t, rec)
	secs := retryAfterSeconds(t, rec.Header().Get("Retry-After"))
	if secs < 1 || secs > int(window.Seconds()) {
		t.Errorf("Retry-After = %d, want between 1 and %d", secs, int(window.Seconds()))
	}
}

func assertCooldownActive(t *testing.T, rec *httptest.ResponseRecorder) {
	t.Helper()
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want 429 within the cooldown (body: %s)", rec.Code, rec.Body)
	}
	var env struct {
		Code string `json:"code"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("decode body: %v (raw: %s)", err, rec.Body)
	}
	if env.Code != "acquisition.cooldown_active" {
		t.Errorf("code = %q, want acquisition.cooldown_active", env.Code)
	}
}

func retryAfterSeconds(t *testing.T, header string) int {
	t.Helper()
	secs, err := strconv.Atoi(header)
	if err != nil {
		t.Fatalf("Retry-After = %q, want whole seconds: %v", header, err)
	}
	return secs
}
