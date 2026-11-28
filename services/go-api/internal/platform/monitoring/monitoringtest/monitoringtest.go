package monitoringtest

import (
	"altune/go-api/internal/platform/monitoring/internal/ports"
	"altune/go-api/internal/platform/monitoring/internal/service"
)

const (
	GroupByArtist            = ports.GroupByArtist
	GroupByContaminationBand = ports.GroupByContaminationBand
	GroupByProvider          = ports.GroupByProvider
)

var MustNewEventTap = service.MustNewEventTap
