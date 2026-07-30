package service

import (
	"fmt"
	"net/http"
	"sync"
	"time"

	"altune/go-api/internal/shared"
)

type RateLimitedError struct {
	RetryAfter time.Duration
}

func (e *RateLimitedError) Error() string {
	return fmt.Sprintf("too many reports, retry in %s", e.RetryAfter.Round(time.Minute))
}

func (e *RateLimitedError) HTTPStatus() int { return http.StatusTooManyRequests }

type RateLimiter struct {
	mu      sync.Mutex
	window  time.Duration
	limit   int
	now     func() time.Time
	byUser  map[string][]time.Time
	lastGC  time.Time
	gcEvery time.Duration
}

func NewRateLimiter(limit int, window time.Duration) *RateLimiter {
	return &RateLimiter{
		window:  window,
		limit:   limit,
		now:     func() time.Time { return time.Now().UTC() },
		byUser:  map[string][]time.Time{},
		gcEvery: window,
	}
}

func (l *RateLimiter) Allow(userId shared.UserId) error {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := l.now()
	l.collect(now)
	key := userId.String()
	recent := after(l.byUser[key], now.Add(-l.window))
	if len(recent) >= l.limit {
		return &RateLimitedError{RetryAfter: recent[0].Add(l.window).Sub(now)}
	}
	l.byUser[key] = append(recent, now)
	return nil
}

func (l *RateLimiter) collect(now time.Time) {
	if now.Sub(l.lastGC) < l.gcEvery {
		return
	}
	l.lastGC = now
	for key, stamps := range l.byUser {
		kept := after(stamps, now.Add(-l.window))
		if len(kept) == 0 {
			delete(l.byUser, key)
			continue
		}
		l.byUser[key] = kept
	}
}

func after(stamps []time.Time, cutoff time.Time) []time.Time {
	for i, stamp := range stamps {
		if stamp.After(cutoff) {
			return stamps[i:]
		}
	}
	return nil
}
