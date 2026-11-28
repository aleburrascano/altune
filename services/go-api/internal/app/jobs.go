package app

import (
	"altune/go-api/internal/download"
	"altune/go-api/internal/library"
	"altune/go-api/internal/platform/leader"
	"altune/go-api/internal/platform/monitoring"
	"altune/go-api/internal/playback"
	"altune/go-api/internal/search"
	"altune/go-api/internal/shared/background"
	"altune/go-api/internal/shared/clock"
	"altune/go-api/internal/shared/logging"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"
)

type jobName string

const (
	jobEvalMeter                jobName = "eval meter"
	jobAlertMonitor             jobName = "alert monitor"
	jobOrphanedAudioReconcile   jobName = "orphaned audio reconcile"
	jobDiscographyEventPrune    jobName = "discography event prune"
	jobSpellingHintsRefresh     jobName = "vocabulary refresh"
	jobBehavioralRankingRefresh jobName = "behavioral ranking refresh"
	jobDeletedUserDataErasure   jobName = "deleted identity erasure"
	jobAcquisitionSourceCanary  jobName = "acquisition source canary"
	jobStreamRecovery           jobName = "stream recovery"
	jobAcquisitionPrune         jobName = "acquisition prune"
	jobAcquisitionUpgrade       jobName = "acquisition upgrade"
	jobUnreleasedRefresh        jobName = "unreleased refresh"
)

type jobSpec struct {
	name          jobName
	alertable     bool
	switchable    bool
	everyInstance bool
}

var jobTable = []jobSpec{
	{name: jobOrphanedAudioReconcile, alertable: true, switchable: true},
	{name: jobDiscographyEventPrune, alertable: true, switchable: true},
	{name: jobSpellingHintsRefresh, alertable: true, switchable: true},
	{name: jobBehavioralRankingRefresh, alertable: true, switchable: true, everyInstance: true},
	{name: jobDeletedUserDataErasure, alertable: true, switchable: false},
	{name: jobAcquisitionSourceCanary, alertable: true, switchable: true},
	{name: jobStreamRecovery, alertable: false, switchable: true},
	{name: jobAcquisitionPrune, alertable: true, switchable: true},
	{name: jobAcquisitionUpgrade, alertable: true, switchable: true},
	{name: jobUnreleasedRefresh, alertable: true, switchable: true},
}

func alertableJobs() []jobName { return namesWhere(func(s jobSpec) bool { return s.alertable }) }

func namesWhere(keep func(jobSpec) bool) []jobName {
	names := make([]jobName, 0, len(jobTable))
	for _, s := range jobTable {
		if keep(s) {
			names = append(names, s.name)
		}
	}
	return names
}

func runsOnEveryInstance(name jobName) bool {
	for _, s := range jobTable {
		if s.name == name {
			return s.everyInstance
		}
	}
	return false
}

func isSwitchable(name jobName) bool {
	for _, s := range jobTable {
		if s.name == name {
			return s.switchable
		}
	}
	return false
}

const (
	backgroundLockKey      int64 = 8_246_113_907_441_002
	backgroundLockInterval       = 10 * time.Second
)

type jobElection interface {
	Start(context.Context)
	Await(context.Context) bool
	LeaderContext(parent context.Context) (ctx context.Context, release context.CancelFunc, ok bool)
	Shutdown(context.Context)
	Counters() leader.Counters
}

func newJobElection(locks leader.LockStore) (*leader.Election, error) {
	r0, err := leader.New(locks, backgroundLockKey, backgroundLockInterval, clock.Now, clock.NewTimer, clock.NewTicker)
	if err != nil {
		return r0, fmt.Errorf("new election: %w", err)
	}
	return r0, nil
}

type jobRunner struct {
	Now       clock.Func
	NewTicker clock.TickerFunc

	loops    background.Tracker
	registry background.JobRegistry
	election jobElection
	led      bool
	starts   []background.NamedStart
}

func (r *jobRunner) DisableAtStartup(ctx context.Context, names []string) error {
	for _, raw := range names {
		name := jobName(raw)
		if !isSwitchable(name) {
			return fmt.Errorf("DISABLED_JOBS: unknown job %q", raw)
		}
		if r.Control(name).DisableIfEnabled() {
			slog.InfoContext(ctx, "job disabled at startup", logging.KeyJob, raw)
		}
	}
	return nil
}

func (r *jobRunner) Background() *background.Tracker {
	return &r.loops
}

func (r *jobRunner) Wait() {
	r.loops.Wait()
}

