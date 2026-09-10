package providers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

const amzConfigURL = "https://music.amazon.com/config.json"

type amazonMusicSession struct {
	DeviceID   string `json:"deviceId"`
	DeviceType string `json:"deviceType"`
	SessionID  string `json:"sessionId"`
	Version    string `json:"version"`
	CSRF       struct {
		Token string `json:"token"`
		Rnd   string `json:"rnd"`
		Ts    string `json:"ts"`
	} `json:"csrf"`
}

type amazonMusicSessionResolver struct {
	*cachedResolver[*amazonMusicSession]
	client    *http.Client
	configURL string
}

func newAmazonMusicSessionResolver(client *http.Client) *amazonMusicSessionResolver {
	r := &amazonMusicSessionResolver{client: client, configURL: amzConfigURL}
	r.cachedResolver = newCachedResolver("session", amzResolveTimeout, r.resolve, func(s *amazonMusicSession) bool { return s != nil })
	return r
}

const amzResolveTimeout = 10 * time.Second

func (r *amazonMusicSessionResolver) resolve(ctx context.Context) (*amazonMusicSession, time.Time, error) {
	status, body, err := getBytes(ctx, r.client, r.configURL, withHeader("User-Agent", amzUserAgent))
	if err != nil {
		return nil, time.Time{}, fmt.Errorf("fetch config.json: status %d: %w", status, err)
	}

	var sess amazonMusicSession
	if err := json.Unmarshal(body, &sess); err != nil {
		return nil, time.Time{}, fmt.Errorf("decode config.json: %w", err)
	}
	if sess.CSRF.Token == "" || sess.SessionID == "" {
		return nil, time.Time{}, fmt.Errorf("config.json did not yield a usable session")
	}

	return &sess, time.Time{}, nil
}
