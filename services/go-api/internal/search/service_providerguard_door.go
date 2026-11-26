package search

import providercall "altune/go-api/internal/search/service/providerguard"

type (
	CircuitBreaker       = providercall.CircuitBreaker
	ContentFetchResponse = providercall.ContentFetchResponse
)

var NewCircuitBreaker = providercall.NewCircuitBreaker
