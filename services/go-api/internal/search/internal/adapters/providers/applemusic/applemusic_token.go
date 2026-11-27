package applemusic

import (
	"altune/go-api/internal/platform/outbound"
	"altune/go-api/internal/search/internal/adapters/providers/providerrequests"
	"altune/go-api/internal/shared/clock"
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
	*providerrequests.CachedResolver[string]
	client        *http.Client
	siteURL       string
	bundleBaseURL string
}

func newAppleMusicTokenResolver(client *http.Client, now clock.Func) *appleMusicTokenResolver {
	r := &appleMusicTokenResolver{
		client:        client,
		siteURL:       appleMusicSiteURL,
		bundleBaseURL: "https://music.apple.com/",
	}
	r.CachedResolver = providerrequests.NewCachedResolver("token", appleMusicResolveTimeout, r.resolve, providerrequests.NonEmpty, now)
	return r
}

const appleMusicResolveTimeout = 20 * time.Second

func (r *appleMusicTokenResolver) resolve(ctx context.Context) (string, time.Time, error) {
	html, err := outbound.GetText(ctx, r.client, r.siteURL, appleMusicMaxBody, appleMusicUserAgent)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("fetch apple music page: %w", err)
	}

	bundlePath := appleMusicBundleRe.FindString(html)
	if bundlePath == "" {
		return "", time.Time{}, errors.New("no index bundle found on apple music page")
	}

	js, err := outbound.GetText(ctx, r.client, r.bundleBaseURL+bundlePath, appleMusicMaxBody, appleMusicUserAgent)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("fetch apple music bundle: %w", err)
	}

	token, expiry, ok := extractAppleMusicToken(js)
	if !ok {
		return "", time.Time{}, errors.New("no anonymous devToken found in apple music bundle")
	}

	return token, expiry, nil
}

const jwtParts = 3

func extractAppleMusicToken(js string) (token string, expiry time.Time, ok bool) {
	for _, candidate := range appleMusicJWTRe.FindAllString(js, -1) {
		parts := strings.Split(candidate, ".")
		if len(parts) != jwtParts {
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
