package httpserver

import (
	"context"
	"net/http"
	"slices"
	"sync"
)

type requestScopeKey struct{}

type requestScope struct {
	mu        sync.Mutex
	userID    string
	finishers []func(Outcome)
	inner     *http.Request
}

func (s *requestScope) setInner(r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.inner = r
}

func innermostContext(ctx context.Context) context.Context {
	scope, ok := ctx.Value(requestScopeKey{}).(*requestScope)
	if !ok {
		return ctx
	}
	scope.mu.Lock()
	defer scope.mu.Unlock()
	if scope.inner == nil {
		return ctx
	}
	return scope.inner.Context()
}

func (s *requestScope) addFinisher(finish func(Outcome)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.finishers = append(s.finishers, finish)
}

func (s *requestScope) routeFinishers() []func(Outcome) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.finishers)
}

func withRequestScope(ctx context.Context) (context.Context, *requestScope) {
	scope := &requestScope{}
	return context.WithValue(ctx, requestScopeKey{}, scope), scope
}

func (s *requestScope) user() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.userID
}

func RecordRequestUser(ctx context.Context, userID string) {
	scope, ok := ctx.Value(requestScopeKey{}).(*requestScope)
	if !ok {
		return
	}
	scope.mu.Lock()
	defer scope.mu.Unlock()
	scope.userID = userID
}
