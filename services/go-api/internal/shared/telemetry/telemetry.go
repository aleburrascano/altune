package telemetry

import "altune/go-api/internal/shared/telemetry/internal/adapters/otlp"

type (
	Service     = otlp.Service
	ClientEvent = otlp.ClientEvent
)

const (
	ScopeName        = otlp.ScopeName
	AppVersionHeader = otlp.AppVersionHeader
	PlatformHeader   = otlp.PlatformHeader
)

var (
	Setup           = otlp.Setup
	Middleware      = otlp.Middleware
	NewClientEvents = otlp.NewClientEvents
)
