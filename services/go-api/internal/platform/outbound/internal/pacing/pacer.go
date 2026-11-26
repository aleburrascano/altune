package pacing

import (
	"altune/go-api/internal/shared/clock"
	"altune/go-api/internal/shared/queuetimeout"
	"context"
	"errors"
	"fmt"
	"math"
	"net/http"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

var providerRateLimits = map[string]rate.Limit{
	"musicbrainz.org":            1,
	"itunes.apple.com":           0.25,
	"ws.audioscrobbler.com":      5,
	"music.youtube.com":          2,
	"api.discogs.com":            1,
	"api.deezer.com":             5,
	"api-v2.soundcloud.com":      3,
	"na.web.skill.music.a2z.com": 2,
	"api-partner.spotify.com":    3,
	"api.genius.com":             2,
	"sheets.googleapis.com":      2,
	"webservice.fanart.tv":       2,
	"coverartarchive.org":        2,
	"api.music.apple.com":        3,
	"open.spotify.com":           3,
	"auth.deezer.com":            1,
	"pipe.deezer.com":            3,
	"theaudiodb.com":             0.5,
	"music.amazon.com":           2,
	"music.apple.com":            2,
	"clienttoken.spotify.com":    2,
	"soundcloud.com":             2,
	"imgur.gg":                   2,
	"krakenfiles.com":            2,
	"api.acoustid.org":           3,
	"www.youtube.com":            5,
}

const defaultProviderBurst = 4

var providerBursts = map[string]int{
	"musicbrainz.org":  1,
	"api.discogs.com":  1,
	"api.acoustid.org": 3,
	"www.youtube.com":  1,
}

const queueDepth = 3

var boundedQueueHosts = map[string]bool{
	"musicbrainz.org":  true,
	"api.discogs.com":  true,
	"itunes.apple.com": true,
}

var errQueueTimeout = fmt.Errorf("%w: %w", queuetimeout.Err, context.DeadlineExceeded)

type hostLimiter struct {
	mu           sync.Mutex
	limiter      *rate.Limiter
	now          clock.Func
	boundedQueue bool
	maxWait      time.Duration
}

type Pacer struct {
	mu       sync.Mutex
	now      clock.Func
	limiters map[string]*hostLimiter
}

var ErrClockRequired = errors.New("pacer needs a clock")

func New(now clock.Func) (*Pacer, error) {
	if now == nil {
		return nil, ErrClockRequired
	}
	return &Pacer{now: now, limiters: map[string]*hostLimiter{}}, nil
}

func (p *Pacer) Wait(req *http.Request) error {
	return p.pace(req, (*hostLimiter).wait)
}

func (p *Pacer) WaitRetry(req *http.Request) error {
	return p.pace(req, (*hostLimiter).waitUncapped)
}

func (p *Pacer) pace(req *http.Request, wait func(*hostLimiter, context.Context) error) error {
	l := p.limiter(req.URL.Host)
	if l == nil {
		return nil
	}
	if err := wait(l, req.Context()); err != nil {
		return fmt.Errorf("wait for a pacing slot for %s: %w", req.URL.Host, err)
	}
	return nil
}

func (l *hostLimiter) waitUncapped(ctx context.Context) error {
	return l.await(ctx, noCap, errDeadlineShort)
}

func (l *hostLimiter) wait(ctx context.Context) error {
	if !l.boundedQueue {
		return l.waitUncapped(ctx)
	}
	return l.await(ctx, l.maxWait, errQueueTimeout)
}

const noCap = time.Duration(math.MaxInt64)

var errDeadlineShort = errors.New("pacing delay would exceed the context deadline")

func (l *hostLimiter) await(ctx context.Context, limit time.Duration, shed error) error {
	reservation, delay, err := l.admit(ctx, limit, shed)
	if err != nil || delay == 0 {
		return err
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		l.release(reservation)
		if errors.Is(shed, errQueueTimeout) && errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return errQueueTimeout
		}
		return ctx.Err()
	}
}

func (l *hostLimiter) admit(ctx context.Context, limit time.Duration, shed error) (*rate.Reservation, time.Duration, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return nil, 0, err
	}
	now := l.now()
	reservation := l.limiter.ReserveN(now, 1)
	delay := reservation.DelayFrom(now)
	if delay > limit || wouldPassDeadline(ctx, now, delay) {
		reservation.CancelAt(now)
		return nil, 0, shed
	}
	return reservation, delay, nil
}

func (l *hostLimiter) release(reservation *rate.Reservation) {
	l.mu.Lock()
	defer l.mu.Unlock()
	reservation.CancelAt(l.now())
}

func wouldPassDeadline(ctx context.Context, now time.Time, delay time.Duration) bool {
	deadline, ok := ctx.Deadline()
	return ok && delay > 0 && now.Add(delay).After(deadline)
}

func (p *Pacer) limiter(host string) *hostLimiter {
	p.mu.Lock()
	defer p.mu.Unlock()
	if l, ok := p.limiters[host]; ok {
		return l
	}
	var l *hostLimiter
	if lim, ok := providerRateLimits[host]; ok {
		l = newHostLimiter(lim, providerBurst(host), boundedQueueHosts[host], p.now)
	}
	p.limiters[host] = l
	return l
}

func newHostLimiter(lim rate.Limit, burst int, boundedQueue bool, now clock.Func) *hostLimiter {
	interval := time.Duration(float64(time.Second) / float64(lim))
	return &hostLimiter{limiter: rate.NewLimiter(lim, burst), now: now, boundedQueue: boundedQueue, maxWait: min(queueDepth*interval, queuetimeout.MaxWait)}
}

func providerBurst(host string) int {
	if b, ok := providerBursts[host]; ok {
		return b
	}
	return defaultProviderBurst
}
