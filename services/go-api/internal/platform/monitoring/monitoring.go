package monitoring

import (
	"altune/go-api/internal/platform/monitoring/internal/adapters/handler"
	"altune/go-api/internal/platform/monitoring/internal/adapters/notifier"
	"altune/go-api/internal/platform/monitoring/internal/adapters/persistence"
	"altune/go-api/internal/platform/monitoring/internal/domain"
	"altune/go-api/internal/platform/monitoring/internal/ports"
	"altune/go-api/internal/platform/monitoring/internal/service"
	"altune/go-api/internal/shared/clock"
	"fmt"
	"net/http"
)

type (
	Alert                    = domain.Alert
	Condition                = domain.Condition
	AcquisitionJob           = ports.AcquisitionJob
	AcquisitionReader        = ports.AcquisitionReader
	AcquisitionTools         = ports.AcquisitionTools
	AlertNotifier            = ports.AlertNotifier
	ClientEventStore         = ports.ClientEventStore
	DiscographyCase          = ports.DiscographyCase
	DiscographyGroupBy       = ports.DiscographyGroupBy
	DiscographyQualityReader = ports.DiscographyQualityReader
	DiscographySuspectRate   = ports.DiscographySuspectRate
	DownloadStatusReport     = ports.DownloadStatusReport
	DepStatus                = handler.DepStatus
	DependencyDetail         = handler.DependencyDetail
	DependencyHealth         = handler.DependencyHealth
	Deps                     = handler.Deps
	Handler                  = handler.Handler
	LiveMetrics              = handler.LiveMetrics
	NopNotifier              = notifier.NopNotifier
	AlertMonitor             = service.AlertMonitor
	EvalMeter                = service.EvalMeter
	EvalMeterQueryResult     = service.EvalMeterQueryResult
	EvalMeterResult          = service.EvalMeterResult
	EvalMeterRunner          = service.EvalMeterRunner
	EventFeed                = service.EventFeed
	EventTap                 = service.EventTap
)

const (
	DepDown           = handler.DepDown
	DepNotConfigured  = handler.DepNotConfigured
	DepUp             = handler.DepUp
	EvalMeterInterval = service.EvalMeterInterval
	SeverityWatch     = domain.SeverityWatch
)

var (
	Gate                        = handler.Gate
	NewAlertMonitor             = service.NewAlertMonitor
	NewEmailNotifier            = notifier.NewEmailNotifier
	NewEvalMeter                = service.NewEvalMeter
	NewEventFeed                = service.NewEventFeed
	NewEventTap                 = service.NewEventTap
	NewObserveHandler           = handler.NewObserveHandler
	NewPostgresClientEventStore = persistence.NewPostgresClientEventStore
	NewWebhookNotifier          = notifier.NewWebhookNotifier
)

func NewClientEventRoutes(store ClientEventStore, sink handler.ClientEventSink, now clock.Func) (http.Handler, error) {
	recorder, err := service.NewRecorder(store, now)
	if err != nil {
		return nil, fmt.Errorf("client events: %w", err)
	}
	return handler.NewClientEventHandler(recorder, sink, handler.DefaultClientEventLimit, now).Routes(), nil
}
