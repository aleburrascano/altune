package main

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

const (
	deezerQuotaCode    = 4
	deezerNoDataCode   = 800
	deezerMaxAttempts  = 5
	deezerRetryBackoff = 2 * time.Second
	deezerMaxBackoff   = 60 * time.Second
	httpTooManyRetries = http.StatusTooManyRequests
)

var (
	errCatalogUnknown = errors.New("catalog does not know this track")
	errCatalogBlocked = errors.New("catalog is blocking requests")
)

func blocked(format string, args ...any) error {
	return fmt.Errorf("%w: %s", errCatalogBlocked, fmt.Sprintf(format, args...))
}

type catalogTrack struct {
	ID              int64
	Title           string
	Artist          string
	Contributors    []string
	Album           string
	AlbumImageMD5   string
	CoverURL        string
	FullCoverURL    string
	DurationSeconds float64
	ReleaseYear     int
	ISRC            string
	AlbumID         int64
	TrackPosition   int
	DiskNumber      int
}

type deezerClient struct {
	http    *http.Client
	baseURL string
	limiter *rate.Limiter
	backoff time.Duration
	ceiling time.Duration
	albums  albumCache
}

type albumInfo struct {
	Genres     []string
	RecordType string
}

type albumCache struct {
	mu      sync.Mutex
	byAlbum map[int64]albumInfo
}

func (g *albumCache) get(albumID int64) (albumInfo, bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	info, hit := g.byAlbum[albumID]
	return info, hit
}

func (g *albumCache) put(albumID int64, info albumInfo) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.byAlbum == nil {
		g.byAlbum = map[int64]albumInfo{}
	}
	g.byAlbum[albumID] = info
}

func newDeezerClient(httpClient *http.Client, baseURL string, requestsPerSecond float64) *deezerClient {
	return &deezerClient{
		http:    httpClient,
		baseURL: baseURL,
		limiter: rate.NewLimiter(rate.Limit(requestsPerSecond), 1),
		backoff: deezerRetryBackoff,
		ceiling: deezerMaxBackoff,
	}
}

type deezerTrackBody struct {
	ID          int64  `json:"id"`
	Title       string `json:"title"`
	ISRC        string `json:"isrc"`
	Duration    int    `json:"duration"`
	ReleaseDate string `json:"release_date"`
	Position    int    `json:"track_position"`
	Disk        int    `json:"disk_number"`
	Artist      struct {
		Name string `json:"name"`
	} `json:"artist"`
	Contributors []struct {
		Name string `json:"name"`
	} `json:"contributors"`
	Album struct {
		ID       int64  `json:"id"`
		Title    string `json:"title"`
		MD5Image string `json:"md5_image"`
		Cover    string `json:"cover_medium"`
		CoverBig string `json:"cover_big"`
		CoverXL  string `json:"cover_xl"`
	} `json:"album"`
}

type deezerSearchBody struct {
	Data []deezerTrackBody `json:"data"`
}

func (c *deezerClient) TrackByISRC(ctx context.Context, isrc string) (catalogTrack, error) {
	var body deezerTrackBody
	if err := c.getJSON(ctx, "/2.0/track/isrc:"+url.PathEscape(isrc), &body); err != nil {
		return catalogTrack{}, err
	}
	return toCatalogTrack(body), nil
}

func (c *deezerClient) Search(ctx context.Context, artist, title string) ([]catalogTrack, error) {
	query := strings.TrimSpace(artist + " " + title)
	var body deezerSearchBody
	if err := c.getJSON(ctx, "/2.0/search?q="+url.QueryEscape(query)+"&limit=10", &body); err != nil {
		return nil, err
	}
	found := make([]catalogTrack, 0, len(body.Data))
	for _, item := range body.Data {
		found = append(found, toCatalogTrack(item))
	}
	return found, nil
}

func (c *deezerClient) TrackByID(ctx context.Context, id int64) (catalogTrack, error) {
	var body deezerTrackBody
	if err := c.getJSON(ctx, "/2.0/track/"+strconv.FormatInt(id, 10), &body); err != nil {
		return catalogTrack{}, err
	}
	return toCatalogTrack(body), nil
}

type deezerAlbumBody struct {
	RecordType string `json:"record_type"`
	Genres     struct {
		Data []struct {
			Name string `json:"name"`
		} `json:"data"`
	} `json:"genres"`
}

func (c *deezerClient) AlbumGenres(ctx context.Context, albumID int64) ([]string, error) {
	info, err := c.albumInfo(ctx, albumID)
	return info.Genres, err
}

