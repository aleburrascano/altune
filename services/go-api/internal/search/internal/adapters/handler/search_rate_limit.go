package handler

import (
	"altune/go-api/internal/shared/clock"
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

func (l RequestLimit) complete() bool { return l.Max > 0 && l.Window > 0 }

func (l DiscoveryRateLimits) complete() bool {
	return l.Search.complete() && l.Suggest.complete() && l.Events.complete() &&
		l.Content.complete() && l.Favorites.complete() && l.History.complete()
}

const (
	searchPerMinute    = 60
	suggestPerMinute   = 120
	eventsPerMinute    = 300
	contentPerMinute   = 180
	favoritesPerMinute = 60
	historyPerMinute   = 60
)

var DefaultDiscoveryRateLimits = DiscoveryRateLimits{
	Search:    RequestLimit{Max: searchPerMinute, Window: time.Minute},
	Suggest:   RequestLimit{Max: suggestPerMinute, Window: time.Minute},
	Events:    RequestLimit{Max: eventsPerMinute, Window: time.Minute},
	Content:   RequestLimit{Max: contentPerMinute, Window: time.Minute},
	Favorites: RequestLimit{Max: favoritesPerMinute, Window: time.Minute},
	History:   RequestLimit{Max: historyPerMinute, Window: time.Minute},
}

func (l RequestLimit) rateLimit() httpserver.RateLimit {
	return httpserver.RateLimit{Every: l.Window / time.Duration(max(l.Max, 1)), Burst: l.Max}
}

func newUserRateLimiter(limit RequestLimit, now clock.Func) func(http.Handler) http.Handler {
	return requestuser.LimitPerUser(limit.rateLimit(), now, ErrRateLimited, nil)
}

func newClientRateLimiter(limit RequestLimit, now clock.Func) func(http.Handler) http.Handler {
	return httpserver.MustNewUserRateLimiter(limit.rateLimit(), now).
		Middleware(func(r *http.Request) (string, bool) { return requestuser.ClientKey(r), true }, ErrRateLimited, nil)
}
