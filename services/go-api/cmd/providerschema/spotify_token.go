package main

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
)

type spotifyTOTPSecret struct {
	version string
	secret  string
}

var spotifyTOTPSecrets = []spotifyTOTPSecret{
	{"61", `,7/*F("rLJ2oxaKL^f+E1xvP@N`},
	{"60", `OmE{ZA.J^":0FG\\Uz?[@WW`},
	{"59", `{iOFn;4}<1PFYKPV?5{%u14]M>/V0hDH`},
}

func (spotify *spotifySession) ensureSession(ctx context.Context) error {
	if spotify.accessToken != "" && spotify.clientToken != "" {
		return nil
	}
	access, err := spotify.firstAccessToken(ctx)
	if err != nil {
		return fmt.Errorf("spotify access token: %w", err)
	}
	client, err := spotify.grantedClientToken(ctx)
	if err != nil {
		return fmt.Errorf("spotify client token: %w", err)
	}
	spotify.accessToken, spotify.clientToken = access, client
	return nil
}

func (spotify *spotifySession) grantedClientToken(ctx context.Context) (string, error) {
	raw, err := spotify.clientTokenBody(ctx, "")
	if err != nil {
		return "", err
	}
	var body struct {
		GrantedToken struct {
			Token string `json:"token"`
		} `json:"granted_token"`
	}
	if err := json.Unmarshal(raw, &body); err != nil || body.GrantedToken.Token == "" {
		return "", errors.New("no granted_token.token in the response")
	}
	return body.GrantedToken.Token, nil
}

func (spotify *spotifySession) firstAccessToken(ctx context.Context) (string, error) {
	lastErr := errors.New("no totp secrets")
	for _, totp := range spotifyTOTPSecrets {
		raw, err := spotify.accessTokenBody(ctx, totp.version)
		if err != nil {
			lastErr = err
			continue
		}
		var body struct {
			AccessToken string `json:"accessToken"`
		}
		if json.Unmarshal(raw, &body) == nil && body.AccessToken != "" {
			return body.AccessToken, nil
		}
		lastErr = fmt.Errorf("no access token for totp version %s", totp.version)
	}
	return "", lastErr
}

func (spotify *spotifySession) serverTime(ctx context.Context, _ string) (json.RawMessage, error) {
	return fetch(ctx, spotify.client, request{method: http.MethodGet, url: spotify.urls.site + "/api/server-time", header: map[string]string{"User-Agent": spotifyUserAgent}})
}

func (spotify *spotifySession) serverSeconds(ctx context.Context) (int64, error) {
	raw, err := spotify.serverTime(ctx, "")
	if err != nil {
		return 0, err
	}
	var body struct {
		ServerTime int64 `json:"serverTime"`
	}
	if err := json.Unmarshal(raw, &body); err != nil || body.ServerTime <= 0 {
		return 0, errors.New("spotify server time: no positive serverTime in the response")
	}
	return body.ServerTime, nil
}

func (spotify *spotifySession) accessTokenBody(ctx context.Context, version string) (json.RawMessage, error) {
	secret, ok := spotifySecret(version)
	if !ok {
		return nil, fmt.Errorf("spotify has no totp secret for version %q", version)
	}
	serverSeconds, err := spotify.serverSeconds(ctx)
	if err != nil {
		return nil, err
	}
	rawURL := fmt.Sprintf("%s/api/token?reason=init&productType=web-player&totp=%s&totpServer=%s&totpVer=%s",
		spotify.urls.site, spotifyTOTP(secret, spotify.now().Unix()), spotifyTOTP(secret, serverSeconds), version)
	return fetch(ctx, spotify.client, request{method: http.MethodGet, url: rawURL, header: map[string]string{"User-Agent": spotifyUserAgent}})
}

func spotifySecret(version string) (string, bool) {
	for _, totp := range spotifyTOTPSecrets {
		if totp.version == version {
			return totp.secret, true
		}
	}
	return "", false
}

func (spotify *spotifySession) clientTokenBody(ctx context.Context, _ string) (json.RawMessage, error) {
	payload, err := spotifyClientTokenPayload()
	if err != nil {
		return nil, err
	}
	return fetch(ctx, spotify.client, request{method: http.MethodPost, url: spotify.urls.clientToken, body: payload, header: map[string]string{
		"Content-Type": "application/json",
		"Accept":       "application/json",
		"User-Agent":   spotifyUserAgent,
	}})
}

const deviceIDBytes = 16

func spotifyClientTokenPayload() ([]byte, error) {
	deviceID := make([]byte, deviceIDBytes)
	_, _ = rand.Read(deviceID)
	r0, err := json.Marshal(map[string]any{
		"client_data": map[string]any{
			"client_version": spotifyWebClientVer,
			"client_id":      spotifyWebClientID,
			"js_sdk_data": map[string]any{
				"device_brand": "unknown",
				"device_model": "unknown",
				"os":           "unknown",
				"os_version":   "unknown",
				"device_id":    hex.EncodeToString(deviceID),
				"device_type":  "computer",
			},
		},
	})
	if err != nil {
		return r0, fmt.Errorf("marshal: %w", err)
	}
	return r0, nil
}

const (
	totpPeriodSeconds  = 30
	totpModulus        = 1000000
	totpOffsetMask     = 0x0F
	totpTruncatedBytes = 4
	totpSignBitMask    = 0x7FFFFFFF
	totpSecretXorCycle = 33
	totpSecretXorBase  = 9
)

func spotifyTOTP(secret string, unixSeconds int64) string {
	var key strings.Builder
	for i := range len(secret) {
		key.WriteString(strconv.Itoa(int(secret[i]) ^ (i%totpSecretXorCycle + totpSecretXorBase)))
	}
	var counter [8]byte
	binary.BigEndian.PutUint64(counter[:], uint64(unixSeconds)/totpPeriodSeconds)
	mac := hmac.New(sha1.New, []byte(key.String()))
	mac.Write(counter[:])
	digest := mac.Sum(nil)
	offset := digest[len(digest)-1] & totpOffsetMask
	code := binary.BigEndian.Uint32(digest[offset:offset+totpTruncatedBytes]) & totpSignBitMask
	return fmt.Sprintf("%06d", code%totpModulus)
}