func (c *deezerClient) AlbumRecordType(ctx context.Context, albumID int64) (string, error) {
	info, err := c.albumInfo(ctx, albumID)
	return info.RecordType, err
}

func (c *deezerClient) albumInfo(ctx context.Context, albumID int64) (albumInfo, error) {
	if cached, hit := c.albums.get(albumID); hit {
		return cached, nil
	}
	var body deezerAlbumBody
	if err := c.getJSON(ctx, "/2.0/album/"+strconv.FormatInt(albumID, 10), &body); err != nil {
		return albumInfo{}, err
	}
	names := make([]string, 0, len(body.Genres.Data))
	for _, genre := range body.Genres.Data {
		names = append(names, genre.Name)
	}
	info := albumInfo{Genres: names, RecordType: body.RecordType}
	c.albums.put(albumID, info)
	return info, nil
}

func toCatalogTrack(body deezerTrackBody) catalogTrack {
	contributors := make([]string, 0, len(body.Contributors))
	for _, contributor := range body.Contributors {
		contributors = append(contributors, contributor.Name)
	}
	year := 0
	if len(body.ReleaseDate) >= 4 {
		year, _ = strconv.Atoi(body.ReleaseDate[:4])
	}
	return catalogTrack{
		ID:              body.ID,
		Title:           body.Title,
		Artist:          body.Artist.Name,
		Contributors:    contributors,
		Album:           body.Album.Title,
		AlbumImageMD5:   body.Album.MD5Image,
		CoverURL:        body.Album.Cover,
		FullCoverURL:    cmp.Or(body.Album.CoverXL, body.Album.CoverBig, body.Album.Cover),
		DurationSeconds: float64(body.Duration),
		ReleaseYear:     year,
		ISRC:            body.ISRC,
		AlbumID:         body.Album.ID,
		TrackPosition:   body.Position,
		DiskNumber:      body.Disk,
	}
}

func (c *deezerClient) getJSON(ctx context.Context, path string, into any) error {
	for attempt := 1; ; attempt++ {
		code, err := c.getOnce(ctx, path, into)
		switch {
		case err != nil:
			return err
		case code == deezerNoDataCode:
			return errCatalogUnknown
		case isThrottle(code) && attempt >= deezerMaxAttempts:
			return blocked("still throttled (code %d) after %d attempts on %s", code, attempt, path)
		case isThrottle(code):
			if err := sleepContext(ctx, c.delayBefore(attempt)); err != nil {
				return err
			}
		case code != 0:
			return fmt.Errorf("deezer error code %d", code)
		default:
			return nil
		}
	}
}

func isThrottle(code int) bool {
	return code == deezerQuotaCode || code == httpTooManyRetries
}

func (c *deezerClient) delayBefore(attempt int) time.Duration {
	delay := c.backoff
	for range attempt - 1 {
		delay *= 2
		if delay >= c.ceiling {
			return c.ceiling
		}
	}
	return min(delay, c.ceiling)
}

func (c *deezerClient) getOnce(ctx context.Context, path string, into any) (int, error) {
	if err := c.limiter.Wait(ctx); err != nil {
		return 0, err
	}
	resp, err := c.fetch(ctx, path)
	if err != nil {
		return 0, err
	}
	defer func() { _ = resp.Body.Close() }()
	switch resp.StatusCode {
	case http.StatusOK:
		return decodeBody(resp.Body, path, into)
	case http.StatusForbidden:
		return 0, blocked("HTTP 403 on %s", path)
	case http.StatusTooManyRequests:
		return httpTooManyRetries, nil
	default:
		return 0, fmt.Errorf("fetch %s: status %d", path, resp.StatusCode)
	}
}

func (c *deezerClient) fetch(ctx context.Context, path string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+path, http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("User-Agent", "altune-libraryaudit")
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch %s: %w", path, err)
	}
	return resp, nil
}

func decodeBody(body io.Reader, path string, into any) (int, error) {
	var envelope struct {
		Error *struct {
			Code int `json:"code"`
		} `json:"error"`
	}
	raw := json.RawMessage{}
	if err := json.NewDecoder(body).Decode(&raw); err != nil {
		return 0, blocked("non-JSON response on %s", path)
	}
	if err := json.Unmarshal(raw, &envelope); err == nil && envelope.Error != nil {
		return envelope.Error.Code, nil
	}
	if err := json.Unmarshal(raw, into); err != nil {
		return 0, fmt.Errorf("decode %s: %w", path, err)
	}
	return 0, nil
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
