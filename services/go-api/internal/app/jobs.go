package app

import (
	"sort"
	"sync/atomic"
	"time"
)

// jobName identifies a background job. It keys the App's job registry and is
// the name an operator passes to the admin job switchboard, so every job is
// declared once here rather than spelled as a literal at its registration site.
// The string values are wire identifiers (GET /admin/jobs lists them, POST
// /admin/jobs/{name}/enable|disable matches on them): renaming one breaks
// operator tooling.
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
	// jobStreamRecovery is not a ticker: it is the request-path recovery that
	// marks a track failed and reschedules its acquisition when a stream finds
	// its audio missing. It shares the job registry so operators flip it through
	// the same /admin/jobs switchboard.
	jobStreamRecovery jobName = "stream recovery"
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

// jobControl carries the runtime kill switch and the health signal for one
// background job. Every field is touched concurrently: the ticker goroutine
// records outcomes while an operator toggles the switch and reads health at
// runtime, so all access goes through atomics.
type jobControl struct {
	disabled    atomic.Bool
	failures    atomic.Int64
	consecutive atomic.Int64
	skipped     atomic.Int64 // ticks that returned early because the kill switch was off
	lastSuccess atomic.Int64 // unix nanoseconds of the last successful run; 0 = never
	lastFailure atomic.Int64 // unix nanoseconds of the last failed run; 0 = never
}

// record folds one run's outcome into the job's health signal.
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

// JobHealth is the queryable snapshot of one background job's kill switch and
// last-success/failure signal, exposed so an operator (or a health probe) can
// tell whether an unattended job is still doing its work.
type JobHealth struct {
	Name        string
	Enabled     bool
	Failures    int64
	Skipped     int64     // ticks skipped by the kill switch
	LastSuccess time.Time // zero when the job has never succeeded
	LastFailure time.Time // zero when the job has never failed
}

// job returns the control block for name, creating it on first use so a job's
// health is queryable from the moment it is registered.
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

// jobSwitch registers name and returns its kill-switch check for work that runs
// outside a ticker. Each call reports whether the job is enabled, counting a
// disabled call as skipped so GET /admin/jobs shows the suppressed work.
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

// SetJobEnabled flips a registered background job's kill switch at runtime and
// returns the job's resulting health snapshot. A disabled job stays registered
// and keeps ticking, but each tick returns early without doing work, so an
// operator can stop a misbehaving job without a redeploy. An unknown name
// reports ok=false and registers nothing, so a mistyped name cannot mint a
// phantom job that appears in JobHealth.
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

// JobHealth returns a snapshot of every registered background job's health,
// ordered by name for stable output.
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

// nanosToTime maps a stored unix-nano timestamp back to a time.Time, keeping the
// "never happened" sentinel (0) as the zero time rather than the unix epoch.
func nanosToTime(nanos int64) time.Time {
	if nanos == 0 {
		return time.Time{}
	}
	return time.Unix(0, nanos).UTC()
}
