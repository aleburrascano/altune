package trackers

import (
	"altune/go-api/internal/discovery/adapters/providers/providerhttp"
	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/discovery/ports"
	"altune/go-api/internal/shared/textnorm"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	sheetsBaseURL  = "https://sheets.googleapis.com/v4/spreadsheets/"
	fieldsMask     = "sheets.data.rowData.values(formattedValue,hyperlink,textFormatRuns.format.link.uri)"
	hubRange       = "Artists!A:A"
	artistRange    = "Unreleased"
	maxSheetBytes  = 16 << 20
	hubIndexTTL    = 24 * time.Hour
	requestTimeout = 30 * time.Second
	apiKeyHeader   = "X-Goog-Api-Key"
)

var _ ports.TrackerSongs = (*Client)(nil)

type Client struct {
	http    *http.Client
	baseURL string
	apiKey  string
	hubID   string
	index   ports.TrackerIndexCache
}

func NewClient(liveClient *http.Client, apiKey, hubSheetID string, index ports.TrackerIndexCache) *Client {
	own := *liveClient
	own.Timeout = requestTimeout
	return &Client{http: &own, baseURL: sheetsBaseURL, apiKey: apiKey, hubID: hubSheetID, index: index}
}

func (c *Client) ArtistSongs(ctx context.Context, artistName string) ([]domain.UnreleasedSong, error) {
	index, err := c.hubIndex(ctx)
	if err != nil {
		return nil, fmt.Errorf("tracker hub: %w", err)
	}
	sheetID, ok := index[textnorm.NormalizeForIdentity(artistName)]
	if !ok {
		return []domain.UnreleasedSong{}, nil
	}
	rows, err := c.artistRows(ctx, sheetID)
	if err != nil {
		return nil, fmt.Errorf("tracker sheet: %w", err)
	}
	return songsFromRows(artistName, rows), nil
}

func (c *Client) hubIndex(ctx context.Context) (map[string]string, error) {
	if c.index != nil {
		if cached, ok := c.index.Get(ctx); ok {
			return cached, nil
		}
	}
	body, err := c.fetch(ctx, c.hubID, hubRange)
	if err != nil {
		return nil, err
	}
	index, err := parseHub(body)
	if err != nil {
		return nil, providerhttp.BadPayloadError{Cause: err}
	}
	c.storeIndex(ctx, index)
	return index, nil
}

func (c *Client) storeIndex(ctx context.Context, index map[string]string) {
	if c.index == nil || len(index) == 0 {
		return
	}
	if err := c.index.Set(ctx, index, hubIndexTTL); err != nil {
		slog.WarnContext(ctx, "trackers.index_cache_set_failed", "error", err)
	}
}

func (c *Client) artistRows(ctx context.Context, sheetID string) ([]trackerRow, error) {
	body, err := c.fetch(ctx, sheetID, artistRange)
	if err != nil {
		return nil, tolerateMissingTab(ctx, sheetID, err)
	}
	rows, mapped, err := parseSheet(body)
	if err != nil {
		return nil, providerhttp.BadPayloadError{Cause: err}
	}
	if !mapped {
		slog.WarnContext(ctx, "trackers.unmapped_sheet", "sheet_id", sheetID)
	}
	return rows, nil
}

func tolerateMissingTab(ctx context.Context, sheetID string, err error) error {
	var status providerhttp.StatusError
	if errors.As(err, &status) && status.Status == http.StatusBadRequest {
		slog.WarnContext(ctx, "trackers.unmapped_sheet", "sheet_id", sheetID, "reason", "no Unreleased tab")
		return nil
	}
	return err
}

func (c *Client) fetch(ctx context.Context, sheetID, sheetRange string) ([]byte, error) {
	query := url.Values{"ranges": {sheetRange}, "prettyPrint": {"false"}, "fields": {fieldsMask}}
	target := c.baseURL + url.PathEscape(strings.TrimSpace(sheetID)) + "?" + query.Encode()
	_, body, err := providerhttp.GetBytesCapped(ctx, c.http, target, maxSheetBytes, providerhttp.WithHeader(apiKeyHeader, c.apiKey))
	return body, err
}
