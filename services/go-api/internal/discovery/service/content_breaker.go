package service

import (
	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/discovery/ports"
	"context"
	"errors"
)

var errCircuitOpen = errors.New("provider circuit open")

type breakerCall struct {
	cb       *CircuitBreaker
	provider domain.ProviderName
}

func admitProviderCall(cb *CircuitBreaker, provider domain.ProviderName) (breakerCall, bool) {
	if cb == nil {
		return breakerCall{provider: provider}, true
	}
	if !cb.AllowRequest(provider) {
		return breakerCall{}, false
	}
	return breakerCall{cb: cb, provider: provider}, true
}

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

func (c breakerCall) release() {
	if c.cb == nil {
		return
	}
	c.cb.ReleaseProbe(c.provider)
}

func (c breakerCall) failPanicked(settled *bool) {
	if c.cb == nil || *settled {
		return
	}
	c.cb.RecordFailure(c.provider)
}

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

type httpStatusCoder interface {
	HTTPStatus() int
}

type transportError interface {
	error
	Timeout() bool
}

const (
	statusTooManyRequests = 429
	statusServerErrorMin  = 500
)

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

func circuitOpenContentResponse(providerName domain.ProviderName) *ContentFetchResponse {
	return &ContentFetchResponse{
		ProviderName: providerName,
		Status:       domain.ProviderStatusCircuitOpen,
		Items:        []domain.SearchResult{},
	}
}
