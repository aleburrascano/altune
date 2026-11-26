package search

import providercall "altune/go-api/internal/search/service/providerguard"

type (
	CircuitAdmission     = providercall.Admission
	CircuitBreaker       = providercall.CircuitBreaker
	ContentFetchResponse = providercall.ContentFetchResponse
)

var NewCircuitBreaker = providercall.NewCircuitBreaker
