package ytmsearch

import (
	acqdomain "altune/go-api/internal/acquisition/domain"
	"altune/go-api/internal/shared/ytminnertube"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sync"
	"time"
)

const (
	DefaultMusicURL  = ytminnertube.MusicURL
	DefaultOEmbedURL = "https://www.youtube.com/oembed"

	songsFilterParams = "EgWKAQIIAWoMEA4QChADEAQQCRAF"
	responseBodyCap   = 16 << 20
	watchURLPrefix    = "https://www.youtube.com/watch?v="
)

type Config struct {
	HTTP        *http.Client
	MusicURL    string
	OEmbedURL   string
	MinInterval time.Duration
	Timeout     time.Duration
}

type Client struct {
	http      *http.Client
	musicURL  string
	oembedURL string
	timeout   time.Duration
	next      *gate
	search    *gate
	oembed    *gate
}

func New(cfg Config) *Client {
	c := &Client{
		http:      cfg.HTTP,
		musicURL:  cfg.MusicURL,
		oembedURL: cfg.OEmbedURL,
		timeout:   cfg.Timeout,
		next:      &gate{interval: cfg.MinInterval},
		search:    &gate{interval: cfg.MinInterval},
		oembed:    &gate{interval: cfg.MinInterval},
	}
	if c.http == nil {
		c.http = &http.Client{}
	}
	if c.musicURL == "" {
		c.musicURL = DefaultMusicURL
	}
	if c.oembedURL == "" {
		c.oembedURL = DefaultOEmbedURL
	}
	return c
}

func (c *Client) Watch(ctx context.Context, videoID string) (Watch, error) {
	raw, err := c.postInnerTube(ctx, c.next, "next", map[string]any{"videoId": videoID})
	if err != nil {
		return Watch{}, c.outage(ctx, err)
	}
	watch, err := ParseWatch(raw)
	if err != nil {
		return Watch{}, c.outage(ctx, err)
	}
	return watch, nil
}

func (c *Client) SearchSongs(ctx context.Context, query string) ([]Song, error) {
	raw, err := c.postInnerTube(ctx, c.search, "search", map[string]any{"query": query, "params": songsFilterParams})
	if err != nil {
		return nil, c.outage(ctx, err)
	}
	songs, err := ParseSongs(raw)
	if err != nil {
		return nil, c.outage(ctx, err)
	}
	return songs, nil
}

func (c *Client) outage(ctx context.Context, err error) error {
	if ctxErr := ctx.Err(); ctxErr != nil {
		return ctxErr
	}
	if errors.Is(err, context.DeadlineExceeded) {
		err = fmt.Errorf("ytmsearch: timed out after %v", c.timeout)
	}
	return &acqdomain.SourceUnavailableError{Source: SourceName, Err: err}
}

func (c *Client) OEmbed(ctx context.Context, videoID string) (OEmbed, error) {
	query := url.Values{"url": {watchURLPrefix + videoID}, "format": {"json"}}
	req, err := http.NewRequest(http.MethodGet, c.oembedURL+"?"+query.Encode(), http.NoBody)
	if err != nil {
		return OEmbed{}, fmt.Errorf("ytmsearch: build oembed request: %w", err)
	}
	raw, err := c.do(ctx, c.oembed, req)
	if err != nil {
		return OEmbed{}, err
	}
	return ParseOEmbed(raw)
}

func (c *Client) postInnerTube(ctx context.Context, g *gate, endpoint string, fields map[string]any) ([]byte, error) {
	payload, err := json.Marshal(ytminnertube.Body(fields))
	if err != nil {
		return nil, fmt.Errorf("ytmsearch: marshal %s: %w", endpoint, err)
	}
	target := ytminnertube.URL(c.musicURL, endpoint)
	req, err := http.NewRequest(http.MethodPost, target, bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("ytmsearch: build %s request: %w", endpoint, err)
	}
	req.Header.Set("Content-Type", "application/json; charset=utf-8")
	req.Header.Set("Referer", DefaultMusicURL+"/")
	return c.do(ctx, g, req)
}

func (c *Client) do(ctx context.Context, g *gate, req *http.Request) ([]byte, error) {
	if err := g.wait(ctx); err != nil {
		return nil, err
	}
	ctx, err := ytminnertube.ReserveSlot(ctx, c.http)
	if err != nil {
		return nil, err
	}
	ctx, cancel := c.boundedContext(ctx)
	defer cancel()
	req = req.WithContext(ctx)
	req.Header.Set("User-Agent", ytminnertube.UserAgent)
	return c.send(req)
}

func (c *Client) boundedContext(ctx context.Context) (context.Context, context.CancelFunc) {
	if c.timeout <= 0 {
		return ctx, func() {}
	}
	return context.WithTimeout(ctx, c.timeout)
}

func (c *Client) send(req *http.Request) ([]byte, error) {
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("ytmsearch: %s: %w", req.URL.Path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("ytmsearch: %s: status %d", req.URL.Path, resp.StatusCode)
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, responseBodyCap))
	if err != nil {
		return nil, fmt.Errorf("ytmsearch: %s: read body: %w", req.URL.Path, err)
	}
	return raw, nil
}

type gate struct {
	mu       sync.Mutex
	interval time.Duration
	nextSlot time.Time
}

func (g *gate) reserve() time.Duration {
	g.mu.Lock()
	defer g.mu.Unlock()
	now := time.Now()
	slot := g.nextSlot
	if slot.Before(now) {
		slot = now
	}
	g.nextSlot = slot.Add(g.interval)
	return slot.Sub(now)
}

func (g *gate) wait(ctx context.Context) error {
	delay := g.reserve()
	if delay <= 0 {
		return nil
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
