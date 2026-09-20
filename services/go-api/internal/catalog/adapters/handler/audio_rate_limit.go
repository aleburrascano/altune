package handler

import (
	"altune/go-api/internal/auth"
	"altune/go-api/internal/catalog/domain"
	"altune/go-api/internal/shared/httputil"
	"math"
	"net/http"
	"strconv"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

// AudioRateLimit is a per-user token bucket: an idle user may make Burst
// requests back to back, then one more per Every. It is the catalog's one
// throttle shape, named for the audio routes it was written for and since
// applied to the row-creating writes as well (#2200).
//
// The limiter is per instance: behind N replicas a user can reach N times the
// budget. A shared (Redis-backed) limiter is a separate, later step.
type AudioRateLimit struct {
	// Every is the steady-state refill interval: one request per Every.
	Every time.Duration
	// Burst is how many requests an idle user may make back to back.
	Burst int
}

// DefaultStreamRateLimit bounds GET /tracks/{id}/audio, where every request is
// an object-storage GetObject. The client only streams through the API when a
// presign failed, but then the native player issues a Range request per track
// start, per buffer refill and per seek (AVPlayer also probes with bytes=0-1).
// A burst of 120 absorbs skipping through a queue and scrubbing a track in one
// go; one request a second sustained is far above any listening session while
// capping an account at 3600 GetObjects an hour per instance.
var DefaultStreamRateLimit = AudioRateLimit{
	Every: time.Second,
	Burst: 120,
}

// DefaultAudioURLRateLimit bounds POST /audio-urls, where every request is a
// track batch lookup plus up to maxAudioURLBatch presigns. The pinned-download
// worker resolves one track per call back to back, so a 200-track offline pin
// batch alone is 200 calls; playback adds one per track change (prefetch) and
// one per presign-window slide. A burst of 300 admits a whole pin batch even
// if every download fails instantly, with headroom for playback alongside it;
// the 4-per-second refill outpaces any real download, so a longer drain never
// runs dry.
var DefaultAudioURLRateLimit = AudioRateLimit{
	Every: 250 * time.Millisecond,
	Burst: 300,
}

// DefaultTrackWriteRateLimit bounds POST /tracks, where every request is an
// INSERT, four trigram index updates and an event publish. "Save all" on an
// album detail fires one call per unowned track, four in flight
// (SAVE_ALL_CONCURRENCY), so the burst has to admit a whole tracklist in one
// tap: 200 covers the largest box set with room for a second album straight
// after, while the two-per-second refill sits far above hand-driven saving and
// caps an account at 7200 inserts an hour per instance.
var DefaultTrackWriteRateLimit = AudioRateLimit{
	Every: 500 * time.Millisecond,
	Burst: 200,
}

// DefaultPlaylistWriteRateLimit bounds creating a playlist and adding tracks to
// one. The client creates playlists by hand and adds tracks through the batch
// route (up to MaxPlaylistBatchSize ids in a single call), so legitimate
// traffic is single requests rather than runs: a burst of 60 absorbs a tapping
// spree, and one per second sustained caps an account at 3600 playlist writes
// an hour per instance.
var DefaultPlaylistWriteRateLimit = AudioRateLimit{
	Every: time.Second,
	Burst: 60,
}

// The refusals the buckets answer with, routed through
// httputil.HandleServiceError so the body carries the same {detail, code}
// envelope as every other error. Reads and writes carry distinct codes because
// the client's answer differs: a throttled listen retries the same request, a
// throttled save must hold back the rows it has not sent yet.
var (
	errAudioRateLimited = &domain.CodedError{
		Msg:    "too many audio requests, try again later",
		Status: http.StatusTooManyRequests,
		Code:   "catalog.audio_rate_limited",
	}
	errWriteRateLimited = &domain.CodedError{
		Msg:    "too many writes, try again later",
		Status: http.StatusTooManyRequests,
		Code:   "catalog.write_rate_limited",
	}
)

type audioUserBucket struct {
	limiter  *rate.Limiter
	lastSeen time.Time
}

// audioRateLimiter is a token bucket per user id. Memory is bounded by the
// users active within one refill window: a bucket idle long enough to have
// refilled completely is indistinguishable from a fresh one, so it is evicted.
type audioRateLimiter struct {
	mu        sync.Mutex
	limit     AudioRateLimit
	refused   error
	now       func() time.Time
	buckets   map[string]*audioUserBucket
	lastSweep time.Time
}

// newAudioRateLimiter builds the bucket the audio reads are served from.
func newAudioRateLimiter(limit AudioRateLimit, now func() time.Time) *audioRateLimiter {
	return newUserRateLimiter(limit, now, errAudioRateLimited)
}

// newWriteRateLimiter builds the same bucket for the routes that create rows,
// so a flood of saves is refused as a write throttle rather than as an audio one.
func newWriteRateLimiter(limit AudioRateLimit, now func() time.Time) *audioRateLimiter {
	return newUserRateLimiter(limit, now, errWriteRateLimited)
}

func newUserRateLimiter(limit AudioRateLimit, now func() time.Time, refused error) *audioRateLimiter {
	if limit.Burst < 1 {
		limit.Burst = 1
	}
	if now == nil {
		now = time.Now
	}
	return &audioRateLimiter{
		limit:   limit,
		refused: refused,
		now:     now,
		buckets: make(map[string]*audioUserBucket),
	}
}

// idleTTL is how long a bucket takes to refill from empty to full.
func (l *audioRateLimiter) idleTTL() time.Duration {
	return l.limit.Every * time.Duration(l.limit.Burst)
}

// allow spends one token for key. When the bucket is empty it reports false
// and how long until the next token, without spending anything.
func (l *audioRateLimiter) allow(key string) (bool, time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := l.now()
	l.sweep(now)
	limiter := l.bucket(key, now)
	if limiter.AllowN(now, 1) {
		return true, 0
	}
	res := limiter.ReserveN(now, 1)
	defer res.CancelAt(now)
	return false, res.DelayFrom(now)
}

// bucket returns key's limiter, creating it on first sight, and marks it seen.
func (l *audioRateLimiter) bucket(key string, now time.Time) *rate.Limiter {
	b, ok := l.buckets[key]
	if !ok {
		b = &audioUserBucket{limiter: rate.NewLimiter(rate.Every(l.limit.Every), l.limit.Burst)}
		l.buckets[key] = b
	}
	b.lastSeen = now
	return b.limiter
}

// sweep drops fully refilled buckets, at most once per idle TTL so the scan
// cost stays amortised across requests.
func (l *audioRateLimiter) sweep(now time.Time) {
	ttl := l.idleTTL()
	if now.Sub(l.lastSweep) < ttl {
		return
	}
	l.lastSweep = now
	for k, b := range l.buckets {
		if now.Sub(b.lastSeen) >= ttl {
			delete(l.buckets, k)
		}
	}
}

// middleware throttles authenticated callers per user id. It must run after
// auth; an unauthenticated request is passed through so the handler's own
// RequireUserID answers it with 401.
func (l *audioRateLimiter) middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		userId, ok := auth.UserIDFromContext(r.Context())
		if !ok {
			next.ServeHTTP(w, r)
			return
		}
		allowed, wait := l.allow(userId.String())
		if !allowed {
			w.Header().Set("Retry-After", audioRetryAfterSeconds(wait))
			httputil.HandleServiceError(w, r, l.refused)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// audioRetryAfterSeconds renders a wait as whole seconds, rounded up and never
// 0 so a client honouring it cannot spin.
func audioRetryAfterSeconds(wait time.Duration) string {
	return strconv.Itoa(max(1, int(math.Ceil(wait.Seconds()))))
}
