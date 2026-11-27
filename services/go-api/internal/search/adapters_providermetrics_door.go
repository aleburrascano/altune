package search

import providermetrics "altune/go-api/internal/search/internal/adapters/providermetrics"

type ProviderMetricsSnapshot = providermetrics.Snapshot

var (
	NewCountingTransport        = providermetrics.NewCountingTransport
	ReadProviderMetricsSnapshot = providermetrics.ReadSnapshot
	ProviderRetryRecorder       = providermetrics.RetryRecorder
)
