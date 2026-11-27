package pacing

import (
	"altune/go-api/internal/shared/clock"
	"altune/go-api/internal/shared/expiringcache"
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

const (
	quarterPerSecond rate.Limit = 0.25
	halfPerSecond    rate.Limit = 0.5
	threePerSecond   rate.Limit = 3
	fivePerSecond    rate.Limit = 5

	acoustidBurst = 3
)

var providerRateLimits = map[string]rate.Limit{
	"musicbrainz.org":            1,
	"itunes.apple.com":           quarterPerSecond,
	"ws.audioscrobbler.com":      fivePerSecond,
	"music.youtube.com":          2,
	"api.discogs.com":            1,
	"api.deezer.com":             fivePerSecond,
	"api-v2.soundcloud.com":      threePerSecond,
	"na.web.skill.music.a2z.com": 2,
	"api-partner.spotify.com":    threePerSecond,
	"api.genius.com":             2,
	"sheets.googleapis.com":      2,
	"webservice.fanart.tv":       2,
	"coverartarchive.org":        2,
	"api.music.apple.com":        threePerSecond,
	"open.spotify.com":           threePerSecond,
	"auth.deezer.com":            1,
	"pipe.deezer.com":            threePerSecond,
	"theaudiodb.com":             halfPerSecond,
	"music.amazon.com":           2,
	"music.apple.com":            2,
	"clienttoken.spotify.com":    2,
	"soundcloud.com":             2,
	"imgur.gg":                   2,
	"krakenfiles.com":            2,
	"api.acoustid.org":           threePerSecond,
	"cdnt-preview.dzcdn.net":     1,
	"www.youtube.com":            fivePerSecond,
}

const defaultProviderBurst = 4

var providerBursts = map[string]int{
	"musicbrainz.org":        1,
	"api.discogs.com":        1,
	"api.acoustid.org":       acoustidBurst,
	"cdnt-preview.dzcdn.net": 1,
	"www.youtube.com":        1,
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
	sleep        clock.Sleeper
	boundedQueue bool
	maxWait      time.Duration
}

type Pacer struct {
	now      clock.Func
	sleep    clock.Sleeper
	limiters *expiringcache.Keyed[string, *hostLimiter]
}

const (
	idleHostAfter = 10 * time.Minute
	maxPacedHosts = 1024
)

var (
	ErrClockRequired   = errors.New("pacer needs a clock")
	ErrSleeperRequired = errors.New("pacer needs a sleeper")
)

func New(now clock.Func, sleep clock.Sleeper) (*Pacer, error) {
	if now == nil {
		return nil, ErrClockRequired
	}
	if sleep == nil {
		return nil, fmt.Errorf("pacing.New: %w", ErrSleeperRequired)
	}
	return &Pacer{now: now, sleep: sleep, limiters: expiringcache.NewKeyed[string, *hostLimiter](idleHostAfter, maxPacedHosts, now)}, nil
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
	if err := l.sleep(ctx, delay); err != nil {
		l.release(reservation)
		if errors.Is(shed, errQueueTimeout) && errors.Is(err, context.DeadlineExceeded) {
			return errQueueTimeout
		}
		return err
	}
	return nil
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
	lim, ok := providerRateLimits[host]
	if !ok {
		return nil
	}
	return p.limiters.Get(host, func() *hostLimiter {
		return newHostLimiter(lim, providerBurst(host), boundedQueueHosts[host], p.now, p.sleep)
	})
}

func (l *hostLimiter) TokensAt(t time.Time) float64 { return l.limiter.TokensAt(t) }

func (l *hostLimiter) Burst() int { return l.limiter.Burst() }

func newHostLimiter(lim rate.Limit, burst int, boundedQueue bool, now clock.Func, sleep clock.Sleeper) *hostLimiter {
	interval := time.Duration(float64(time.Second) / float64(lim))
	return &hostLimiter{limiter: rate.NewLimiter(lim, burst), now: now, sleep: sleep, boundedQueue: boundedQueue, maxWait: min(queueDepth*interval, queuetimeout.MaxWait)}
}

func providerBurst(host string) int {
	if b, ok := providerBursts[host]; ok {
		return b
	}
	return defaultProviderBurst
}
