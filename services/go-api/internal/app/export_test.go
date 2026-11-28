package app

import (
	"altune/go-api/internal/platform/monitoring"
	"altune/go-api/internal/platform/requestmetrics"
	"altune/go-api/internal/shared/telemetry"
	"context"
)

type (
	Handles        = handles
	Assembly       = composed
	Datastores     = datastores
	JobElection    = jobElection
	JobName        = jobName
	JobRunner      = jobRunner
	RouterSettings = routerSettings
	StopBudgets    = stopBudgets
	StopComponents = stopComponents
	StopOutcome    = stopOutcome
	StopStep       = stopStep
	V1Routes       = v1Routes
)

const (
	JobAcquisitionPrune         = jobAcquisitionPrune
	JobAcquisitionSourceCanary  = jobAcquisitionSourceCanary
	JobAcquisitionUpgrade       = jobAcquisitionUpgrade
	JobAlertMonitor             = jobAlertMonitor
	JobBehavioralRankingRefresh = jobBehavioralRankingRefresh
	JobDeletedUserDataErasure   = jobDeletedUserDataErasure
	JobDiscographyEventPrune    = jobDiscographyEventPrune
	JobEvalMeter                = jobEvalMeter
	JobOrphanedAudioReconcile   = jobOrphanedAudioReconcile
	JobSpellingHintsRefresh     = jobSpellingHintsRefresh
	JobStreamRecovery           = jobStreamRecovery
	JobUnreleasedRefresh        = jobUnreleasedRefresh
	ServerDrainTimeout          = serverDrainTimeout
	StopBackground              = stopBackground
	StopCompleted               = stopCompleted
	StopDetachedTasks           = stopDetachedTasks
	StopEventFeed               = stopEventFeed
	StopLeaderElection          = stopLeaderElection
	StopScheduler               = stopScheduler
	StopSkipped                 = stopSkipped
	StopTimedOut                = stopTimedOut
)

var (
	Assemble          = assemble
	AlertableJobs     = alertableJobs
	BuildRouter       = buildRouter
	CloseDatastores   = closeDatastores
	ClientEventSink   = clientEventSink
	DrainServer       = drainServer
	MountV1           = mountV1
	NewAlertMonitor   = newAlertMonitor
	NewEvalMeter      = newEvalMeter
	NewHTTPServer     = newHTTPServer
	NewJobElection    = newJobElection
	PlanShutdown      = planShutdown
	RunShutdown       = runShutdown
	SearchBaseDeps    = searchBaseDeps
	V1ResponseHeaders = v1ResponseHeaders
	WarnOnJobFailure  = warnOnJobFailure
)

func LiveSourcesOf(metrics *telemetry.Registry, latency *requestmetrics.Registry) monitoring.LiveSources {
	a := &application{metrics: metrics, latency: latency}
	return a.liveSources()
}

func StopOf[T stopper](component T) func(context.Context) { return stopOf(component) }
