package goapi

import "altune/overseer/internal/goapi/internal/core"

type (
	TokenSource        = core.TokenSource
	StaticTokenSource  = core.StaticTokenSource
	SourceDownError    = core.SourceDownError
	APIError           = core.APIError
	TokenError         = core.TokenError
	Status             = core.Status
	Event              = core.Event
	LogRecord          = core.LogRecord
	Health             = core.Health
	OperatorHealth     = core.OperatorHealth
	HealthDetail       = core.HealthDetail
	LiveMetrics        = core.LiveMetrics
	LatencyMetrics     = core.LatencyMetrics
	RouteLatency       = core.RouteLatency
	StatusClasses      = core.StatusClasses
	LatencyBucket      = core.LatencyBucket
	ProviderUsage      = core.ProviderUsage
	ProviderOutcomes   = core.ProviderOutcomes
	EvalStatus         = core.EvalStatus
	EvalQuery          = core.EvalQuery
	AcquisitionStatus  = core.AcquisitionStatus
	DiscographyQuality = core.DiscographyQuality
	DiscographyCase    = core.DiscographyCase
	CredentialHealth   = core.CredentialHealth
)

const (
	StatusConnecting = core.StatusConnecting
	StatusUp         = core.StatusUp
	StatusDown       = core.StatusDown
)

var ErrNoToken = core.ErrNoToken
