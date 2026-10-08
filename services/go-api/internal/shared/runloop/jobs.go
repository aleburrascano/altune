package runloop

import (
	"sort"
	"sync"
	"sync/atomic"
	"time"
)

// JobControl is the per-job kill switch plus failure counters.
type JobControl struct {
	disabled    atomic.Bool
	failures    atomic.Int64
	consecutive atomic.Int64
	skipped     atomic.Int64
	lastSuccess atomic.Int64
	lastFailure atomic.Int64
}

// Admit reports whether the job may run; a disabled job is counted as skipped.
func (jc *JobControl) Admit() bool {
	if jc.disabled.Load() {
		jc.skipped.Add(1)
		return false
	}
	return true
}

// Record notes the outcome of one run.
func (jc *JobControl) Record(err error) {
	now := time.Now().UnixNano()
	if err != nil {
		jc.failures.Add(1)
		jc.consecutive.Add(1)
		jc.lastFailure.Store(now)
		return
	}
	jc.consecutive.Store(0)
	jc.lastSuccess.Store(now)
}

// DisableIfEnabled disables the job and reports whether it was enabled before.
func (jc *JobControl) DisableIfEnabled() bool {
	return jc.disabled.CompareAndSwap(false, true)
}

func (jc *JobControl) SetEnabled(enabled bool) { jc.disabled.Store(!enabled) }

func (jc *JobControl) Disabled() bool { return jc.disabled.Load() }

func (jc *JobControl) ConsecutiveFailures() int64 { return jc.consecutive.Load() }

type JobHealth struct {
	Name        string
	Enabled     bool
	Failures    int64
	Skipped     int64
	LastSuccess time.Time
	LastFailure time.Time
}

func (jc *JobControl) snapshot(name string) JobHealth {
	return JobHealth{
		Name:        name,
		Enabled:     !jc.disabled.Load(),
		Failures:    jc.failures.Load(),
		Skipped:     jc.skipped.Load(),
		LastSuccess: nanosToTime(jc.lastSuccess.Load()),
		LastFailure: nanosToTime(jc.lastFailure.Load()),
	}
}

func nanosToTime(nanos int64) time.Time {
	if nanos == 0 {
		return time.Time{}
	}
	return time.Unix(0, nanos).UTC()
}

// JobRegistry holds the JobControl of every job by name. The zero value is ready to use.
type JobRegistry struct {
	mu   sync.Mutex
	jobs map[string]*JobControl
}

// Control returns the control for name, creating it on first use.
func (r *JobRegistry) Control(name string) *JobControl {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.jobs == nil {
		r.jobs = make(map[string]*JobControl)
	}
	jc, ok := r.jobs[name]
	if !ok {
		jc = &JobControl{}
		r.jobs[name] = jc
	}
	return jc
}

// SetEnabled flips the switch of a registered job; false means the name is unknown.
func (r *JobRegistry) SetEnabled(name string, enabled bool) (JobHealth, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	jc, ok := r.jobs[name]
	if !ok {
		return JobHealth{}, false
	}
	jc.SetEnabled(enabled)
	return jc.snapshot(name), true
}

// Health snapshots every registered job, sorted by name.
func (r *JobRegistry) Health() []JobHealth {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]JobHealth, 0, len(r.jobs))
	for name, jc := range r.jobs {
		out = append(out, jc.snapshot(name))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}
