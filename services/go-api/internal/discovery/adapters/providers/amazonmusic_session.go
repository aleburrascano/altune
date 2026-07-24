package providers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"golang.org/x/sync/singleflight"
)

// --- session resolution ----------------------------------------------------
//
// Amazon Music's web player gates its internal showSearch endpoint on a
// per-session bundle (device id, session id, CSRF token) rather than a login.
// An anonymous GET to config.json mints a fresh one every time — no rotating
// cryptographic secret to reverse-engineer, unlike Spotify's internal API.
// This resolver caches the bundle and re-mints it on invalidate() (an auth
// failure), the same shape as SoundCloud's clientIDResolver.

const amzConfigURL = "https://music.amazon.com/config.json"

// amazonMusicSession is the subset of config.json's response the showSearch
// request body needs.
type amazonMusicSession struct {
	DeviceID   string `json:"deviceId"`
	DeviceType string `json:"deviceType"`
	SessionID  string `json:"sessionId"`
	Version    string `json:"version"`
	CSRF       struct {
		Token string `json:"token"`
		Rnd   string `json:"rnd"`
		Ts    string `json:"ts"`
	} `json:"csrf"`
}

type amazonMusicSessionResolver struct {
	client    *http.Client
	configURL string // overridable in tests
	sf        singleflight.Group
	mu        sync.Mutex
	cached    *amazonMusicSession
}

func newAmazonMusicSessionResolver(client *http.Client) *amazonMusicSessionResolver {
	return &amazonMusicSessionResolver{client: client, configURL: amzConfigURL}
}

func (r *amazonMusicSessionResolver) get(ctx context.Context) (*amazonMusicSession, error) {
	r.mu.Lock()
	cached := r.cached
	r.mu.Unlock()
	if cached != nil {
		return cached, nil
	}

	v, err, _ := r.sf.Do("session", func() (any, error) {
		r.mu.Lock()
		existing := r.cached
		r.mu.Unlock()
		if existing != nil {
			return existing, nil
		}
		// Detach from the winning caller's ctx so one impatient caller can't
		// poison the shared resolve for every piggybacked waiter; the resolve
		// gets its own budget instead.
		rctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), amzResolveTimeout)
		defer cancel()
		return r.resolve(rctx)
	})
	if err != nil {
		return nil, err
	}
	return v.(*amazonMusicSession), nil
}

// amzResolveTimeout bounds one shared session resolve (a single config.json
// GET) independently of any single caller's deadline.
const amzResolveTimeout = 10 * time.Second

// invalidate drops the cached session only if it is still the one the caller
// failed with — a second 401 handler must not wipe the fresh session the first
// re-resolve just cached (the invalidate-storm case).
func (r *amazonMusicSessionResolver) invalidate(failed *amazonMusicSession) {
	r.mu.Lock()
	if r.cached == failed {
		r.cached = nil
	}
	r.mu.Unlock()
}

func (r *amazonMusicSessionResolver) resolve(ctx context.Context) (*amazonMusicSession, error) {
	status, body, err := getBytes(ctx, r.client, r.configURL, withHeader("User-Agent", amzUserAgent))
	if err != nil {
		return nil, fmt.Errorf("fetch config.json: status %d: %w", status, err)
	}

	var sess amazonMusicSession
	if err := json.Unmarshal(body, &sess); err != nil {
		return nil, fmt.Errorf("decode config.json: %w", err)
	}
	if sess.CSRF.Token == "" || sess.SessionID == "" {
		return nil, fmt.Errorf("config.json did not yield a usable session")
	}

	r.mu.Lock()
	r.cached = &sess
	r.mu.Unlock()
	return &sess, nil
}
