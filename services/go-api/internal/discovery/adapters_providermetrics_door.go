package discovery

import providermetrics "altune/go-api/internal/discovery/internal/adapters/providermetrics"

type ProviderMetricsSnapshot = providermetrics.Snapshot

var (
	NewCountingTransport = providermetrics.NewCountingTransport
	ReadSnapshot         = providermetrics.ReadSnapshot
	RecordRetry          = providermetrics.RecordRetry
)
