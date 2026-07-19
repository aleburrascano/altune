package providers

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"sync"

	"golang.org/x/sync/singleflight"
)

// --- client_id resolution -------------------------------------------------
//
// This half of the SoundCloud adapter is independent of the search/mapping
// code in soundcloud.go: it only scrapes and caches the public client_id the
// api-v2 backend requires, and it changes on its own externally-forced
// cadence (SoundCloud periodically rotates how the id is embedded, breaking
// the scraper independent of anything else in the adapter).

// scAssetURLRe matches the JS bundle URLs the SoundCloud homepage loads; one of
// them embeds the public client_id the api-v2 backend requires. The homepage we
// fetch is the trust anchor, so we match any absolute /assets/*.js it lists and
// scan each for the key, rather than hard-coding the CDN host.
var scAssetURLRe = regexp.MustCompile(`https?://[^"' ]+/assets/[^"' ]+\.js`)

// scClientIDRe extracts the 32-char client_id from a JS bundle, tolerating the
// minified `client_id:"…"` and `client_id="…"` forms.
var scClientIDRe = regexp.MustCompile(`client_id\s*[:=]\s*"?([a-zA-Z0-9]{32})"?`)

const (
	scSiteURL      = "https://soundcloud.com/"
	scMaxBodyBytes = 16 << 20 // JS bundles are a few MB; cap defensively
)

// clientIDResolver fetches and caches the public client_id the api-v2 backend
// requires. Resolution scrapes the homepage's JS bundles once; the cached value
// is reused until an auth failure invalidates it (the rotation tax). Concurrent
// cold-start callers are collapsed with singleflight so a burst of searches
// triggers exactly one scrape.
type clientIDResolver struct {
	client  *http.Client
	siteURL string // overridable in tests
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
		return r.resolve(ctx)
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

func (r *clientIDResolver) invalidate() {
	r.mu.Lock()
	r.cached = ""
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

	// The client_id lives in one of the later bundles, so scan from the end.
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
