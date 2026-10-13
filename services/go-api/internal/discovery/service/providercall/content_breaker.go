package providercall

import (
	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/discovery/ports"
	"context"
	"errors"
)

var errCircuitOpen = errors.New("provider circuit open")

type BreakerCall struct {
	ctx      context.Context
	cb       *CircuitBreaker
	provider domain.ProviderName
}

func AdmitProviderCall(ctx context.Context, cb *CircuitBreaker, provider domain.ProviderName) (BreakerCall, bool) {
	if cb == nil {
		return BreakerCall{ctx: ctx, provider: provider}, true
	}
	if !cb.AllowRequest(ctx, provider) {
		return BreakerCall{}, false
	}
	return BreakerCall{ctx: ctx, cb: cb, provider: provider}, true
}

func (c BreakerCall) settle(callerCtx context.Context, err error) {
	if c.cb == nil {
		return
	}
	switch {
	case err == nil:
		c.cb.RecordSuccess(callerCtx, c.provider)
	case callerCtx.Err() != nil, !isProviderHealthFailure(err):
		c.cb.ReleaseProbe(c.provider)
	default:
		c.cb.RecordFailure(callerCtx, c.provider)
	}
}

func (c BreakerCall) Release() {
	if c.cb == nil {
		return
	}
	c.cb.ReleaseProbe(c.provider)
}

func (c BreakerCall) failPanicked(settled *bool) {
	if c.cb == nil || *settled {
		return
	}
	c.cb.RecordFailure(c.ctx, c.provider)
}

func GuardedFetch[T any](
	callerCtx context.Context,
	cb *CircuitBreaker,
	provider domain.ProviderName,
	call func() (T, error),
) (T, error) {
	c, ok := AdmitProviderCall(callerCtx, cb, provider)
	if !ok {
		var zero T
		return zero, errCircuitOpen
	}
	return RunAdmitted(callerCtx, c, BreakerOutcome, call)
}

func RunAdmitted[T any](
	callerCtx context.Context,
	c BreakerCall,
	outcome func(error) error,
	call func() (T, error),
) (T, error) {
	settled := false
	defer c.failPanicked(&settled)
	res, err := call()
	settled = true
	c.settle(callerCtx, outcome(err))
	return res, err
}

func BreakerOutcome(err error) error {
	if IsPartialResult(err) {
		return nil
	}
	return err
}

func IsPartialResult(err error) bool {
	var partial *domain.PartialResultError
	return errors.As(err, &partial)
}

type httpStatusCoder interface {
	HTTPStatus() int
}

type transportError interface {
	error
	Timeout() bool
}

type persistentAuthFailure interface {
	PersistentAuthFailure() bool
}

type badPayload interface {
	BadPayload() bool
}

const (
	statusTooManyRequests = 429
	statusServerErrorMin  = 500
)

func isProviderHealthFailure(err error) bool {
	if errors.Is(err, context.Canceled) || errors.Is(err, ports.ErrProviderRateLimitQueueTimeout) {
		return false
	}
	var auth persistentAuthFailure
	if errors.As(err, &auth) && auth.PersistentAuthFailure() {
		return true
	}
	var payload badPayload
	if errors.As(err, &payload) && payload.BadPayload() {
		return true
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
