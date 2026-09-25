package handler

import (
	"altune/go-api/internal/admin/requeststore"
	"altune/go-api/internal/auth"
	"altune/go-api/internal/shared"
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

func rerunRequest() *http.Request {
	return httptest.NewRequest(http.MethodPost, "/rerun", strings.NewReader(`{"query":"kendrick"}`))
}

// asOperator mounts h behind a fixed principal, the way the production admin
// gate does, so the per-operator bucket has a principal to key on.
func asOperator(h *AdminHandler, operator shared.UserId) chi.Router {
	r := chi.NewRouter()
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			next.ServeHTTP(w, req.WithContext(auth.ContextWithUserID(req.Context(), operator)))
		})
	})
	h.RegisterData(r)
	return r
}

func retryAfterSeconds(t *testing.T, rec *httptest.ResponseRecorder) int {
	t.Helper()
	header := rec.Header().Get("Retry-After")
	seconds, err := strconv.Atoi(header)
	if err != nil {
		t.Fatalf("Retry-After = %q, want whole seconds", header)
	}
	return seconds
}

// TestInspectorRoutes_shedTheThirdConcurrentReplay reproduces #1996: the three
// inspector routes ran the real provider fan-out with nothing capping how many
// ran at once, so N concurrent operator calls put N fan-outs on Apple,
// SoundCloud and MusicBrainz at the same time. Past the cap the caller must be
// refused with a coded 429 and a Retry-After — and the slot must come back when
// the running replay finishes.
func TestInspectorRoutes_shedTheThirdConcurrentReplay(t *testing.T) {

	running := make(chan struct{}, maxConcurrentReplays)
	finish := make(chan struct{})
	blocking := func(context.Context, string, []string) (requeststore.ReRunResult, error) {
		running <- struct{}{}
		select {
		case <-finish:
		// A replay admitted past the cap must not wedge the suite: it answers
		// late instead, and the assertion below is what fails.
		case <-time.After(3 * time.Second):
		}
		return requeststore.ReRunResult{}, nil
	}

	r := chi.NewRouter()
	New(nil, nil).WithReRunner(blocking).RegisterData(r)

	var inFlight sync.WaitGroup
	for range maxConcurrentReplays {
		inFlight.Add(1)
		go func() {
			defer inFlight.Done()
			r.ServeHTTP(httptest.NewRecorder(), rerunRequest())
		}()
	}
	for range maxConcurrentReplays {
		select {
		case <-running:
		case <-time.After(5 * time.Second):
			t.Fatal("the replays never reached the runner")
		}
	}

	shed := httptest.NewRecorder()
	r.ServeHTTP(shed, rerunRequest())

	if shed.Code != http.StatusTooManyRequests {
		t.Fatalf("replay past the in-flight cap: status = %d, want 429 (body %s)", shed.Code, shed.Body.String())
	}
	if _, code := decodedErrorBody(t, shed.Body.String()); code != "admin.inspector_busy" {
		t.Errorf("code = %q, want admin.inspector_busy", code)
	}
	if seconds := retryAfterSeconds(t, shed); seconds < 1 {
		t.Errorf("Retry-After = %d, want at least 1 second", seconds)
	}

	close(finish)
	inFlight.Wait()

	admitted := httptest.NewRecorder()
	r.ServeHTTP(admitted, rerunRequest())
	if admitted.Code != http.StatusOK {
		t.Errorf("after the replays finished: status = %d, want 200 (body %s)", admitted.Code, admitted.Body.String())
	}
}

// TestInspectorRoutes_throttleOneOperatorsReplayBurst reproduces the other half
// of #1996: a script holding an operator token could drive back-to-back replays
// for as long as it liked, since each one finished before the next began and so
// never met the in-flight cap.
func TestInspectorRoutes_throttleOneOperatorsReplayBurst(t *testing.T) {

	immediate := func(context.Context, string, []string) (requeststore.ReRunResult, error) {
		return requeststore.ReRunResult{}, nil
	}
	r := asOperator(New(nil, nil).WithReRunner(immediate), shared.NewUserId(uuid.New()))

	for i := range replayBurst {
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, rerunRequest())
		if rec.Code != http.StatusOK {
			t.Fatalf("replay %d of the burst: status = %d, want 200 (body %s)", i+1, rec.Code, rec.Body.String())
		}
	}

	throttled := httptest.NewRecorder()
	r.ServeHTTP(throttled, rerunRequest())

	if throttled.Code != http.StatusTooManyRequests {
		t.Fatalf("replay past the burst: status = %d, want 429 (body %s)", throttled.Code, throttled.Body.String())
	}
	if _, code := decodedErrorBody(t, throttled.Body.String()); code != "admin.inspector_throttled" {
		t.Errorf("code = %q, want admin.inspector_throttled", code)
	}
	if seconds := retryAfterSeconds(t, throttled); seconds < 1 {
		t.Errorf("Retry-After = %d, want at least 1 second", seconds)
	}
}

// TestInspectorRoutes_shareTheirBudgetAcrossRoutes pins that the cap belongs to
// the inspector seam rather than to one route: the three replay the same
// pipeline into the same providers, so /search and /rerun-detail must not each
// get their own allowance while /rerun is at the cap.
func TestInspectorRoutes_shareTheirBudgetAcrossRoutes(t *testing.T) {

	running := make(chan struct{}, maxConcurrentReplays)
	finish := make(chan struct{})
	blocking := func(context.Context, string, []string) (requeststore.ReRunResult, error) {
		running <- struct{}{}
		select {
		case <-finish:
		// A replay admitted past the cap must not wedge the suite: it answers
		// late instead, and the assertion below is what fails.
		case <-time.After(3 * time.Second):
		}
		return requeststore.ReRunResult{}, nil
	}
	search := func(context.Context, string, []string) ([]requeststore.ResultRow, error) {
		return nil, nil
	}
	detail := func(context.Context, string) (requeststore.DetailReRunResult, error) {
		return requeststore.DetailReRunResult{}, nil
	}

	r := chi.NewRouter()
	New(nil, nil).WithReRunner(blocking).WithSearchInspector(search).WithDetailReRunner(detail).RegisterData(r)

	var inFlight sync.WaitGroup
	for range maxConcurrentReplays {
		inFlight.Add(1)
		go func() {
			defer inFlight.Done()
			r.ServeHTTP(httptest.NewRecorder(), rerunRequest())
		}()
	}
	for range maxConcurrentReplays {
		select {
		case <-running:
		case <-time.After(5 * time.Second):
			t.Fatal("the replays never reached the runner")
		}
	}

	for _, path := range []string{"/search", "/rerun-detail"} {
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{"query":"kendrick"}`)))
		if rec.Code != http.StatusTooManyRequests {
			t.Errorf("%s while /rerun holds every slot: status = %d, want 429 (body %s)", path, rec.Code, rec.Body.String())
		}
	}

	close(finish)
	inFlight.Wait()
}
