package providerguard

import (
	"altune/go-api/internal/search/domain"
	"altune/go-api/internal/search/internal/core/ports"
	"context"
	"errors"
)

var errCircuitOpen = errors.New("provider circuit open")

type BreakerCall struct {
	ctx      context.Context
	cb       *CircuitBreaker
	provider domain.ProviderName
	adm      Admission
}

func AdmitProviderCall(ctx context.Context, cb *CircuitBreaker, provider domain.ProviderName) (BreakerCall, bool) {
	adm, ok := cb.AllowRequest(ctx, provider)
	if !ok {
		return BreakerCall{}, false
	}
	return BreakerCall{ctx: ctx, cb: cb, provider: provider, adm: adm}, true
}

func (c BreakerCall) settle(callerCtx context.Context, err error) {
	switch {
	case err == nil:
		c.cb.RecordSuccess(callerCtx, c.provider, c.adm)
	case callerCtx.Err() != nil, !isProviderHealthFailure(err):
		c.cb.ReleaseProbe(c.provider, c.adm)
	default:
		c.cb.RecordFailure(callerCtx, c.provider, c.adm)
	}
}

func (c BreakerCall) Release() {
	c.cb.ReleaseProbe(c.provider, c.adm)
}

func (c BreakerCall) failPanicked(settled *bool) {
	if *settled {
		return
	}
	c.cb.RecordFailure(c.ctx, c.provider, c.adm)
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

type upstreamStatusCoder interface {
	UpstreamStatus() int
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
	var status upstreamStatusCoder
	if errors.As(err, &status) {
		code := status.UpstreamStatus()
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
