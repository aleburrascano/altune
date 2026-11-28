package spotify

import (
	"altune/go-api/internal/platform/outbound"
	"altune/go-api/internal/search/internal/adapters/providers"
	"altune/go-api/internal/shared/clock"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"
)

const (
	spotifyServerTimeURL  = "https://open.spotify.com/api/server-time"
	spotifyAccessTokenURL = "https://open.spotify.com/api/token"
	spotifyClientTokenURL = "https://clienttoken.spotify.com/v1/clienttoken"
)

type spotifySession struct {
	accessToken  string
	accessExpiry time.Time
	clientToken  string
	clientExpiry time.Time
}

func (s *spotifySession) validAt(now time.Time) bool {
	return s != nil && now.Before(s.accessExpiry) && now.Before(s.clientExpiry)
}

type spotifyTokenResolver struct {
	*providers.CachedResolver[*spotifySession]
	client *http.Client
	now    clock.Func
}

func newSpotifyTokenResolver(client *http.Client, now clock.Func) *spotifyTokenResolver {
	r := &spotifyTokenResolver{client: client, now: now}
	r.CachedResolver = providers.NewCachedResolver("session", spotifyResolveTimeout, r.resolve, r.usable, now)
	return r
}

func (r *spotifyTokenResolver) usable(s *spotifySession) bool { return s.validAt(r.now()) }

const (
	spotifyResolveTimeout  = 20 * time.Second
	spotifyMaxTokenTTL     = 24 * time.Hour
	spotifyMinSessionLife  = 30 * time.Second
	spotifyAccessMargin    = time.Minute
	spotifyClientMargin    = time.Hour
	spotifyMaxTokenSeconds = int64(spotifyMaxTokenTTL / time.Second)
)

func spotifyExpiryAfter(now time.Time, ttl, margin time.Duration) time.Time {
	ttl = min(ttl, spotifyMaxTokenTTL)
	return now.Add(max(ttl-margin, spotifyMinSessionLife))
}

func (r *spotifyTokenResolver) resolve(ctx context.Context) (*spotifySession, time.Time, error) {
	accessToken, accessExpiry, err := r.resolveAccessToken(ctx)
	if err != nil {
		return nil, time.Time{}, fmt.Errorf("resolve access token: %w", err)
	}
	clientToken, clientExpiry, err := r.resolveClientToken(ctx)
	if err != nil {
		return nil, time.Time{}, fmt.Errorf("resolve client token: %w", err)
	}

	return &spotifySession{
		accessToken:  accessToken,
		accessExpiry: accessExpiry,
		clientToken:  clientToken,
		clientExpiry: clientExpiry,
	}, time.Time{}, nil
}

func (r *spotifyTokenResolver) resolveAccessToken(ctx context.Context) (string, time.Time, error) {
	serverTime, err := r.fetchServerTime(ctx)
	if err != nil {
		return "", time.Time{}, err
	}
	now := r.now().Unix()

	var lastErr error
	for _, s := range spotifyTOTPSecrets {
		u := fmt.Sprintf(
			"%s?reason=init&productType=web-player&totp=%s&totpServer=%s&totpVer=%d",
			spotifyAccessTokenURL, Code(s.secret, now), Code(s.secret, serverTime), s.version,
		)

		var body struct {
			AccessToken                      string `json:"accessToken"`
			AccessTokenExpirationTimestampMs int64  `json:"accessTokenExpirationTimestampMs"`
			Error                            string `json:"error"`
		}
		if err := outbound.GetJSON(ctx, r.client, u, &body, outbound.WithHeader("User-Agent", spotifyUserAgent)); err != nil {
			lastErr = err
			continue
		}
		if body.Error == "totpVerExpired" {
			lastErr = fmt.Errorf("totp version %d expired", s.version)
			continue
		}
		if body.AccessToken == "" {
			lastErr = fmt.Errorf("empty access token for totp version %d", s.version)
			continue
		}
		ttlMs := body.AccessTokenExpirationTimestampMs - r.now().UnixMilli()
		if body.AccessTokenExpirationTimestampMs <= 0 || ttlMs <= 0 {
			return "", time.Time{}, fmt.Errorf("access token expiry %d is not in the future", body.AccessTokenExpirationTimestampMs)
		}
		ttl := time.Duration(min(ttlMs, spotifyMaxTokenSeconds*1000)) * time.Millisecond
		return body.AccessToken, spotifyExpiryAfter(r.now(), ttl, spotifyAccessMargin), nil
	}
	return "", time.Time{}, fmt.Errorf("all totp secret versions exhausted: %w", lastErr)
}

func (r *spotifyTokenResolver) fetchServerTime(ctx context.Context) (int64, error) {
	var body struct {
		ServerTime int64 `json:"serverTime"`
	}
	if err := outbound.GetJSON(ctx, r.client, spotifyServerTimeURL, &body, outbound.WithHeader("User-Agent", spotifyUserAgent)); err != nil {
		return 0, err
	}
	if body.ServerTime <= 0 {
		return 0, fmt.Errorf("server time %d is not positive", body.ServerTime)
	}
	return body.ServerTime, nil
}

func (r *spotifyTokenResolver) resolveClientToken(ctx context.Context) (string, time.Time, error) {
	payload, err := json.Marshal(map[string]any{
		"client_data": map[string]any{
			"client_version": spotifyClientVersion,
			"client_id":      spotifyWebPlayerClientID,
			"js_sdk_data": map[string]any{
				"device_brand": "unknown",
				"device_model": "unknown",
				"os":           "unknown",
				"os_version":   "unknown",
				"device_id":    randomHexID(deviceIDBytes),
				"device_type":  "computer",
			},
		},
	})
	if err != nil {
		return "", time.Time{}, fmt.Errorf("encode the spotify client token request: %w", err)
	}

	var body struct {
		GrantedToken struct {
			Token               string `json:"token"`
			ExpiresAfterSeconds int64  `json:"expires_after_seconds"`
		} `json:"granted_token"`
	}
	if _, err := outbound.PostJSON(ctx, r.client, spotifyClientTokenURL, payload, &body, spotifyClientTokenHeaders()...); err != nil {
		return "", time.Time{}, err
	}
	if body.GrantedToken.Token == "" {
		return "", time.Time{}, errors.New("empty client token")
	}
	seconds := body.GrantedToken.ExpiresAfterSeconds
	if seconds <= 0 {
		return "", time.Time{}, fmt.Errorf("client token lifetime %ds is not positive", seconds)
	}
	ttl := time.Duration(min(seconds, spotifyMaxTokenSeconds)) * time.Second
	return body.GrantedToken.Token, spotifyExpiryAfter(r.now(), ttl, spotifyClientMargin), nil
}

func spotifyClientTokenHeaders() []outbound.ReqOption {
	return []outbound.ReqOption{
		outbound.WithHeader("Content-Type", "application/json"),
		outbound.WithHeader("Accept", "application/json"),
		outbound.WithHeader("User-Agent", spotifyUserAgent),
	}
}

const deviceIDBytes = 16

func randomHexID(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

type spotifyTOTPSecret struct {
	version int
	secret  string
}

var spotifyTOTPSecrets = []spotifyTOTPSecret{
	{61, `,7/*F("rLJ2oxaKL^f+E1xvP@N`},
	{60, `OmE{ZA.J^":0FG\\Uz?[@WW`},
	{59, `{iOFn;4}<1PFYKPV?5{%u14]M>/V0hDH`},
}
