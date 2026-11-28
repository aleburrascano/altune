package monitoring

import (
	"altune/go-api/internal/platform/monitoring/internal/adapters/handler"
	"altune/go-api/internal/platform/monitoring/internal/adapters/notifier"
	"altune/go-api/internal/platform/monitoring/internal/adapters/persistence"
	"altune/go-api/internal/platform/monitoring/internal/domain"
	"altune/go-api/internal/platform/monitoring/internal/ports"
	"altune/go-api/internal/platform/monitoring/internal/service"
	"altune/go-api/internal/shared/clock"
	"context"
	"fmt"
	"net/http"
)

type (
	Condition                = domain.Condition
	AlertSources             = service.AlertSources
	AcquisitionJob           = ports.AcquisitionJob
	AcquisitionReader        = ports.AcquisitionReader
	AcquisitionTools         = ports.AcquisitionTools
	AlertNotifier            = ports.AlertNotifier
	ClientEventStore         = ports.ClientEventStore
	CoverageEvents           = ports.CoverageEvents
	DiscographyCase          = ports.DiscographyCase
	DiscographyGroupBy       = ports.DiscographyGroupBy
	DiscographyQualityReader = ports.DiscographyQualityReader
	DiscographySuspectRate   = ports.DiscographySuspectRate
	DownloadStatusReport     = ports.DownloadStatusReport
	DependencyHealth         = handler.DependencyHealth
	Deps                     = handler.Deps
	Handler                  = handler.Handler
	JobHealthReader          = ports.JobHealthReader
	JobState                 = domain.JobState
	LatencyBucket            = domain.LatencyBucket
	LeaderCounters           = domain.LeaderCounters
	LeaderReader             = ports.LeaderReader
	LiveSources              = handler.LiveSources
	NotifierSettings         = notifier.Settings
	AlertMonitor             = service.AlertMonitor
	EvalMeter                = service.EvalMeter
	EvalMeterQueryResult     = service.EvalMeterQueryResult
	EvalMeterResult          = service.EvalMeterResult
	EvalMeterRunner          = service.EvalMeterRunner
	EventFeed                = service.EventFeed
	EventTap                 = service.EventTap
	Probes                   = service.Probes
	RequestLatency           = domain.RequestLatency
	RequestLatencyReader     = ports.RequestLatencyReader
	RouteLatency             = domain.RouteLatency
	SnapshotReader           = ports.SnapshotReader
	SourceSessions           = ports.SourceSessions
	StatusClasses            = domain.StatusClasses
)

const (
	DefaultProbeTimeout = service.DefaultProbeTimeout
	EvalMeterInterval   = service.EvalMeterInterval
)

var (
	AlertConditions                      = service.AlertConditions
	CheckDependencies                    = service.CheckDependencies
	EraseClientEventsOfDeletedIdentities = service.EraseClientEventsOfDeletedIdentities
	Gate                                 = handler.Gate
	NewAlertMonitor                      = service.NewAlertMonitor
	NewEvalMeter                         = service.NewEvalMeter
	NewEventFeed                         = service.NewEventFeed
	NewEventTap                          = service.NewEventTap
	NewHealthEndpoint                    = handler.NewEndpoint
	NewLiveMetrics                       = handler.NewLiveMetrics
	NewPostgresClientEventStore          = persistence.NewPostgresClientEventStore
	PruneClientEvents                    = service.PruneClientEvents
)

func NewAlertNotifier(ctx context.Context, s NotifierSettings) AlertNotifier {
	return notifier.Select(ctx, s)
}

func New(now clock.Func, newTicker clock.TickerFunc, d Deps) *Handler {
	return handler.NewObserveHandler(now, newTicker, d)
}

func NewClientEventRoutes(store ClientEventStore, sink handler.ClientEventSink, now clock.Func) (http.Handler, error) {
	recorder, err := service.NewRecorder(store, now)
	if err != nil {
		return nil, fmt.Errorf("client events: %w", err)
	}
	return handler.NewClientEventHandler(recorder, sink, handler.DefaultClientEventLimit, now).Routes(), nil
}