func (r *jobRunner) Control(name jobName) *background.JobControl {
	return r.registry.Control(string(name))
}

func (r *jobRunner) Switch(name jobName) func() bool {
	return r.Control(name).Admit
}

func (r *jobRunner) SetEnabled(name jobName, enabled bool) (background.JobHealth, bool) {
	return r.registry.SetEnabled(string(name), enabled)
}

func (r *jobRunner) Health() []background.JobHealth {
	return r.registry.Health()
}

func (r *jobRunner) WhenLeader(name jobName, start func(context.Context)) {
	r.starts = append(r.starts, background.NamedStart{Name: string(name), Start: start})
}

func (r *jobRunner) Ticker(name jobName, interval time.Duration, fn func(context.Context) error) {
	if runsOnEveryInstance(name) {
		panic(fmt.Sprintf("jobs: %q runs on every instance, so it registers with EveryInstance, not the leader-only Ticker", name))
	}
	r.requireClock(name)
	r.Control(name)
	r.WhenLeader(name, func(ctx context.Context) {
		run := r.newRun(name, interval)
		if err := background.Every(ctx, &r.loops, interval, r.NewTicker, func() { r.tickAsLeader(ctx, run, fn) }); err != nil {
			slog.ErrorContext(ctx, "job not started", logging.KeyJob, string(name), logging.KeyError, err)
		}
	})
}

func (r *jobRunner) EveryInstance(ctx context.Context, name jobName, interval time.Duration, fn func(context.Context) error) {
	if !runsOnEveryInstance(name) {
		panic(fmt.Sprintf("jobs: %q is leader-only, so it registers with Ticker; only a job the table marks everyInstance may use EveryInstance", name))
	}
	r.requireClock(name)
	run := r.newRun(name, interval)
	if err := background.Every(ctx, &r.loops, interval, r.NewTicker, func() { r.tick(ctx, run, fn) }); err != nil {
		panic(fmt.Sprintf("jobs: %q: %v", name, err))
	}
}

func (r *jobRunner) requireClock(name jobName) {
	if r.Now == nil || r.NewTicker == nil {
		panic(fmt.Sprintf("jobs: %q needs the Runner's Now and NewTicker set before it registers", name))
	}
}

func (r *jobRunner) Lead(ctx context.Context, election jobElection) {
	r.election = election
	r.led = true
	election.Start(ctx)
	background.StartWhenLeader(ctx, &r.loops, election.Await, r.starts)
}

func (r *jobRunner) Election() jobElection {
	return r.election
}

func (r *jobRunner) LeaderCounters() leader.Counters {
	if r.election == nil {
		return leader.Counters{}
	}
	return r.election.Counters()
}

func (r *jobRunner) LeaderContext(ctx context.Context) (context.Context, context.CancelFunc, bool) {
	if !r.led {
		return background.EveryPassLeads(ctx)
	}
	return r.election.LeaderContext(ctx)
}

type jobRun struct {
	control *background.JobControl
	name    jobName
	budget  time.Duration
}

func (r *jobRunner) newRun(name jobName, interval time.Duration) jobRun {
	return jobRun{control: r.Control(name), name: name, budget: background.RunBudget(interval)}
}

func (r *jobRunner) tick(ctx context.Context, run jobRun, fn func(context.Context) error) {
	if !run.control.Admit() {
		return
	}
	background.RunJob(ctx, run.control, r.Now, string(run.name), run.budget, fn)
}

func (r *jobRunner) tickAsLeader(ctx context.Context, run jobRun, fn func(context.Context) error) {
	if !run.control.Admit() {
		return
	}
	leaderCtx, release, ok := r.LeaderContext(ctx)
	if !ok {
		return
	}
	defer release()
	background.RunJob(leaderCtx, run.control, r.Now, string(run.name), run.budget, fn)
}

func (r *jobRunner) StartTicker(ctx context.Context, name jobName, interval time.Duration, run func(context.Context) error, startedAttrs ...any) {
	r.Ticker(name, interval, run)
	slog.InfoContext(ctx, string(name)+" started", startedAttrs...)
}

func warnOnJobFailure(name jobName, run func(context.Context) error) func(context.Context) error {
	return func(ctx context.Context) error {
		if err := run(ctx); err != nil {
			slog.WarnContext(ctx, string(name)+" failed", logging.KeyError, err)
			return err
		}
		return nil
	}
}

func (a *application) startSimpleJob(ctx context.Context, name jobName, interval time.Duration, run func(context.Context) error, startedAttrs ...any) {
	a.jobs.StartTicker(ctx, name, interval, warnOnJobFailure(name, run), startedAttrs...)
}

