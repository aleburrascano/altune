package pacing

import (
	"altune/go-api/internal/shared/queuetimeout"
	"context"
	"errors"
	"fmt"
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
}

const defaultProviderBurst = 4

var providerBursts = map[string]int{
	"musicbrainz.org": 1,
	"api.discogs.com": 1,
}

const queueDepth = 3

var boundedQueueHosts = map[string]bool{
	"musicbrainz.org":  true,
	"api.discogs.com":  true,
	"itunes.apple.com": true,
}

var errQueueTimeout = fmt.Errorf("%w: %w", queuetimeout.Err, context.DeadlineExceeded)

type hostLimiter struct {
	limiter      *rate.Limiter
	boundedQueue bool
	maxWait      time.Duration
}

type Pacer struct {
	mu       sync.Mutex
	limiters map[string]*hostLimiter
}

func New() *Pacer {
	return &Pacer{limiters: map[string]*hostLimiter{}}
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
	return l.limiter.Wait(ctx)
}

func (l *hostLimiter) wait(ctx context.Context) error {
	if !l.boundedQueue {
		return l.waitUncapped(ctx)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	reservation := l.limiter.Reserve()
	delay := reservation.Delay()
	if delay == 0 {
		return nil
	}
	if delay > l.maxWait || wouldPassDeadline(ctx, delay) {
		reservation.Cancel()
		return errQueueTimeout
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		reservation.Cancel()
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return errQueueTimeout
		}
		return ctx.Err()
	}
}

func wouldPassDeadline(ctx context.Context, delay time.Duration) bool {
	deadline, ok := ctx.Deadline()
	return ok && time.Now().Add(delay).After(deadline)
}

func (p *Pacer) limiter(host string) *hostLimiter {
	p.mu.Lock()
	defer p.mu.Unlock()
	if l, ok := p.limiters[host]; ok {
		return l
	}
	var l *hostLimiter
	if lim, ok := providerRateLimits[host]; ok {
		l = newHostLimiter(lim, providerBurst(host), boundedQueueHosts[host])
	}
	p.limiters[host] = l
	return l
}

func newHostLimiter(lim rate.Limit, burst int, boundedQueue bool) *hostLimiter {
	interval := time.Duration(float64(time.Second) / float64(lim))
	return &hostLimiter{limiter: rate.NewLimiter(lim, burst), boundedQueue: boundedQueue, maxWait: min(queueDepth*interval, queuetimeout.MaxWait)}
}

func providerBurst(host string) int {
	if b, ok := providerBursts[host]; ok {
		return b
	}
	return defaultProviderBurst
}
