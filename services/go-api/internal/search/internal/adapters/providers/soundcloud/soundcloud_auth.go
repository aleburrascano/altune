package soundcloud

import (
	"altune/go-api/internal/platform/outbound"
	"altune/go-api/internal/search/internal/adapters/providers/providerrequests"
	"altune/go-api/internal/shared/clock"
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
	*providerrequests.CachedResolver[string]
	client  *http.Client
	siteURL string
}

func newClientIDResolver(client *http.Client, now clock.Func) *clientIDResolver {
	r := &clientIDResolver{client: client, siteURL: scSiteURL}
	r.CachedResolver = providerrequests.NewCachedResolver("client_id", scResolveTimeout, r.resolve, providerrequests.NonEmpty, now)
	return r
}

const scResolveTimeout = 20 * time.Second

func (r *clientIDResolver) resolve(ctx context.Context) (string, time.Time, error) {
	html, err := outbound.GetText(ctx, r.client, r.siteURL, scMaxBodyBytes, scUserAgent)
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
		body, err := outbound.GetText(ctx, r.client, assets[i], scMaxBodyBytes, scUserAgent)
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
