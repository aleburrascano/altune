package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"time"

	"golang.org/x/time/rate"
)

const (
	maxAttempts      = 4
	retryBackoff     = 2 * time.Second
	maxServerErrors  = 5
	maxResponseBytes = 4 << 20
	backoffGrowth    = 2
	maxWaitCeiling   = time.Duration(math.MaxInt64)
)

type throttlePolicy struct {
	Attempts int
	MaxWait  time.Duration
}

var (
	errCatalogUnknown = errors.New("catalog does not know this id")
	errCatalogBlocked = errors.New("catalog is blocking requests")
)

func blocked(format string, args ...any) error {
	return fmt.Errorf("%w: %s", errCatalogBlocked, fmt.Sprintf(format, args...))
}

type catalogClient struct {
	http         *http.Client
	baseURL      string
	userAgent    string
	limiter      *rate.Limiter
	backoff      time.Duration
	attempts     int
	maxWait      time.Duration
	isThrottled  func(status int, body []byte) bool
	serverErrors int
}

func newCatalogClient(httpClient *http.Client, baseURL, userAgent string, requestsPerSecond float64, isThrottled func(int, []byte) bool) *catalogClient {
	return &catalogClient{
		http:        httpClient,
		baseURL:     baseURL,
		userAgent:   userAgent,
		limiter:     rate.NewLimiter(rate.Limit(requestsPerSecond), 1),
		backoff:     retryBackoff,
		attempts:    maxAttempts,
		isThrottled: isThrottled,
	}
}

func (c *catalogClient) get(ctx context.Context, path string) ([]byte, error) {
	for attempt := 1; ; attempt++ {
		status, body, err := c.getOnce(ctx, path)
		if err != nil {
			return nil, err
		}
		if !c.isThrottled(status, body) {
			return body, c.checkServerErrors(status, path)
		}
		c.serverErrors = 0
		if attempt >= c.attempts {
			return nil, blocked("still throttled after %d attempts on %s", attempt, path)
		}
		if err := sleepContext(ctx, c.waitBefore(attempt)); err != nil {
			return nil, err
		}
	}
}

func (c *catalogClient) waitBefore(attempt int) time.Duration {
	wait := c.backoff
	for range attempt - 1 {
		if wait > maxWaitCeiling/backoffGrowth {
			break
		}
		wait *= backoffGrowth
	}
	if c.maxWait > 0 {
		return min(wait, c.maxWait)
	}
	return wait
}

func (c *catalogClient) withThrottle(policy throttlePolicy) {
	c.attempts, c.maxWait = policy.Attempts, policy.MaxWait
}

func (c *catalogClient) checkServerErrors(status int, path string) error {
	if status < http.StatusInternalServerError {
		c.serverErrors = 0
		return statusError(status, path)
	}
	c.serverErrors++
	if c.serverErrors >= maxServerErrors {
		return blocked("%d server errors in a row, last HTTP %d on %s", c.serverErrors, status, path)
	}
	return statusError(status, path)
}

func statusError(status int, path string) error {
	switch status {
	case http.StatusOK:
		return nil
	case http.StatusForbidden:
		return blocked("HTTP 403 on %s", path)
	case http.StatusNotFound:
		return errCatalogUnknown
	}
	return fmt.Errorf("fetch %s: status %d", path, status)
}

func (c *catalogClient) getOnce(ctx context.Context, path string) (int, []byte, error) {
	if err := c.limiter.Wait(ctx); err != nil {
		return 0, nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+path, http.NoBody)
	if err != nil {
		return 0, nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("User-Agent", c.userAgent)
	req.Header.Set("Accept", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return 0, nil, fmt.Errorf("fetch %s: %w", path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return 0, nil, fmt.Errorf("read %s: %w", path, err)
	}
	return resp.StatusCode, body, nil
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
