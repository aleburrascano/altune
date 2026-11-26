package background

import (
	"sort"
	"sync"
	"sync/atomic"
	"time"
)

type JobControl struct {
	disabled    atomic.Bool
	failures    atomic.Int64
	consecutive atomic.Int64
	skipped     atomic.Int64
	lastSuccess atomic.Int64
	lastFailure atomic.Int64
}

func (jc *JobControl) Admit() bool {
	if jc.disabled.Load() {
		jc.skipped.Add(1)
		return false
	}
	return true
}

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

type JobRegistry struct {
	jobs sync.Map
}

func (r *JobRegistry) Control(name string) *JobControl {
	jc, _ := r.jobs.LoadOrStore(name, &JobControl{})
	return jc.(*JobControl)
}

func (r *JobRegistry) SetEnabled(name string, enabled bool) (JobHealth, bool) {
	stored, ok := r.jobs.Load(name)
	if !ok {
		return JobHealth{}, false
	}
	jc := stored.(*JobControl)
	jc.SetEnabled(enabled)
	return jc.snapshot(name), true
}

func (r *JobRegistry) Health() []JobHealth {
	var out []JobHealth
	r.jobs.Range(func(name, stored any) bool {
		out = append(out, stored.(*JobControl).snapshot(name.(string)))
		return true
	})
	if out == nil {
		out = []JobHealth{}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}
