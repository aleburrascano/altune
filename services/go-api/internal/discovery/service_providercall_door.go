package discovery

import providercall "altune/go-api/internal/discovery/service/providercall"

type (
	CircuitBreaker       = providercall.CircuitBreaker
	ContentFetchResponse = providercall.ContentFetchResponse
)

var NewCircuitBreaker = providercall.NewCircuitBreaker
