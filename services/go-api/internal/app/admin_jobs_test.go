package app

import (
	"altune/go-api/internal/auth"
	"altune/go-api/internal/shared"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	adminHandler "altune/go-api/internal/admin/handler"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

const (
	jobsTestJob      = "discovery metrics rollup"
	jobsTestJobPath  = "/admin/jobs/discovery%20metrics%20rollup"
	jobsTestInterval = 2 * time.Millisecond
	operatorToken    = "operator-token"
	strangerToken    = "stranger-token"
)

type jobDTO struct {
	Name        string     `json:"name"`
	Enabled     bool       `json:"enabled"`
	Failures    int64      `json:"failures"`
	Skipped     int64      `json:"skipped"`
	LastSuccess *time.Time `json:"last_success"`
}

// jobsAdminServer mounts the production /admin tree (bearer auth, then the
// operator gate) with the production job adapter over a, authenticating
// operatorToken as the operator and strangerToken as some other user.
func jobsAdminServer(t *testing.T, a *App, withJobs bool) http.Handler {
	t.Helper()
	operator := shared.NewUserId(uuid.New())
	stranger := shared.NewUserId(uuid.New())
	verifier := auth.VerifierFunc(func(_ context.Context, token string) (auth.VerifiedToken, error) {
		switch token {
		case operatorToken:
			return auth.VerifiedToken{UserID: operator, ExpiresAt: time.Now().Add(time.Hour)}, nil
		case strangerToken:
			return auth.VerifiedToken{UserID: stranger, ExpiresAt: time.Now().Add(time.Hour)}, nil
		}
		return auth.VerifiedToken{}, errors.New("bad token")
	})
	h := adminHandler.New(nil, nil)
	if withJobs {
		h = h.WithJobs(adminJobs{app: a})
	}
	r := chi.NewRouter()
	mountAdmin(r, verifier, adminPrincipals{operator: operator.String()}, h)
	return r
}

func callAdmin(t *testing.T, srv http.Handler, method, path, token string) (int, []byte) {
	t.Helper()
	req := httptest.NewRequest(method, path, nil)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	return rec.Code, rec.Body.Bytes()
}

func flipJob(t *testing.T, srv http.Handler, action string) jobDTO {
	t.Helper()
	code, body := callAdmin(t, srv, http.MethodPost, jobsTestJobPath+"/"+action, operatorToken)
	if code != http.StatusOK {
		t.Fatalf("POST %s = %d, want 200; body %s", action, code, body)
	}
	var st jobDTO
	if err := json.Unmarshal(body, &st); err != nil {
		t.Fatalf("decode %s response %q: %v", action, body, err)
	}
	return st
}

func listedJob(t *testing.T, srv http.Handler) jobDTO {
	t.Helper()
	code, body := callAdmin(t, srv, http.MethodGet, "/admin/jobs", operatorToken)
	if code != http.StatusOK {
		t.Fatalf("GET /admin/jobs = %d, want 200; body %s", code, body)
	}
	var out struct {
		Jobs []jobDTO `json:"jobs"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatalf("decode jobs %q: %v", body, err)
	}
	for _, j := range out.Jobs {
		if j.Name == jobsTestJob {
			return j
		}
	}
	t.Fatalf("job %q not listed: %s", jobsTestJob, body)
	return jobDTO{}
}

func waitForJob(t *testing.T, srv http.Handler, what string, cond func(jobDTO) bool) jobDTO {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for {
		j := listedJob(t, srv)
		if cond(j) {
			return j
		}
		if time.Now().After(deadline) {
			t.Fatalf("%s: job stayed %+v", what, j)
		}
		time.Sleep(jobsTestInterval)
	}
}

// startCountingJob registers a ticker through the production startTicker path
// and runs the leader-acquired start, as startBackgroundWhenLeader does.
func startCountingJob(t *testing.T, a *App, runs *atomic.Int64) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	a.startTicker(ctx, jobsTestJob, jobsTestInterval, func(context.Context) error {
		runs.Add(1)
		return nil
	})
	t.Cleanup(func() { cancel(); a.wg.Wait() })
	for _, job := range a.backgroundStarts {
		job.start(ctx)
	}
}

// TestAdminJobs_DisableSkipsTicksAndEnableResumes is the regression for #1020:
// the background-job kill switch and health must be reachable through the
// operator-gated admin router, so disabling a job stops its work (each tick
// recorded as skipped), and re-enabling resumes it, without a redeploy.
func TestAdminJobs_DisableSkipsTicksAndEnableResumes(t *testing.T) {
	var logs bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&logs, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })

	var runs atomic.Int64
	a := &App{}
	srv := jobsAdminServer(t, a, true)
	startCountingJob(t, a, &runs)

	waitForJob(t, srv, "job never succeeded", func(j jobDTO) bool { return j.Enabled && j.LastSuccess != nil })

	if st := flipJob(t, srv, "disable"); st.Enabled {
		t.Fatalf("disable response still enabled: %+v", st)
	}
	time.Sleep(20 * time.Millisecond) // let a tick already past the gate finish
	paused := runs.Load()
	skippedFrom := listedJob(t, srv).Skipped
	j := waitForJob(t, srv, "disabled ticks not recorded as skipped", func(j jobDTO) bool { return j.Skipped >= skippedFrom+3 })
	if j.Enabled {
		t.Fatalf("GET /admin/jobs reports disabled job enabled: %+v", j)
	}
	if got := runs.Load(); got != paused {
		t.Fatalf("disabled job kept running: runs %d -> %d", paused, got)
	}

	if st := flipJob(t, srv, "enable"); !st.Enabled {
		t.Fatalf("enable response still disabled: %+v", st)
	}
	deadline := time.Now().Add(3 * time.Second)
	for runs.Load() <= paused+2 {
		if time.Now().After(deadline) {
			t.Fatalf("re-enabled job did not resume: runs stuck at %d", runs.Load())
		}
		time.Sleep(jobsTestInterval)
	}
	if !listedJob(t, srv).Enabled {
		t.Fatal("GET /admin/jobs reports re-enabled job disabled")
	}

	assertKillSwitchAudit(t, logs.String())
}

func assertKillSwitchAudit(t *testing.T, logged string) {
	t.Helper()
	var paused []any
	for _, line := range strings.Split(strings.TrimSpace(logged), "\n") {
		var m map[string]any
		if json.Unmarshal([]byte(line), &m) != nil || m["msg"] != "admin.kill_switch" {
			continue
		}
		if m["loop"] != "background_job" || m["job"] != jobsTestJob || m["actor"] == "unknown" || m["actor"] == nil {
			t.Errorf("malformed kill-switch audit record: %v", m)
		}
		paused = append(paused, m["paused"])
	}
	if len(paused) != 2 || paused[0] != true || paused[1] != false {
		t.Fatalf("kill-switch audit records paused = %v, want [true false]; logs:\n%s", paused, logged)
	}
}

func TestAdminJobs_OperatorOnly(t *testing.T) {
	a := &App{}
	srv := jobsAdminServer(t, a, true)
	a.job(jobsTestJob)

	requests := []struct{ method, path string }{
		{http.MethodGet, "/admin/jobs"},
		{http.MethodPost, jobsTestJobPath + "/enable"},
		{http.MethodPost, jobsTestJobPath + "/disable"}, // last, so a leaky gate leaves it disabled
	}
	for _, rq := range requests {
		if code, _ := callAdmin(t, srv, rq.method, rq.path, ""); code != http.StatusUnauthorized {
			t.Errorf("%s %s unauthenticated = %d, want 401", rq.method, rq.path, code)
		}
		if code, _ := callAdmin(t, srv, rq.method, rq.path, strangerToken); code != http.StatusForbidden {
			t.Errorf("%s %s non-operator = %d, want 403", rq.method, rq.path, code)
		}
	}
	if h := findJobHealth(t, a.JobHealth(), jobsTestJob); !h.Enabled {
		t.Fatalf("rejected caller flipped the kill switch: %+v", h)
	}
}

func TestAdminJobs_UnknownJobAndUnwired(t *testing.T) {
	a := &App{}
	srv := jobsAdminServer(t, a, true)
	code, body := callAdmin(t, srv, http.MethodPost, "/admin/jobs/nope/disable", operatorToken)
	if code != http.StatusNotFound || !strings.Contains(string(body), "admin.job_not_found") {
		t.Errorf("unknown job = %d %s, want 404 admin.job_not_found", code, body)
	}
	if len(a.JobHealth()) != 0 {
		t.Errorf("unknown job name registered a job: %+v", a.JobHealth())
	}

	unwired := jobsAdminServer(t, a, false)
	for _, path := range []string{"/admin/jobs", "/admin/jobs/nope/disable"} {
		method := http.MethodPost
		if path == "/admin/jobs" {
			method = http.MethodGet
		}
		code, body := callAdmin(t, unwired, method, path, operatorToken)
		if code != http.StatusServiceUnavailable || !strings.Contains(string(body), "admin.jobs_unavailable") {
			t.Errorf("unwired %s %s = %d %s, want 503 admin.jobs_unavailable", method, path, code, body)
		}
	}
}
