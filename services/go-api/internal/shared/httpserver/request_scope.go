package httpserver

import (
	"context"
	"sync"
)

type requestScopeKey struct{}

type requestScope struct {
	mu     sync.Mutex
	userID string
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
