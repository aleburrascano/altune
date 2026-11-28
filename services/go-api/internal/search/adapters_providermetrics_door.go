package search

import searchtelemetry "altune/go-api/internal/search/internal/adapters/telemetry"

type ProviderMetricsSnapshot = searchtelemetry.Snapshot

var (
	NewCountingTransport        = searchtelemetry.NewCountingTransport
	ReadProviderMetricsSnapshot = searchtelemetry.ReadSnapshot
	ProviderRetryRecorder       = searchtelemetry.RetryRecorder
)
