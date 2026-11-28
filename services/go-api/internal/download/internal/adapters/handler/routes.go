package handler

import "github.com/go-chi/chi/v5"

type Routes struct {
	retry     *RetryHandler
	reacquire *ReacquireHandler
}

func NewRoutes(retry *RetryHandler, reacquire *ReacquireHandler) Routes {
	return Routes{retry: retry, reacquire: reacquire}
}

func (rt Routes) Bounded(r chi.Router) {
	if rt.retry == nil || rt.reacquire == nil {
		return
	}
	r.Post("/tracks/{trackId}/reacquire", rt.reacquire.HandleReacquire)
	r.Post("/tracks/{trackId}/retry", rt.retry.HandleRetryAcquisition)
}

func (Routes) LongLived(chi.Router) {}
