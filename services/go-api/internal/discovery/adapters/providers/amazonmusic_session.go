package providers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"

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
		return r.resolve(ctx)
	})
	if err != nil {
		return nil, err
	}
	return v.(*amazonMusicSession), nil
}

func (r *amazonMusicSessionResolver) invalidate() {
	r.mu.Lock()
	r.cached = nil
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
