package main

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"golang.org/x/time/rate"
)

const (
	deezerPlaceholderImageID = "d41d8cd98f00b204e9800998ecf8427e"
	coverCDNRequestsPerSec   = 10.0
)

type coverProbe interface {
	IsPlaceholder(ctx context.Context, coverURL string) (bool, error)
	Resolves(ctx context.Context, coverURL string) (bool, error)
}

type trustedCovers struct{}

func (trustedCovers) IsPlaceholder(context.Context, string) (bool, error) { return false, nil }

func (trustedCovers) Resolves(context.Context, string) (bool, error) { return true, nil }

type httpCoverProbe struct {
	following   *http.Client
	notFollowed *http.Client
	limiter     *rate.Limiter
}

func newHTTPCoverProbe(base *http.Client, requestsPerSecond float64) httpCoverProbe {
	notFollowed := *base
	notFollowed.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return httpCoverProbe{following: base, notFollowed: &notFollowed, limiter: rate.NewLimiter(rate.Limit(requestsPerSecond), 1)}
}

func (p httpCoverProbe) IsPlaceholder(ctx context.Context, coverURL string) (bool, error) {
	return p.fetch(ctx, p.notFollowed, coverURL, func(resp *http.Response) bool {
		return resp.StatusCode/100 == 3 && strings.Contains(resp.Header.Get("Location"), deezerPlaceholderImageID)
	})
}

func (p httpCoverProbe) Resolves(ctx context.Context, coverURL string) (bool, error) {
	return p.fetch(ctx, p.following, coverURL, func(resp *http.Response) bool {
		return resp.StatusCode == http.StatusOK && strings.HasPrefix(resp.Header.Get("Content-Type"), "image/")
	})
}

func (p httpCoverProbe) fetch(ctx context.Context, client *http.Client, coverURL string, judge func(*http.Response) bool) (bool, error) {
	if err := p.limiter.Wait(ctx); err != nil {
		return false, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, coverURL, http.NoBody)
	if err != nil {
		return false, fmt.Errorf("build cover request: %w", err)
	}
	req.Header.Set("User-Agent", deezerUserAgent)
	resp, err := client.Do(req)
	if err != nil {
		return false, fmt.Errorf("probe cover %s: %w", coverURL, err)
	}
	defer func() { _ = resp.Body.Close() }()
	return judge(resp), nil
}
