package providers

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"sync"
	"time"

	"golang.org/x/sync/singleflight"
)

var scAssetURLRe = regexp.MustCompile(`https?://[^"' ]+/assets/[^"' ]+\.js`)

var scClientIDRe = regexp.MustCompile(`client_id\s*[:=]\s*"?([a-zA-Z0-9]{32})"?`)

const (
	scSiteURL      = "https://soundcloud.com/"
	scMaxBodyBytes = 16 << 20
)

type clientIDResolver struct {
	client  *http.Client
	siteURL string
	sf      singleflight.Group
	mu      sync.Mutex
	cached  string
}

func newClientIDResolver(client *http.Client) *clientIDResolver {
	return &clientIDResolver{client: client, siteURL: scSiteURL}
}

func (r *clientIDResolver) get(ctx context.Context) (string, error) {
	r.mu.Lock()
	cached := r.cached
	r.mu.Unlock()
	if cached != "" {
		return cached, nil
	}

	v, err, _ := r.sf.Do("client_id", func() (any, error) {
		r.mu.Lock()
		existing := r.cached
		r.mu.Unlock()
		if existing != "" {
			return existing, nil
		}
		rctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), scResolveTimeout)
		defer cancel()
		return r.resolve(rctx)
	})
	if err != nil {
		return "", err
	}

	id, _ := v.(string)
	if id == "" {
		return "", errors.New("soundcloud: resolved empty client_id")
	}
	r.mu.Lock()
	r.cached = id
	r.mu.Unlock()
	return id, nil
}

const scResolveTimeout = 20 * time.Second

func (r *clientIDResolver) invalidate(failed string) {
	r.mu.Lock()
	if r.cached == failed {
		r.cached = ""
	}
	r.mu.Unlock()
}

func (r *clientIDResolver) resolve(ctx context.Context) (string, error) {
	html, err := r.fetchText(ctx, r.siteURL)
	if err != nil {
		return "", fmt.Errorf("fetch soundcloud home: %w", err)
	}

	assets := dedupePreserveOrder(scAssetURLRe.FindAllString(html, -1))
	if len(assets) == 0 {
		return "", errors.New("no asset bundles found on soundcloud home")
	}

	for i := len(assets) - 1; i >= 0; i-- {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		body, err := r.fetchText(ctx, assets[i])
		if err != nil {
			continue
		}
		if m := scClientIDRe.FindStringSubmatch(body); m != nil {
			return m[1], nil
		}
	}
	return "", errors.New("client_id not found in any asset bundle")
}

func (r *clientIDResolver) fetchText(ctx context.Context, u string) (string, error) {
	status, body, err := getBytesCapped(ctx, r.client, u, scMaxBodyBytes, withHeader("User-Agent", scUserAgent))
	if err != nil {
		if status != 0 {
			return "", fmt.Errorf("GET %s: status %d", u, status)
		}
		return "", err
	}
	return string(body), nil
}

func dedupePreserveOrder(in []string) []string {
	seen := make(map[string]bool, len(in))
	out := make([]string, 0, len(in))
	for _, s := range in {
		if seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	return out
}
