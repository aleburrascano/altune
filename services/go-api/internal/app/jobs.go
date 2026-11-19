package app

import "altune/go-api/internal/shared/runloop"

type jobName string

const (
	jobEvalMeter                jobName = "eval meter"
	jobAlertMonitor             jobName = "alert monitor"
	jobOrphanedAudioReconcile   jobName = "orphaned audio reconcile"
	jobBehavioralCorpusRefresh  jobName = "behavioral corpus refresh"
	jobDiscographyEventPrune    jobName = "discography event prune"
	jobVocabularyRefresh        jobName = "vocabulary refresh"
	jobBehavioralRankingRefresh jobName = "behavioral ranking refresh"
	jobDeletedIdentityErasure   jobName = "deleted identity erasure"
	jobAcquisitionSourceCanary  jobName = "acquisition source canary"
	jobStreamRecovery           jobName = "stream recovery"
	jobAcquisitionPrune         jobName = "acquisition prune"
	jobAcquisitionUpgrade       jobName = "acquisition upgrade"
	jobUnreleasedRefresh        jobName = "unreleased refresh"
)

type jobSpec struct {
	name       jobName
	alertable  bool
	switchable bool
}

var jobTable = []jobSpec{
	{name: jobOrphanedAudioReconcile, alertable: true, switchable: true},
	{name: jobBehavioralCorpusRefresh, alertable: true, switchable: true},
	{name: jobDiscographyEventPrune, alertable: true, switchable: true},
	{name: jobVocabularyRefresh, alertable: true, switchable: true},
	{name: jobBehavioralRankingRefresh, alertable: true, switchable: true},
	{name: jobDeletedIdentityErasure, alertable: true, switchable: false},
	{name: jobAcquisitionSourceCanary, alertable: true, switchable: true},
	{name: jobStreamRecovery, alertable: false, switchable: true},
	{name: jobAcquisitionPrune, alertable: true, switchable: true},
	{name: jobAcquisitionUpgrade, alertable: true, switchable: true},
	{name: jobUnreleasedRefresh, alertable: true, switchable: true},
}

var (
	switchableJobNames = jobNamesWhere(func(spec jobSpec) bool { return spec.switchable })
	alertableJobs      = jobNamesWhere(func(spec jobSpec) bool { return spec.alertable })
)

func jobNamesWhere(keep func(jobSpec) bool) []jobName {
	names := make([]jobName, 0, len(jobTable))
	for _, spec := range jobTable {
		if keep(spec) {
			names = append(names, spec.name)
		}
	}
	return names
}

func isSwitchableJobName(name jobName) bool {
	for _, known := range switchableJobNames {
		if known == name {
			return true
		}
	}
	return false
}

func (a *App) job(name jobName) *runloop.JobControl {
	return a.jobs.Control(string(name))
}

func (a *App) jobSwitch(name jobName) func() bool {
	return a.job(name).Admit
}

func (a *App) SetJobEnabled(name jobName, enabled bool) (runloop.JobHealth, bool) {
	return a.jobs.SetEnabled(string(name), enabled)
}

func (a *App) JobHealth() []runloop.JobHealth {
	return a.jobs.Health()
}
