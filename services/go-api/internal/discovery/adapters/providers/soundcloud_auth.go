package providers

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"time"
)

var scAssetURLRe = regexp.MustCompile(`https?://[^"' ]+/assets/[^"' ]+\.js`)

var scClientIDRe = regexp.MustCompile(`client_id\s*[:=]\s*"?([a-zA-Z0-9]{32})"?`)

const (
	scSiteURL      = "https://soundcloud.com/"
	scMaxBodyBytes = 16 << 20
)

type clientIDResolver struct {
	*cachedResolver[string]
	client  *http.Client
	siteURL string
}

func newClientIDResolver(client *http.Client) *clientIDResolver {
	r := &clientIDResolver{client: client, siteURL: scSiteURL}
	r.cachedResolver = newCachedResolver("client_id", scResolveTimeout, r.resolve, nonEmpty)
	return r
}

const scResolveTimeout = 20 * time.Second

func (r *clientIDResolver) resolve(ctx context.Context) (string, time.Time, error) {
	html, err := r.fetchText(ctx, r.siteURL)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("fetch soundcloud home: %w", err)
	}

	assets := dedupePreserveOrder(scAssetURLRe.FindAllString(html, -1))
	if len(assets) == 0 {
		return "", time.Time{}, errors.New("no asset bundles found on soundcloud home")
	}

	for i := len(assets) - 1; i >= 0; i-- {
		if ctx.Err() != nil {
			return "", time.Time{}, ctx.Err()
		}
		body, err := r.fetchText(ctx, assets[i])
		if err != nil {
			continue
		}
		if m := scClientIDRe.FindStringSubmatch(body); m != nil {
			if m[1] == "" {
				return "", time.Time{}, errors.New("soundcloud: resolved empty client_id")
			}
			return m[1], time.Time{}, nil
		}
	}
	return "", time.Time{}, errors.New("client_id not found in any asset bundle")
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
