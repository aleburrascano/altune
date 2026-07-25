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

const amzConfigURL = "https://music.amazon.com/config.json"

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
	configURL string
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
		rctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), amzResolveTimeout)
		defer cancel()
		return r.resolve(rctx)
	})
	if err != nil {
		return nil, err
	}
	return v.(*amazonMusicSession), nil
}

const amzResolveTimeout = 10 * time.Second

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
