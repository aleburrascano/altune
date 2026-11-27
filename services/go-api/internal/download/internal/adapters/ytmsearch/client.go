package ytmsearch

import (
	acqdomain "altune/go-api/internal/download/internal/core/domain"
	"altune/go-api/internal/platform/outbound"
	"altune/go-api/internal/shared/youtubemusicapi"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
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

func ForClient(client *http.Client) (*Client, error) {
	if client == nil {
		return nil, ErrHTTPRequired
	}
	return New(Config{HTTP: client, Timeout: client.Timeout})
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
	target := c.oembedURL + "?" + query.Encode()
	raw, err := c.call(ctx, "/oembed", func(ctx context.Context) (int, []byte, error) {
		return outbound.GetBytesCapped(ctx, c.http, target, responseBodyCap, outbound.WithHeader("User-Agent", youtubemusicapi.UserAgent))
	})
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
	return c.call(ctx, endpoint, func(ctx context.Context) (int, []byte, error) {
		return c.postOnce(ctx, target, payload)
	})
}

func (c *Client) postOnce(ctx context.Context, target string, payload []byte) (int, []byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, target, bytes.NewReader(payload))
	if err != nil {
		return 0, nil, fmt.Errorf("build the request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json; charset=utf-8")
	req.Header.Set("Referer", DefaultMusicURL+"/")
	req.Header.Set("User-Agent", youtubemusicapi.UserAgent)
	status, raw, err := outbound.DoCapped(c.http, req, responseBodyCap)
	if err == nil && status != http.StatusOK {
		err = outbound.StatusError{Status: status}
	}
	return status, raw, err
}

func (c *Client) call(ctx context.Context, label string, fetch func(context.Context) (int, []byte, error)) ([]byte, error) {
	ctx, err := youtubemusicapi.ReserveSlot(ctx, c.http)
	if err != nil {
		return nil, fmt.Errorf("reserve slot: %w", err)
	}
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	_, raw, err := fetch(ctx)
	if err != nil {
		return nil, fmt.Errorf("ytmsearch: %s: %w", label, err)
	}
	return raw, nil
}
