package telemetry

import "altune/go-api/internal/shared/telemetry/internal/adapters/otlp"

type (
	Service      = otlp.Service
	ClientEvent  = otlp.ClientEvent
	ClientEvents = otlp.ClientEvents
)

const (
	ScopeName        = otlp.ScopeName
	AppVersionHeader = otlp.AppVersionHeader
	PlatformHeader   = otlp.PlatformHeader
	AnalyticsHeader  = otlp.AnalyticsHeader
	AnalyticsOff     = otlp.AnalyticsOff
)

var (
	Enabled          = otlp.Enabled
	Setup            = otlp.Setup
	Middleware       = otlp.Middleware
	AnalyticsAllowed = otlp.AnalyticsAllowed
	NewClientEvents  = otlp.NewClientEvents
)
