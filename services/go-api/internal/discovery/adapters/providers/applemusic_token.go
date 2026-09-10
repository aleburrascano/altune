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
	"time"
)

const (
	appleMusicSiteURL = "https://music.apple.com/us/search"
	appleMusicMaxBody = 8 << 20
)

var (
	appleMusicBundleRe = regexp.MustCompile(`assets/index~[A-Za-z0-9]+\.js`)
	appleMusicJWTRe    = regexp.MustCompile(`eyJ[A-Za-z0-9_-]+\.eyJ[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+`)
)

type appleMusicTokenResolver struct {
	*cachedResolver[string]
	client        *http.Client
	siteURL       string
	bundleBaseURL string
}

func newAppleMusicTokenResolver(client *http.Client) *appleMusicTokenResolver {
	r := &appleMusicTokenResolver{
		client:        client,
		siteURL:       appleMusicSiteURL,
		bundleBaseURL: "https://music.apple.com/",
	}
	r.cachedResolver = newCachedResolver("token", appleMusicResolveTimeout, r.resolve, nonEmpty)
	return r
}

const appleMusicResolveTimeout = 20 * time.Second

func (r *appleMusicTokenResolver) resolve(ctx context.Context) (string, time.Time, error) {
	html, err := r.fetchText(ctx, r.siteURL)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("fetch apple music page: %w", err)
	}

	bundlePath := appleMusicBundleRe.FindString(html)
	if bundlePath == "" {
		return "", time.Time{}, errors.New("no index bundle found on apple music page")
	}

	js, err := r.fetchText(ctx, r.bundleBaseURL+bundlePath)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("fetch apple music bundle: %w", err)
	}

	token, expiry, ok := extractAppleMusicToken(js)
	if !ok {
		return "", time.Time{}, errors.New("no anonymous devToken found in apple music bundle")
	}

	return token, expiry, nil
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
