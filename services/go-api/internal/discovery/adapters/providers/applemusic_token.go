package providers

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"

	"golang.org/x/sync/singleflight"
)

// --- token resolution -------------------------------------------------
//
// Apple Music's web player (music.apple.com) embeds a long-lived, anonymous
// MusicKit developer token directly in its public JS bundle — no Apple
// Developer Program enrollment needed, unlike the paid MusicKit API most
// integrators use. This resolver scrapes it the same way SoundCloud's
// clientIDResolver scrapes its client_id: fetch the page, find the current
// cache-busted bundle URL, scan it for the token. Confirmed live 2026-07-22:
// the token's JWT `exp` gives a multi-week validity window (~35 days
// observed), so this refreshes rarely — far less often than SoundCloud's
// client_id or Amazon Music's per-session bundle.

const (
	appleMusicSiteURL = "https://music.apple.com/us/search"
	appleMusicMaxBody = 8 << 20 // the index bundle is a few MB
)

var (
	appleMusicBundleRe = regexp.MustCompile(`assets/index~[A-Za-z0-9]+\.js`)
	appleMusicJWTRe     = regexp.MustCompile(`eyJ[A-Za-z0-9_-]+\.eyJ[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+`)
)

type appleMusicTokenResolver struct {
	client        *http.Client
	siteURL       string // overridable in tests
	bundleBaseURL string // overridable in tests; the bundle path is same-origin
	sf            singleflight.Group
	mu            sync.Mutex
	cached        string
	expiry        time.Time
}

func newAppleMusicTokenResolver(client *http.Client) *appleMusicTokenResolver {
	return &appleMusicTokenResolver{
		client:        client,
		siteURL:       appleMusicSiteURL,
		bundleBaseURL: "https://music.apple.com/",
	}
}

func (r *appleMusicTokenResolver) get(ctx context.Context) (string, error) {
	r.mu.Lock()
	cached, expiry := r.cached, r.expiry
	r.mu.Unlock()
	if cached != "" && time.Now().Before(expiry) {
		return cached, nil
	}

	v, err, _ := r.sf.Do("token", func() (any, error) {
		r.mu.Lock()
		existing, existingExpiry := r.cached, r.expiry
		r.mu.Unlock()
		if existing != "" && time.Now().Before(existingExpiry) {
			return existing, nil
		}
		return r.resolve(ctx)
	})
	if err != nil {
		return "", err
	}
	return v.(string), nil
}

func (r *appleMusicTokenResolver) invalidate() {
	r.mu.Lock()
	r.cached = ""
	r.mu.Unlock()
}

func (r *appleMusicTokenResolver) resolve(ctx context.Context) (string, error) {
	html, err := r.fetchText(ctx, r.siteURL)
	if err != nil {
		return "", fmt.Errorf("fetch apple music page: %w", err)
	}

	bundlePath := appleMusicBundleRe.FindString(html)
	if bundlePath == "" {
		return "", errors.New("no index bundle found on apple music page")
	}

	js, err := r.fetchText(ctx, r.bundleBaseURL+bundlePath)
	if err != nil {
		return "", fmt.Errorf("fetch apple music bundle: %w", err)
	}

	token, expiry, ok := extractAppleMusicToken(js)
	if !ok {
		return "", errors.New("no anonymous devToken found in apple music bundle")
	}

	r.mu.Lock()
	r.cached = token
	r.expiry = expiry
	r.mu.Unlock()
	return token, nil
}

func (r *appleMusicTokenResolver) fetchText(ctx context.Context, u string) (string, error) {
	status, body, err := getBytesCapped(ctx, r.client, u, appleMusicMaxBody, withHeader("User-Agent", appleMusicUserAgent))
	if err != nil {
		if status != 0 {
			return "", fmt.Errorf("GET %s: status %d", u, status)
		}
		return "", err
	}
	return string(body), nil
}

// extractAppleMusicToken scans js for embedded JWTs (the bundle carries
// several, issued to different Apple internal consumers) and returns the one
// whose payload identifies it as the anonymous web-player token
// ("AMPWebPlay" — confirmed live to authenticate api.music.apple.com), along
// with its expiry.
func extractAppleMusicToken(js string) (token string, expiry time.Time, ok bool) {
	for _, candidate := range appleMusicJWTRe.FindAllString(js, -1) {
		parts := strings.Split(candidate, ".")
		if len(parts) != 3 {
			continue
		}
		payload, err := base64.RawURLEncoding.DecodeString(parts[1])
		if err != nil {
			continue
		}
		var claims struct {
			Iss string `json:"iss"`
			Exp int64  `json:"exp"`
		}
		if err := json.Unmarshal(payload, &claims); err != nil {
			continue
		}
		if claims.Iss == "AMPWebPlay" && claims.Exp > 0 {
			return candidate, time.Unix(claims.Exp, 0), true
		}
	}
	return "", time.Time{}, false
}
