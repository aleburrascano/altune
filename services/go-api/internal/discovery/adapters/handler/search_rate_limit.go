package handler

import (
	"altune/go-api/internal/discovery/adapters/authbridge"
	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/shared/httputil"
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

func (l RequestLimit) rateLimit() httputil.RateLimit {
	return httputil.RateLimit{Every: l.Window / time.Duration(max(l.Max, 1)), Burst: l.Max}
}

func newUserRateLimiter(limit RequestLimit, now func() time.Time) func(http.Handler) http.Handler {
	return httputil.NewUserRateLimiter(limit.rateLimit(), now).
		Middleware(httputil.UserKey(authbridge.UserIDFromContext), domain.ErrRateLimited, nil)
}

func newClientRateLimiter(limit RequestLimit, now func() time.Time) func(http.Handler) http.Handler {
	return httputil.NewUserRateLimiter(limit.rateLimit(), now).
		Middleware(func(r *http.Request) (string, bool) { return authbridge.ClientKey(r), true }, domain.ErrRateLimited, nil)
}
