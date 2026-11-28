package monitoringtest

import (
	"altune/go-api/internal/platform/monitoring/internal/adapters/handler"
	"altune/go-api/internal/platform/monitoring/internal/adapters/notifier"
	"altune/go-api/internal/platform/monitoring/internal/domain"
	"altune/go-api/internal/platform/monitoring/internal/ports"
	"altune/go-api/internal/platform/monitoring/internal/service"
)

type (
	Alert            = domain.Alert
	AlertStatus      = service.AlertStatus
	DepStatus        = handler.DepStatus
	DependencyDetail = handler.DependencyDetail
	LiveMetrics      = handler.LiveMetrics
	NopNotifier      = notifier.NopNotifier
)

const (
	DepDown                  = handler.DepDown
	DepNotConfigured         = handler.DepNotConfigured
	DepUp                    = handler.DepUp
	GroupByArtist            = ports.GroupByArtist
	GroupByContaminationBand = ports.GroupByContaminationBand
	GroupByProvider          = ports.GroupByProvider
	SeverityWatch            = domain.SeverityWatch
)

var MustNewEventTap = service.MustNewEventTap
