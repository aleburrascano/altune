package handler

import (
	"altune/go-api/internal/search/domain"
	"altune/go-api/internal/shared/httpserver"
	"altune/go-api/internal/shared/requestuser"
	"net/http"
	"time"
)

type RequestLimit struct {
	Max    int
	Window time.Duration
}

type DiscoveryRateLimits struct {
	Search    RequestLimit
	Suggest   RequestLimit
	Events    RequestLimit
	Content   RequestLimit
	Favorites RequestLimit
	History   RequestLimit
}

var DefaultDiscoveryRateLimits = DiscoveryRateLimits{
	Search:    RequestLimit{Max: 60, Window: time.Minute},
	Suggest:   RequestLimit{Max: 120, Window: time.Minute},
	Events:    RequestLimit{Max: 300, Window: time.Minute},
	Content:   RequestLimit{Max: 180, Window: time.Minute},
	Favorites: RequestLimit{Max: 60, Window: time.Minute},
	History:   RequestLimit{Max: 60, Window: time.Minute},
}

func (l RequestLimit) rateLimit() httpserver.RateLimit {
	return httpserver.RateLimit{Every: l.Window / time.Duration(max(l.Max, 1)), Burst: l.Max}
}

func newUserRateLimiter(limit RequestLimit, now func() time.Time) func(http.Handler) http.Handler {
	return httpserver.NewUserRateLimiter(limit.rateLimit(), now).
		Middleware(httpserver.UserKey(requestuser.UserIDFromContext), domain.ErrRateLimited, nil)
}

func newClientRateLimiter(limit RequestLimit, now func() time.Time) func(http.Handler) http.Handler {
	return httpserver.NewUserRateLimiter(limit.rateLimit(), now).
		Middleware(func(r *http.Request) (string, bool) { return requestuser.ClientKey(r), true }, domain.ErrRateLimited, nil)
}
