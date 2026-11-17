package goapi

import goapihttp "altune/overseer/internal/goapi/internal/adapters/goapihttp"

type (
	Client                = goapihttp.Client
	Consumer              = goapihttp.Consumer
	LogsConsumer          = goapihttp.LogsConsumer
	RefreshingTokenSource = goapihttp.RefreshingTokenSource
)

var (
	New                      = goapihttp.New
	WithTimeout              = goapihttp.WithTimeout
	NewConsumer              = goapihttp.NewConsumer
	WithConsumerHTTPClient   = goapihttp.WithConsumerHTTPClient
	WithBackoff              = goapihttp.WithBackoff
	NewLogsConsumer          = goapihttp.NewLogsConsumer
	WithLogsBackoff          = goapihttp.WithLogsBackoff
	NewRefreshingTokenSource = goapihttp.NewRefreshingTokenSource
	WithRefreshHTTPClient    = goapihttp.WithRefreshHTTPClient
	WithPasswordGrant        = goapihttp.WithPasswordGrant
)
