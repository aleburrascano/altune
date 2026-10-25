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
	"slices"
	"strings"
	"time"
)

const (
	sheetsBaseURL  = "https://sheets.googleapis.com/v4/spreadsheets/"
	fieldsMask     = "sheets.data.rowData.values(formattedValue,hyperlink,textFormatRuns.format.link.uri)"
	hubRange       = "Artists!A:A"
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
	if err != nil && !errors.Is(err, ports.ErrTrackerPartial) {
		return nil, fmt.Errorf("tracker sheet: %w", err)
	}
	return songsFromRows(artistName, rows), err
}

func (c *Client) Artists(ctx context.Context) ([]string, error) {
	index, err := c.hubIndex(ctx)
	if err != nil {
		return nil, fmt.Errorf("tracker hub: %w", err)
	}
	names := make([]string, 0, len(index))
	for name := range index {
		names = append(names, name)
	}
	slices.Sort(names)
	return names, nil
}

func (c *Client) hubIndex(ctx context.Context) (map[string]string, error) {
	if c.index != nil {
		if cached, ok := c.index.Get(ctx); ok {
			return cached, nil
		}
	}
	body, err := c.fetch(ctx, c.hubID, gridQuery(hubRange))
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
	tabs, err := c.findTabs(ctx, sheetID)
	if err != nil {
		return nil, err
	}
	unreleased, err := c.tabRows(ctx, sheetID, tabs.unreleased, parseSheet)
	if err != nil {
		return nil, err
	}
	released, err := c.tabRows(ctx, sheetID, tabs.released, parseReleased)
	if err != nil {
		slog.WarnContext(ctx, "trackers.released_tab_failed", "sheet_id", sheetID, "error", err)
		return unreleased, fmt.Errorf("%w: released tab: %w", ports.ErrTrackerPartial, err)
	}
	return append(unreleased, released...), nil
}

type tabParser func(body []byte) (rows []trackerRow, mapped bool, err error)

func (c *Client) tabRows(ctx context.Context, sheetID, tab string, parse tabParser) ([]trackerRow, error) {
	if tab == "" {
		return nil, nil
	}
	body, err := c.fetch(ctx, sheetID, gridQuery(tabRange(tab)))
	if err != nil {
		return nil, err
	}
	rows, mapped, err := parse(body)
	if err != nil {
		return nil, providerhttp.BadPayloadError{Cause: err}
	}
	if !mapped {
		slog.WarnContext(ctx, "trackers.unmapped_sheet", "sheet_id", sheetID, "tab", tab)
	}
	return rows, nil
}

func (c *Client) findTabs(ctx context.Context, sheetID string) (sheetTabs, error) {
	body, err := c.fetch(ctx, sheetID, url.Values{"fields": {tabsMask}})
	if err != nil {
		return sheetTabs{}, err
	}
	tabs, err := pickTabs(body)
	if err != nil {
		return sheetTabs{}, providerhttp.BadPayloadError{Cause: err}
	}
	if tabs.unreleased == "" {
		slog.WarnContext(ctx, "trackers.unmapped_sheet", "sheet_id", sheetID, "reason", "no unreleased tab")
	}
	return tabs, nil
}

func gridQuery(sheetRange string) url.Values {
	return url.Values{"ranges": {sheetRange}, "fields": {fieldsMask}}
}

func (c *Client) fetch(ctx context.Context, sheetID string, query url.Values) ([]byte, error) {
	query.Set("prettyPrint", "false")
	target := c.baseURL + url.PathEscape(strings.TrimSpace(sheetID)) + "?" + query.Encode()
	_, body, err := providerhttp.GetBytesCapped(ctx, c.http, target, maxSheetBytes, providerhttp.WithHeader(apiKeyHeader, c.apiKey))
	return body, err
}
