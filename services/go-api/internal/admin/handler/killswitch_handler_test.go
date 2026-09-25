package handler_test

import (
	"altune/go-api/internal/admin/alert"
	"altune/go-api/internal/admin/handler"
	"altune/go-api/internal/observe/evalmeter"
	"altune/go-api/internal/shared"
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
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

// oneJobSwitchboard is a switchboard holding a single registered job, enough to
// drive the /jobs kill switch without the leader ticker.
type oneJobSwitchboard struct {
	name    string
	enabled bool
}

func (s *oneJobSwitchboard) Jobs() []handler.JobStatus {
	return []handler.JobStatus{{Name: s.name, Enabled: s.enabled}}
}

func (s *oneJobSwitchboard) SetJobEnabled(name string, enabled bool) (handler.JobStatus, bool) {
	if name != s.name {
		return handler.JobStatus{}, false
	}
	s.enabled = enabled
	return handler.JobStatus{Name: s.name, Enabled: enabled}, true
}

func captureLogs(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&buf, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return &buf
}

// killSwitchRecords returns the admin.kill_switch audit records in log order.
func killSwitchRecords(t *testing.T, logged string) []map[string]any {
	t.Helper()
	var records []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(logged), "\n") {
		var m map[string]any
		if line == "" || json.Unmarshal([]byte(line), &m) != nil {
			continue
		}
		if m["msg"] == "admin.kill_switch" {
			records = append(records, m)
		}
	}
	return records
}

func assertFlipAudited(t *testing.T, record map[string]any, loop string, paused bool) {
	t.Helper()
	if record["loop"] != loop || record["paused"] != paused {
		t.Errorf("audit record = %v, want loop %q paused %v", record, loop, paused)
	}
}

// TestKillSwitch_EveryLoopAuditsUnderOneEventName guards #1990: whichever loop
// is flipped, the audit record carries the same event name and the same
// loop/paused fields, so an operator greps one thing. The job name stays the
// /jobs extra; actor and at belong to every flip and are asserted in
// killswitch_audit_test.go.
func TestKillSwitch_EveryLoopAuditsUnderOneEventName(t *testing.T) {
	const jobName = "corpus_refresh"
	flips := []struct{ loop, pausePath, resumePath string }{
		{"alert_monitor", "/admin/alerts/pause", "/admin/alerts/resume"},
		{"eval_meter", "/admin/eval/pause", "/admin/eval/resume"},
		{"acquisition", "/admin/acquisition/pause", "/admin/acquisition/resume"},
		{"background_job", "/admin/jobs/" + jobName + "/disable", "/admin/jobs/" + jobName + "/enable"},
	}

	operator := shared.NewUserId(uuid.New())
	srv := mountAdminHandler(allLoopsHandler(jobName), operator.String(), operator, true)
	logs := captureLogs(t)

	for _, f := range flips {
		if code, _ := doAdmin(t, srv, http.MethodPost, f.pausePath); code != http.StatusOK {
			t.Fatalf("POST %s = %d, want 200", f.pausePath, code)
		}
	}
	for _, f := range flips {
		if code, _ := doAdmin(t, srv, http.MethodPost, f.resumePath); code != http.StatusOK {
			t.Fatalf("POST %s = %d, want 200", f.resumePath, code)
		}
	}

	records := killSwitchRecords(t, logs.String())
	if len(records) != 2*len(flips) {
		t.Fatalf("got %d admin.kill_switch records, want %d; logs:\n%s", len(records), 2*len(flips), logs.String())
	}
	for i, f := range flips {
		assertFlipAudited(t, records[i], f.loop, true)
		assertFlipAudited(t, records[i+len(flips)], f.loop, false)
	}

	jobRecord := records[len(flips)-1]
	if jobRecord["job"] != jobName {
		t.Errorf("job audit record = %v, want job %q", jobRecord, jobName)
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
