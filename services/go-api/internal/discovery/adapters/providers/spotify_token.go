package providers

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"

	"golang.org/x/sync/singleflight"
)

// --- session resolution --------------------------------------------------
//
// Two independent tokens are needed to call Spotify's internal partner
// GraphQL API anonymously: a TOTP-gated access token (open.spotify.com/api/
// token — see spotify_totp.go) and a separate client-integrity token
// (clienttoken.spotify.com), confirmed live to be required alongside the
// bearer token on every pathfinder call. Neither requires an account login
// (no sp_dc cookie observed on any of these requests) — this is Spotify's
// anonymous web-visitor path, the same one librespot/zotify's login-gated
// tier is NOT — confirmed by direct network capture, 2026-07-22.

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

func (s *spotifySession) valid() bool {
	now := time.Now()
	return s != nil && now.Before(s.accessExpiry) && now.Before(s.clientExpiry)
}

type spotifyTokenResolver struct {
	client *http.Client
	sf     singleflight.Group
	mu     sync.Mutex
	cached *spotifySession

	// overridable in tests
	serverTimeURL  string
	accessTokenURL string
	clientTokenURL string
}

func newSpotifyTokenResolver(client *http.Client) *spotifyTokenResolver {
	return &spotifyTokenResolver{
		client:         client,
		serverTimeURL:  spotifyServerTimeURL,
		accessTokenURL: spotifyAccessTokenURL,
		clientTokenURL: spotifyClientTokenURL,
	}
}

func (r *spotifyTokenResolver) get(ctx context.Context) (*spotifySession, error) {
	r.mu.Lock()
	cached := r.cached
	r.mu.Unlock()
	if cached.valid() {
		return cached, nil
	}

	v, err, _ := r.sf.Do("session", func() (any, error) {
		r.mu.Lock()
		existing := r.cached
		r.mu.Unlock()
		if existing.valid() {
			return existing, nil
		}
		return r.resolve(ctx)
	})
	if err != nil {
		return nil, err
	}
	return v.(*spotifySession), nil
}

func (r *spotifyTokenResolver) invalidate() {
	r.mu.Lock()
	r.cached = nil
	r.mu.Unlock()
}

func (r *spotifyTokenResolver) resolve(ctx context.Context) (*spotifySession, error) {
	accessToken, accessExpiry, err := r.resolveAccessToken(ctx)
	if err != nil {
		return nil, fmt.Errorf("resolve access token: %w", err)
	}
	clientToken, clientExpiry, err := r.resolveClientToken(ctx)
	if err != nil {
		return nil, fmt.Errorf("resolve client token: %w", err)
	}

	sess := &spotifySession{
		accessToken:  accessToken,
		accessExpiry: accessExpiry,
		clientToken:  clientToken,
		clientExpiry: clientExpiry,
	}
	r.mu.Lock()
	r.cached = sess
	r.mu.Unlock()
	return sess, nil
}

// resolveAccessToken fetches Spotify's server time (the TOTP counter must be
// within Spotify's clock skew tolerance) then tries each TOTP secret
// version newest-first, falling back on a "totpVerExpired" response — the
// same rotation-tolerant shape as SoundCloud's client_id re-resolve.
func (r *spotifyTokenResolver) resolveAccessToken(ctx context.Context) (string, time.Time, error) {
	serverTime, err := r.fetchServerTime(ctx)
	if err != nil {
		return "", time.Time{}, err
	}
	now := time.Now().Unix()

	var lastErr error
	for _, s := range spotifyTOTPSecrets {
		totp := spotifyTOTPGenerate(s.secret, now)
		totpServer := spotifyTOTPGenerate(s.secret, serverTime)
		u := fmt.Sprintf(
			"%s?reason=init&productType=web-player&totp=%s&totpServer=%s&totpVer=%d",
			r.accessTokenURL, totp, totpServer, s.version,
		)

		var body struct {
			AccessToken                      string `json:"accessToken"`
			AccessTokenExpirationTimestampMs int64  `json:"accessTokenExpirationTimestampMs"`
			Error                            string `json:"error"`
		}
		if err := getJSON(ctx, r.client, u, &body, withHeader("User-Agent", spotifyUserAgent)); err != nil {
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
		// Skew the cached expiry a minute early so a request never starts
		// against a token that expires mid-flight.
		return body.AccessToken, time.UnixMilli(body.AccessTokenExpirationTimestampMs).Add(-time.Minute), nil
	}
	return "", time.Time{}, fmt.Errorf("all totp secret versions exhausted: %w", lastErr)
}

func (r *spotifyTokenResolver) fetchServerTime(ctx context.Context) (int64, error) {
	var body struct {
		ServerTime int64 `json:"serverTime"`
	}
	if err := getJSON(ctx, r.client, r.serverTimeURL, &body, withHeader("User-Agent", spotifyUserAgent)); err != nil {
		return 0, err
	}
	return body.ServerTime, nil
}

// resolveClientToken exchanges a synthetic device identity for the
// client-integrity token pathfinder requires alongside the bearer token.
// client_id here is Spotify's own public web-player client id (not a
// developer-registered app id) — the same fixed value the web player itself
// uses, confirmed live 2026-07-22.
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
				"device_id":    randomHexID(16),
				"device_type":  "computer",
			},
		},
	})
	if err != nil {
		return "", time.Time{}, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, r.clientTokenURL, bytes.NewReader(payload))
	if err != nil {
		return "", time.Time{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", spotifyUserAgent)

	resp, err := r.client.Do(req)
	if err != nil {
		return "", time.Time{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", time.Time{}, fmt.Errorf("clienttoken http status %d", resp.StatusCode)
	}

	var body struct {
		GrantedToken struct {
			Token               string `json:"token"`
			ExpiresAfterSeconds int64  `json:"expires_after_seconds"`
		} `json:"granted_token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return "", time.Time{}, err
	}
	if body.GrantedToken.Token == "" {
		return "", time.Time{}, errors.New("empty client token")
	}
	// Long-lived (observed ~14 days); skew an hour early to be safe.
	expiry := time.Now().Add(time.Duration(body.GrantedToken.ExpiresAfterSeconds) * time.Second).Add(-time.Hour)
	return body.GrantedToken.Token, expiry, nil
}

func randomHexID(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
