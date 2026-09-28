package app

import (
	"sort"
	"sync/atomic"
	"time"
)

type jobName string

const (
	jobEvalMeter                jobName = "eval meter"
	jobAlertMonitor             jobName = "alert monitor"
	jobStalePendingReconcile    jobName = "stale pending reconcile"
	jobOrphanedAudioReconcile   jobName = "orphaned audio reconcile"
	jobBehavioralCorpusRefresh  jobName = "behavioral corpus refresh"
	jobDiscographyEventPrune    jobName = "discography event prune"
	jobVocabularyRefresh        jobName = "vocabulary refresh"
	jobBehavioralRankingRefresh jobName = "behavioral ranking refresh"
	jobDeletedIdentityErasure   jobName = "deleted identity erasure"
	jobAcquisitionSourceCanary  jobName = "acquisition source canary"
	jobStreamRecovery           jobName = "stream recovery"
)

var knownJobNames = []jobName{
	jobEvalMeter,
	jobAlertMonitor,
	jobStalePendingReconcile,
	jobOrphanedAudioReconcile,
	jobBehavioralCorpusRefresh,
	jobDiscographyEventPrune,
	jobVocabularyRefresh,
	jobBehavioralRankingRefresh,
	jobDeletedIdentityErasure,
	jobAcquisitionSourceCanary,
	jobStreamRecovery,
}

func isKnownJobName(name jobName) bool {
	for _, known := range knownJobNames {
		if known == name {
			return true
		}
	}
	return false
}

type jobControl struct {
	disabled    atomic.Bool
	failures    atomic.Int64
	consecutive atomic.Int64
	skipped     atomic.Int64
	lastSuccess atomic.Int64
	lastFailure atomic.Int64
}

func (jc *jobControl) record(err error) {
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

type JobHealth struct {
	Name        string
	Enabled     bool
	Failures    int64
	Skipped     int64
	LastSuccess time.Time
	LastFailure time.Time
}

func (a *App) job(name jobName) *jobControl {
	a.jobsMu.Lock()
	defer a.jobsMu.Unlock()
	if a.jobs == nil {
		a.jobs = make(map[jobName]*jobControl)
	}
	jc, ok := a.jobs[name]
	if !ok {
		jc = &jobControl{}
		a.jobs[name] = jc
	}
	return jc
}

func (a *App) jobSwitch(name jobName) func() bool {
	jc := a.job(name)
	return func() bool {
		if jc.disabled.Load() {
			jc.skipped.Add(1)
			return false
		}
		return true
	}
}

func (a *App) SetJobEnabled(name jobName, enabled bool) (JobHealth, bool) {
	a.jobsMu.Lock()
	defer a.jobsMu.Unlock()
	jc, ok := a.jobs[name]
	if !ok {
		return JobHealth{}, false
	}
	jc.disabled.Store(!enabled)
	return jc.snapshot(name), true
}

func (a *App) JobHealth() []JobHealth {
	a.jobsMu.Lock()
	defer a.jobsMu.Unlock()
	out := make([]JobHealth, 0, len(a.jobs))
	for name, jc := range a.jobs {
		out = append(out, jc.snapshot(name))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func (jc *jobControl) snapshot(name jobName) JobHealth {
	return JobHealth{
		Name:        string(name),
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
