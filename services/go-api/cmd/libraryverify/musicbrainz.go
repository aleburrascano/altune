package main

import (
	"altune/go-api/internal/shared/songfacts"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"golang.org/x/time/rate"
)

const (
	musicBrainzBaseURL   = "https://musicbrainz.org"
	musicBrainzUserAgent = "altune-libraryverify/1.0 ( https://github.com/aleburrascano/altune )"
	musicBrainzAttempts  = 4
	musicBrainzBackoff   = 2 * time.Second
	musicBrainzBodyCap   = 4 << 20
)

var errNoResponse = errors.New("the http client returned neither a response nor an error")

var errMusicBrainzThrottled = errors.New("musicbrainz is still throttling after retries")

type recordingLookups interface {
	RecordingsByISRC(ctx context.Context, isrc string) ([]string, error)
	CanonicalRecording(ctx context.Context, id string) (string, error)
}

type musicBrainz struct {
	client  *http.Client
	baseURL string
	limiter *rate.Limiter
	backoff time.Duration
}

func newMusicBrainz(client *http.Client, baseURL string, requestsPerSecond float64) *musicBrainz {
	return &musicBrainz{client: client, baseURL: baseURL, limiter: rate.NewLimiter(rate.Limit(requestsPerSecond), 1), backoff: musicBrainzBackoff}
}

type isrcBody struct {
	Recordings []struct {
		ID string `json:"id"`
	} `json:"recordings"`
}

type recordingIDBody struct {
	ID string `json:"id"`
}

func (m *musicBrainz) RecordingsByISRC(ctx context.Context, isrc string) ([]string, error) {
	var body isrcBody
	found, err := m.getJSON(ctx, "/ws/2/isrc/"+url.PathEscape(songfacts.CanonicalISRC(isrc))+"?fmt=json", &body)
	if err != nil || !found {
		return nil, err
	}
	ids := make([]string, 0, len(body.Recordings))
	for _, recording := range body.Recordings {
		ids = append(ids, recording.ID)
	}
	return ids, nil
}

func (m *musicBrainz) CanonicalRecording(ctx context.Context, id string) (string, error) {
	var body recordingIDBody
	found, err := m.getJSON(ctx, "/ws/2/recording/"+url.PathEscape(id)+"?fmt=json", &body)
	if err != nil || !found {
		return "", err
	}
	return body.ID, nil
}

func (m *musicBrainz) getJSON(ctx context.Context, path string, into any) (bool, error) {
	for attempt := 1; attempt <= musicBrainzAttempts; attempt++ {
		status, raw, err := m.getOnce(ctx, path)
		switch {
		case err != nil:
			return false, err
		case status == http.StatusNotFound:
			return false, nil
		case status == http.StatusOK:
			if err := json.Unmarshal(raw, into); err != nil {
				return false, fmt.Errorf("decode musicbrainz %s: %w", path, err)
			}
			return true, nil
		case status != http.StatusServiceUnavailable && status != http.StatusTooManyRequests:
			return false, fmt.Errorf("musicbrainz %s: status %d", path, status)
		}
		if err := sleepContext(ctx, m.backoff<<(attempt-1)); err != nil {
			return false, err
		}
	}
	return false, errMusicBrainzThrottled
}

func (m *musicBrainz) getOnce(ctx context.Context, path string) (int, []byte, error) {
	if err := m.limiter.Wait(ctx); err != nil {
		return 0, nil, err
	}
	req, err := m.newRequest(ctx, path)
	if err != nil {
		return 0, nil, err
	}
	resp, err := m.client.Do(req)
	switch {
	case err != nil:
		return 0, nil, fmt.Errorf("musicbrainz %s: %w", path, err)
	case resp == nil:
		return 0, nil, fmt.Errorf("musicbrainz %s: %w", path, errNoResponse)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, musicBrainzBodyCap))
	if err != nil {
		return 0, nil, fmt.Errorf("read musicbrainz %s: %w", path, err)
	}
	return resp.StatusCode, raw, nil
}

func (m *musicBrainz) newRequest(ctx context.Context, path string) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, m.baseURL+path, http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("build musicbrainz request: %w", err)
	}
	req.Header.Set("User-Agent", musicBrainzUserAgent)
	req.Header.Set("Accept", "application/json")
	return req, nil
}

func sleepContext(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
