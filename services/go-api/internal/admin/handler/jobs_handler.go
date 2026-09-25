package handler

import (
	"altune/go-api/internal/shared/httputil"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
)

// The job routes expose the leader ticker's per-job kill switch and health
// signal (stale-pending reconcile, corpus refresh, metrics rollup, vocabulary
// refresh) so an operator can stop a misbehaving background job and inspect its
// failure/success history without a redeploy. Like the alert/eval kill switches
// the state is in-memory and per process, and admin auth is a bearer token, so
// these POSTs need no CSRF token.

var (
	errJobsUnavailable = &codedError{
		msg:    "background jobs not configured",
		status: http.StatusServiceUnavailable,
		code:   "admin.jobs_unavailable",
	}
	errJobNotFound = &codedError{
		msg:    "unknown background job",
		status: http.StatusNotFound,
		code:   "admin.job_not_found",
	}
)

// JobStatus is the wiring-boundary view of one background job's kill switch and
// health signal. Zero times mean "never happened".
type JobStatus struct {
	Name        string
	Enabled     bool
	Failures    int64
	Skipped     int64
	LastSuccess time.Time
	LastFailure time.Time
}

// JobSwitchboard lists background jobs and flips their kill switch. SetJobEnabled
// reports ok=false for a name that is not a registered job.
type JobSwitchboard interface {
	Jobs() []JobStatus
	SetJobEnabled(name string, enabled bool) (JobStatus, bool)
}

type jobStatusDTO struct {
	Name        string     `json:"name"`
	Enabled     bool       `json:"enabled"`
	Failures    int64      `json:"failures"`
	Skipped     int64      `json:"skipped"`
	LastSuccess *time.Time `json:"last_success"`
	LastFailure *time.Time `json:"last_failure"`
}

type jobsDTO struct {
	Jobs []jobStatusDTO `json:"jobs"`
}

func toJobStatusDTO(s JobStatus) jobStatusDTO {
	return jobStatusDTO{
		Name:        s.Name,
		Enabled:     s.Enabled,
		Failures:    s.Failures,
		Skipped:     s.Skipped,
		LastSuccess: optionalTime(s.LastSuccess),
		LastFailure: optionalTime(s.LastFailure),
	}
}

// optionalTime renders a zero time as JSON null rather than year 1.
func optionalTime(t time.Time) *time.Time {
	if t.IsZero() {
		return nil
	}
	return &t
}

// WithJobs exposes the background-job kill switch and health on the
// operator-only /jobs routes.
func (h *AdminHandler) WithJobs(j JobSwitchboard) *AdminHandler {
	h.jobs = j
	return h
}

func (h *AdminHandler) serveJobs(w http.ResponseWriter, r *http.Request) {
	if h.jobs == nil {
		httputil.HandleServiceError(w, r, errJobsUnavailable)
		return
	}
	statuses := h.jobs.Jobs()
	out := jobsDTO{Jobs: make([]jobStatusDTO, 0, len(statuses))}
	for _, s := range statuses {
		out.Jobs = append(out.Jobs, toJobStatusDTO(s))
	}
	httputil.WriteJSON(w, http.StatusOK, out)
}

func (h *AdminHandler) enableJob(w http.ResponseWriter, r *http.Request) {
	h.flipJob(w, r, true)
}

func (h *AdminHandler) disableJob(w http.ResponseWriter, r *http.Request) {
	h.flipJob(w, r, false)
}

// flipJob applies one kill-switch transition to the job named in the path and
// leaves an audit record of who flipped it.
func (h *AdminHandler) flipJob(w http.ResponseWriter, r *http.Request, enabled bool) {
	if h.jobs == nil {
		httputil.HandleServiceError(w, r, errJobsUnavailable)
		return
	}
	name := chi.URLParam(r, "name")
	st, ok := h.jobs.SetJobEnabled(name, enabled)
	if !ok {
		httputil.HandleServiceError(w, r, errJobNotFound)
		return
	}
	auditKillSwitch(r.Context(), "background_job", !st.Enabled, slog.String("job", st.Name))
	httputil.WriteJSON(w, http.StatusOK, toJobStatusDTO(st))
}
