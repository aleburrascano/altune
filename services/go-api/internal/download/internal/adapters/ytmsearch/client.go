package ytmsearch

import (
	acqdomain "altune/go-api/internal/download/internal/core/domain"
	"altune/go-api/internal/shared/retrysafe"
	"altune/go-api/internal/shared/youtubemusicapi"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

const (
	DefaultMusicURL  = youtubemusicapi.MusicURL
	DefaultOEmbedURL = "https://www.youtube.com/oembed"

	songsFilterParams = "EgWKAQIIAWoMEA4QChADEAQQCRAF"
	responseBodyCap   = 16 << 20
	watchURLPrefix    = "https://www.youtube.com/watch?v="
)

type Config struct {
	HTTP      *http.Client
	MusicURL  string
	OEmbedURL string
	Timeout   time.Duration
}

type Client struct {
	http      *http.Client
	musicURL  string
	oembedURL string
	timeout   time.Duration
}

var (
	ErrHTTPRequired    = errors.New("ytmsearch: an HTTP client is required")
	ErrTimeoutRequired = errors.New("ytmsearch: a positive per-call timeout is required")
)

func New(cfg Config) (*Client, error) {
	if cfg.HTTP == nil {
		return nil, ErrHTTPRequired
	}
	if cfg.Timeout <= 0 {
		return nil, ErrTimeoutRequired
	}
	c := &Client{
		http:      cfg.HTTP,
		musicURL:  cfg.MusicURL,
		oembedURL: cfg.OEmbedURL,
		timeout:   cfg.Timeout,
	}
	if c.musicURL == "" {
		c.musicURL = DefaultMusicURL
	}
	if c.oembedURL == "" {
		c.oembedURL = DefaultOEmbedURL
	}
	return c, nil
}

func (c *Client) Timeout() time.Duration { return c.timeout }

func (c *Client) Watch(ctx context.Context, videoID string) (Watch, error) {
	raw, err := c.postInnerTube(ctx, "next", map[string]any{"videoId": videoID})
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
	raw, err := c.postInnerTube(ctx, "search", map[string]any{"query": query, "params": songsFilterParams})
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
		return fmt.Errorf("ytmsearch: stopped: %w", ctxErr)
	}
	if errors.Is(err, context.DeadlineExceeded) {
		err = fmt.Errorf("ytmsearch: timed out after %v", c.timeout)
	}
	return &acqdomain.SourceUnavailableError{Source: SourceName, Err: err}
}

func (c *Client) OEmbed(ctx context.Context, videoID string) (OEmbed, error) {
	query := url.Values{"url": {watchURLPrefix + videoID}, "format": {"json"}}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.oembedURL+"?"+query.Encode(), http.NoBody)
	if err != nil {
		return OEmbed{}, fmt.Errorf("ytmsearch: build oembed request: %w", err)
	}
	raw, err := c.do(ctx, req)
	if err != nil {
		return OEmbed{}, err
	}
	return ParseOEmbed(raw)
}

func (c *Client) postInnerTube(ctx context.Context, endpoint string, fields map[string]any) ([]byte, error) {
	payload, err := json.Marshal(youtubemusicapi.Body(fields))
	if err != nil {
		return nil, fmt.Errorf("ytmsearch: marshal %s: %w", endpoint, err)
	}
	target := youtubemusicapi.URL(c.musicURL, endpoint)
	req, err := http.NewRequestWithContext(retrysafe.Mark(ctx), http.MethodPost, target, bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("ytmsearch: build %s request: %w", endpoint, err)
	}
	req.Header.Set("Content-Type", "application/json; charset=utf-8")
	req.Header.Set("Referer", DefaultMusicURL+"/")
	return c.do(ctx, req)
}

func (c *Client) do(ctx context.Context, req *http.Request) ([]byte, error) {
	ctx, err := youtubemusicapi.ReserveSlot(ctx, c.http)
	if err != nil {
		return nil, err
	}
	ctx, cancel := c.boundedContext(ctx)
	defer cancel()
	req = req.WithContext(ctx)
	req.Header.Set("User-Agent", youtubemusicapi.UserAgent)
	return c.send(req)
}

func (c *Client) boundedContext(ctx context.Context) (context.Context, context.CancelFunc) {
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
