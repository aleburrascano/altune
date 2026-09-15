package service

import (
	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/discovery/ports"
	"context"
	"errors"
)

// errCircuitOpen is returned by a guarded content call the circuit breaker
// short-circuited: the provider was never called.
var errCircuitOpen = errors.New("provider circuit open")

// breakerCall is one admitted provider call. A nil breaker admits everything
// and records nothing, so services built without one stay ungated.
type breakerCall struct {
	cb       *CircuitBreaker
	provider domain.ProviderName
}

// admitProviderCall asks the breaker whether provider may be called. When it
// admits the call, exactly one of settle, release or failPanicked must follow,
// or a half-open probe slot stays held until its lease expires.
func admitProviderCall(cb *CircuitBreaker, provider domain.ProviderName) (breakerCall, bool) {
	if cb == nil {
		return breakerCall{provider: provider}, true
	}
	if !cb.AllowRequest(provider) {
		return breakerCall{}, false
	}
	return breakerCall{cb: cb, provider: provider}, true
}

// settle records the call's outcome. callerCtx is the request's own context,
// not a fan-out or per-call timeout derived from it: a call abandoned because
// the caller went away says nothing about the provider, so it only hands back
// the probe slot, while a call cut off by our own timeout counts as a failure.
//
// Only errors that speak to the provider's health (transport failures,
// timeouts, 5xx, 429) count. Content calls take a client-supplied external ID,
// so counting every error would let a client trip a provider open for every
// user with a handful of bogus IDs; search shares the same rule. A call shed
// by the provider's rate-limiter queue never reached the provider, and any
// other error proves nothing either way: both release.
func (c breakerCall) settle(callerCtx context.Context, err error) {
	if c.cb == nil {
		return
	}
	switch {
	case err == nil:
		c.cb.RecordSuccess(c.provider)
	case callerCtx.Err() != nil, !isProviderHealthFailure(err):
		c.cb.ReleaseProbe(c.provider)
	default:
		c.cb.RecordFailure(c.provider)
	}
}

// release hands the call back without an outcome, for an admitted call that
// was never made.
func (c breakerCall) release() {
	if c.cb == nil {
		return
	}
	c.cb.ReleaseProbe(c.provider)
}

// failPanicked records a failure for a call that panicked before it could be
// settled. Defer it with a pointer to a flag set once the call returns; it
// does not recover, so the panic still reaches the goroutine's own handler.
func (c breakerCall) failPanicked(settled *bool) {
	if c.cb == nil || *settled {
		return
	}
	c.cb.RecordFailure(c.provider)
}

// guardedFetch runs one provider call through the breaker: it short-circuits
// with errCircuitOpen when the provider's circuit is open and records the
// outcome otherwise.
func guardedFetch[T any](
	callerCtx context.Context,
	cb *CircuitBreaker,
	provider domain.ProviderName,
	call func() (T, error),
) (T, error) {
	c, ok := admitProviderCall(cb, provider)
	if !ok {
		var zero T
		return zero, errCircuitOpen
	}
	settled := false
	defer c.failPanicked(&settled)
	res, err := call()
	settled = true
	c.settle(callerCtx, err)
	return res, err
}

// httpStatusCoder is implemented by provider errors that carry the upstream
// HTTP status of a non-200 response.
type httpStatusCoder interface {
	HTTPStatus() int
}

// transportError matches net.Error (and so *url.Error, which wraps every
// failed round trip) without importing the network stack into this layer.
type transportError interface {
	error
	Timeout() bool
}

const (
	statusTooManyRequests = 429
	statusServerErrorMin  = 500
)

// isProviderHealthFailure reports whether err indicates the provider itself is
// unreachable, slow or failing, as opposed to rejecting this one request.
func isProviderHealthFailure(err error) bool {
	if errors.Is(err, context.Canceled) || errors.Is(err, ports.ErrProviderRateLimitQueueTimeout) {
		return false
	}
	var status httpStatusCoder
	if errors.As(err, &status) {
		code := status.HTTPStatus()
		return code >= statusServerErrorMin || code == statusTooManyRequests
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	var transport transportError
	return errors.As(err, &transport)
}

// circuitOpenContentResponse is the response for a content call skipped
// because the provider's circuit is open.
func circuitOpenContentResponse(providerName domain.ProviderName) *ContentFetchResponse {
	return &ContentFetchResponse{
		ProviderName: providerName,
		Status:       domain.ProviderStatusCircuitOpen,
		Items:        []domain.SearchResult{},
	}
}
