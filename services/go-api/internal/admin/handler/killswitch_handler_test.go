package handler_test

import (
	"altune/go-api/internal/admin/alert"
	"altune/go-api/internal/admin/evalmeter"
	"altune/go-api/internal/admin/handler"
	"altune/go-api/internal/shared"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

const (
	killSwitchLoopInterval = 5 * time.Millisecond
	killSwitchSettle       = 50 * time.Millisecond
	killSwitchQuiet        = 150 * time.Millisecond
	killSwitchDeadline     = 3 * time.Second
)

// mountAdminHandler builds the /admin group shape of internal/app/admin_wiring.go
// (auth then OperatorOnly) around a caller-supplied handler.
func mountAdminHandler(h *handler.AdminHandler, operatorID string, caller shared.UserId, authed bool) http.Handler {
	r := chi.NewRouter()
	r.Route("/admin", func(ar chi.Router) {
		ar.Group(func(gr chi.Router) {
			gr.Use(injectUser(caller, authed))
			gr.Use(handler.OperatorOnly(operatorID))
			h.RegisterData(gr)
		})
	})
	return r
}

type pausedBody struct {
	Paused bool `json:"paused"`
}

func doAdmin(t *testing.T, srv http.Handler, method, path string) (int, pausedBody) {
	t.Helper()
	req := httptest.NewRequest(method, path, nil)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	var body pausedBody
	if rec.Code == http.StatusOK {
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("%s %s: decode %q: %v", method, path, rec.Body.String(), err)
		}
	}
	return rec.Code, body
}

func waitAbove(t *testing.T, counter *atomic.Int64, floor int64, what string) {
	t.Helper()
	deadline := time.Now().Add(killSwitchDeadline)
	for counter.Load() <= floor {
		if time.Now().After(deadline) {
			t.Fatalf("%s: count stayed at %d, want > %d", what, counter.Load(), floor)
		}
		time.Sleep(killSwitchLoopInterval)
	}
}

// assertKillSwitch drives pause -> ticks skipped -> resume -> ticks run again
// through the operator-gated admin router against a running loop whose ticks
// increment counter.
func assertKillSwitch(t *testing.T, srv http.Handler, counter *atomic.Int64, statusPath, pausePath, resumePath string) {
	t.Helper()
	waitAbove(t, counter, 0, "loop never ticked before pause")

	code, body := doAdmin(t, srv, http.MethodPost, pausePath)
	if code != http.StatusOK || !body.Paused {
		t.Fatalf("POST %s = %d paused=%v, want 200 paused=true", pausePath, code, body.Paused)
	}
	if code, body = doAdmin(t, srv, http.MethodGet, statusPath); code != http.StatusOK || !body.Paused {
		t.Fatalf("GET %s = %d paused=%v, want 200 paused=true", statusPath, code, body.Paused)
	}

	// Let a tick already past the gate finish, then prove no further work runs.
	time.Sleep(killSwitchSettle)
	paused := counter.Load()
	time.Sleep(killSwitchQuiet)
	if got := counter.Load(); got != paused {
		t.Fatalf("paused loop kept ticking: count %d -> %d", paused, got)
	}

	code, body = doAdmin(t, srv, http.MethodPost, resumePath)
	if code != http.StatusOK || body.Paused {
		t.Fatalf("POST %s = %d paused=%v, want 200 paused=false", resumePath, code, body.Paused)
	}
	if code, body = doAdmin(t, srv, http.MethodGet, statusPath); code != http.StatusOK || body.Paused {
		t.Fatalf("GET %s = %d paused=%v, want 200 paused=false", statusPath, code, body.Paused)
	}
	waitAbove(t, counter, paused, "resumed loop did not tick again")
}

func TestAlertKillSwitch_PauseSkipsTicksAndResumeRestores(t *testing.T) {
	var evals atomic.Int64
	m := alert.NewMonitor(alert.NopNotifier{}, killSwitchLoopInterval, alert.Condition{
		Key: "counting",
		Eval: func(context.Context) *alert.Alert {
			evals.Add(1)
			return nil
		},
	})
	ctx, cancel := context.WithCancel(context.Background())
	m.Start(ctx)
	t.Cleanup(func() { cancel(); m.Shutdown(context.Background()) })

	operator := shared.NewUserId(uuid.New())
	srv := mountAdminHandler(handler.New(nil, nil).WithAlertMonitor(m), operator.String(), operator, true)

	assertKillSwitch(t, srv, &evals, "/admin/alerts", "/admin/alerts/pause", "/admin/alerts/resume")
}

func TestEvalKillSwitch_PauseSkipsTicksAndResumeRestores(t *testing.T) {
	var runs atomic.Int64
	m := evalmeter.New(true, killSwitchLoopInterval, func(context.Context) (evalmeter.Result, error) {
		runs.Add(1)
		return evalmeter.Result{Score: 1, Baseline: 1}, nil
	})
	ctx, cancel := context.WithCancel(context.Background())
	m.Start(ctx)
	t.Cleanup(func() { cancel(); m.Shutdown(context.Background()) })

	operator := shared.NewUserId(uuid.New())
	srv := mountAdminHandler(handler.New(nil, nil).WithEvalMeter(m), operator.String(), operator, true)

	assertKillSwitch(t, srv, &runs, "/admin/eval", "/admin/eval/pause", "/admin/eval/resume")
}

func TestKillSwitch_OperatorOnly(t *testing.T) {
	operator := shared.NewUserId(uuid.New())
	other := shared.NewUserId(uuid.New())

	paths := []string{"/admin/alerts/pause", "/admin/alerts/resume", "/admin/eval/pause", "/admin/eval/resume"}
	callers := []struct {
		name       string
		caller     shared.UserId
		authed     bool
		wantStatus int
	}{
		{"unauthenticated", shared.UserId{}, false, http.StatusUnauthorized},
		{"non-operator", other, true, http.StatusForbidden},
	}

	for _, path := range paths {
		for _, c := range callers {
			t.Run(c.name+" "+path, func(t *testing.T) {
				mon := alert.NewMonitor(alert.NopNotifier{}, time.Hour)
				meter := evalmeter.New(true, time.Hour, nil)
				h := handler.New(nil, nil).WithAlertMonitor(mon).WithEvalMeter(meter)
				srv := mountAdminHandler(h, operator.String(), c.caller, c.authed)

				req := httptest.NewRequest(http.MethodPost, path, nil)
				rec := httptest.NewRecorder()
				srv.ServeHTTP(rec, req)

				if rec.Code != c.wantStatus {
					t.Fatalf("status = %d, want %d", rec.Code, c.wantStatus)
				}
				if mon.Paused() || meter.Paused() {
					t.Fatal("rejected caller flipped a kill switch")
				}
			})
		}
	}
}

func TestKillSwitch_UnwiredLoopIsUnavailable(t *testing.T) {
	operator := shared.NewUserId(uuid.New())
	srv := mountAdminHandler(handler.New(nil, nil), operator.String(), operator, true)

	for _, path := range []string{"/admin/alerts/pause", "/admin/alerts/resume", "/admin/eval/pause", "/admin/eval/resume"} {
		req := httptest.NewRequest(http.MethodPost, path, nil)
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, req)
		if rec.Code != http.StatusServiceUnavailable {
			t.Errorf("POST %s = %d, want 503 (body %s)", path, rec.Code, rec.Body.String())
		}
	}

	code, body := doAdmin(t, srv, http.MethodGet, "/admin/alerts")
	if code != http.StatusOK || body.Paused {
		t.Errorf("GET /admin/alerts unwired = %d paused=%v, want 200 paused=false", code, body.Paused)
	}
}
