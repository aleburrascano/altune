// Package providerhealth rolls the discovery providers' recent call outcomes
// into the per-provider view the admin console shows. It holds the windowed
// in-memory Store the provider call sites record each call into, and the
// ProviderSnapshot it summarizes a provider's retained samples as: current
// status, per-status counts, average and p95 latency, error rate and
// rate-limit count over the trailing window.
package providerhealth