func (a *application) startCatalogJobs(ctx context.Context, cat catalogWiring, playbackModule *playback.Module, disc *search.Module) {
	a.startOrphanedAudioReconcile(ctx, cat.orphanedAudio, cat.audioStore)
	a.startDeletedIdentityErasure(ctx, playbackModule, disc)
	a.startSourceCanary(ctx, cat.download)
	a.startAcquisitionPrune(ctx, cat.download)
	a.startAcquisitionUpgrade(ctx, cat.download)
}

const orphanedAudioReconcileInterval = 10 * time.Minute

func (a *application) startOrphanedAudioReconcile(ctx context.Context, queue library.OrphanedAudioQueue, audioStore library.AudioStore) {
	if audioStore == nil {
		return
	}
	svc := library.NewReconcileOrphanedAudioService(queue, audioStore,
		library.NewAudioStoreMetrics(a.metrics), a.jobs.Switch(jobOrphanedAudioReconcile))
	a.startSimpleJob(ctx, jobOrphanedAudioReconcile, orphanedAudioReconcileInterval, func(ctx context.Context) error {
		if _, err := svc.Execute(ctx); err != nil {
			return fmt.Errorf("reconcile orphaned audio: %w", err)
		}
		return nil
	}, "interval", orphanedAudioReconcileInterval.String())
}

const deletedIdentityErasureInterval = time.Hour

func (a *application) startDeletedIdentityErasure(ctx context.Context, module *playback.Module, disc *search.Module) {
	a.startSimpleJob(ctx, jobDeletedUserDataErasure, deletedIdentityErasureInterval, func(ctx context.Context) error {
		_, queueErr := module.EraseDeletedUserData(ctx)
		return errors.Join(queueErr, disc.EraseDeletedUserData(ctx), monitoring.EraseClientEventsOfDeletedIdentities(ctx, a.clientEvents))
	}, "interval", deletedIdentityErasureInterval.String())
}

const discographyPruneInterval = 24 * time.Hour

func (a *application) startDiscographyPrune(ctx context.Context, disc *search.Module) {
	clientPrune := monitoring.PruneClientEvents(a.clientEvents, clock.Now)
	a.jobs.StartTicker(ctx, jobDiscographyEventPrune, discographyPruneInterval,
		func(ctx context.Context) error { return errors.Join(disc.PruneEvents(ctx), clientPrune(ctx)) },
		"interval", discographyPruneInterval.String())
}

const acquisitionPruneInterval = 24 * time.Hour

func (a *application) startAcquisitionPrune(ctx context.Context, module *download.Module) {
	a.jobs.StartTicker(ctx, jobAcquisitionPrune, acquisitionPruneInterval,
		module.PruneAcquisitionRows, "interval", acquisitionPruneInterval.String())
}

func (a *application) startVocabularyRefresh(ctx context.Context, vocabStore search.SpellingHintsStore, charts []search.ChartProvider) {
	if vocabStore == nil || len(charts) == 0 {
		return
	}
	const (
		vocabRefreshInterval = 6 * time.Hour
		vocabChartDepth      = 50
	)
	refresh := search.NewVocabularyRefreshService(
		charts, vocabStore, vocabChartDepth,
	)
	a.startSimpleJob(ctx, jobSpellingHintsRefresh, vocabRefreshInterval, refresh.RunOnce)
}

const unreleasedRefreshInterval = 30 * time.Minute

func (a *application) startUnreleasedRefresh(ctx context.Context, disc *search.Module) {
	if !disc.HasUnreleasedRefresh() {
		return
	}
	a.startSimpleJob(ctx, jobUnreleasedRefresh, unreleasedRefreshInterval, disc.RefreshDueUnreleased, "interval", unreleasedRefreshInterval.String())
}

const sourceCanaryInterval = time.Hour

func (a *application) startSourceCanary(ctx context.Context, module *download.Module) {
	if !module.HasSourceChecks() {
		return
	}
	a.startSimpleJob(ctx, jobAcquisitionSourceCanary, sourceCanaryInterval, module.CheckSources,
		"interval", sourceCanaryInterval.String())
}

func (a *application) startAcquisitionUpgrade(ctx context.Context, module *download.Module) {
	plan, interval := module.Upgrade()
	if plan == nil {
		return
	}
	a.jobs.StartTicker(ctx, jobAcquisitionUpgrade, interval, plan,
		"interval", interval.String())
}
