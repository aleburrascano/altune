package handler

import (
	"altune/go-api/internal/platform/monitoring/internal/ports"
	"altune/go-api/internal/platform/monitoring/internal/service"
	"altune/go-api/internal/shared/clock"
	"altune/go-api/internal/shared/logging"
	"time"
)

type LiveMetrics map[string]any

type LiveMetricsSource func() LiveMetrics

type Deps struct {
	Health      HealthProbe
	Logs        *logging.RingBuffer
	Events      *service.EventFeed
	Eval        *service.EvalMeter
	Acquisition ports.AcquisitionReader
	LiveMetrics LiveMetricsSource
	Discography ports.DiscographyQualityReader
	Shutdown    <-chan struct{}
	Streams     StreamBounds
}

type Handler struct {
	now          clock.Func
	newTicker    clock.TickerFunc
	deps         Deps
	probeTimeout time.Duration
	streams      StreamBounds
}

func NewObserveHandler(now clock.Func, newTicker clock.TickerFunc, d Deps) *Handler {
	if now == nil {
		panic("handler.NewObserveHandler: a nil clock")
	}
	if newTicker == nil {
		panic("handler.NewObserveHandler: a nil ticker")
	}
	defaults := DefaultStreamBounds()
	streams := StreamBounds{
		WriteIdle:   positiveOr(d.Streams.WriteIdle, defaults.WriteIdle),
		Heartbeat:   positiveOr(d.Streams.Heartbeat, defaults.Heartbeat),
		MaxLifetime: positiveOr(d.Streams.MaxLifetime, defaults.MaxLifetime),
	}
	return &Handler{now: now, newTicker: newTicker, deps: d, probeTimeout: defaultProbeTimeout, streams: streams}
}

func positiveOr(d, fallback time.Duration) time.Duration {
	if d > 0 {
		return d
	}
	return fallback
}
