package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"golang.org/x/time/rate"
)

const (
	musicBrainzBaseURL           = "https://musicbrainz.org"
	musicBrainzRequestsPerSecond = 1.0
	musicBrainzUserAgent         = "altune-libraryaudit/1.0 ( https://github.com/aleburrascano/altune )"
	musicBrainzMaxAttempts       = 5
	musicBrainzRetryBackoff      = 2 * time.Second
	musicBrainzResultLimit       = 25
)

type recording struct {
	ID              string
	Title           string
	Artists         []string
	DurationSeconds float64
	ISRCs           []string
	Releases        []release
}

type release struct {
	ID     string
	Title  string
	Date   string
	Artist string
}

func (r recording) asTrack() catalogTrack {
	track := catalogTrack{Title: r.Title, DurationSeconds: r.DurationSeconds}
	if len(r.Artists) > 0 {
		track.Artist = r.Artists[0]
		track.Contributors = r.Artists
	}
	return track
}

func (r recording) hasISRC(isrc string) bool {
	want := canonicalISRC(isrc)
	for _, listed := range r.ISRCs {
		if canonicalISRC(listed) == want {
			return true
		}
	}
	return false
}

func canonicalISRC(isrc string) string {
	return strings.ToUpper(strings.ReplaceAll(strings.TrimSpace(isrc), "-", ""))
}

type recordingCatalog interface {
	SearchRecordings(ctx context.Context, artist, title string) ([]recording, error)
	RecordingsByISRC(ctx context.Context, isrc string) ([]recording, error)
}

type musicBrainzClient struct {
	http    *http.Client
	baseURL string
	limiter *rate.Limiter
	backoff time.Duration
}

func newMusicBrainzClient(httpClient *http.Client, baseURL string, requestsPerSecond float64) *musicBrainzClient {
	return &musicBrainzClient{
		http:    httpClient,
		baseURL: baseURL,
		limiter: rate.NewLimiter(rate.Limit(requestsPerSecond), 1),
		backoff: musicBrainzRetryBackoff,
	}
}

type musicBrainzRecordingBody struct {
	ID           string                   `json:"id"`
	Title        string                   `json:"title"`
	Length       int                      `json:"length"`
	ISRCs        []string                 `json:"isrcs"`
	Releases     []musicBrainzReleaseBody `json:"releases"`
	ArtistCredit []struct {
		Name   string `json:"name"`
		Artist struct {
			Name string `json:"name"`
		} `json:"artist"`
	} `json:"artist-credit"`
}

type musicBrainzReleaseBody struct {
	ID           string `json:"id"`
	Title        string `json:"title"`
	Date         string `json:"date"`
	ArtistCredit []struct {
		Name       string `json:"name"`
		JoinPhrase string `json:"joinphrase"`
	} `json:"artist-credit"`
}

type musicBrainzSearchBody struct {
	Recordings []musicBrainzRecordingBody `json:"recordings"`
}

func luceneQuote(value string) string {
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(value) + `"`
}

func (c *musicBrainzClient) SearchRecordings(ctx context.Context, artist, title string) ([]recording, error) {
	return c.search(ctx, "recording:"+luceneQuote(baseTitle(title))+" AND artist:"+luceneQuote(artist))
}

func (c *musicBrainzClient) RecordingsByISRC(ctx context.Context, isrc string) ([]recording, error) {
	return c.search(ctx, "isrc:"+luceneQuote(canonicalISRC(isrc)))
}

func (c *musicBrainzClient) search(ctx context.Context, query string) ([]recording, error) {
	path := "/ws/2/recording?query=" + url.QueryEscape(query) + "&inc=isrcs&fmt=json&limit=" + fmt.Sprint(musicBrainzResultLimit)
	var body musicBrainzSearchBody
	if err := c.getJSON(ctx, path, &body); err != nil {
		return nil, err
	}
	found := make([]recording, 0, len(body.Recordings))
	for _, item := range body.Recordings {
		found = append(found, toRecording(item))
	}
	return found, nil
}

func toRecording(body musicBrainzRecordingBody) recording {
	var artists []string
	for _, credit := range body.ArtistCredit {
		artists = append(artists, credit.Name)
		if credit.Artist.Name != credit.Name {
			artists = append(artists, credit.Artist.Name)
		}
	}
	releases := make([]release, 0, len(body.Releases))
	for _, item := range body.Releases {
		releases = append(releases, toRelease(item))
	}
	return recording{
		Releases:        releases,
		ID:              body.ID,
		Title:           body.Title,
		Artists:         artists,
		DurationSeconds: float64(body.Length) / 1000,
		ISRCs:           body.ISRCs,
	}
}

func toRelease(body musicBrainzReleaseBody) release {
	var artist strings.Builder
	for _, credit := range body.ArtistCredit {
		artist.WriteString(credit.Name + credit.JoinPhrase)
	}
	return release{ID: body.ID, Title: body.Title, Date: body.Date, Artist: strings.TrimSpace(artist.String())}
}

type musicBrainzISRCBody struct {
	Recordings []musicBrainzRecordingBody `json:"recordings"`
}

func (c *musicBrainzClient) RecordingsWithReleases(ctx context.Context, isrc string) ([]recording, error) {
	var body musicBrainzISRCBody
	path := "/ws/2/isrc/" + url.PathEscape(canonicalISRC(isrc)) + "?inc=releases+artist-credits&fmt=json"
	err := c.getJSON(ctx, path, &body)
	if errors.Is(err, errCatalogUnknown) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	found := make([]recording, 0, len(body.Recordings))
	for _, item := range body.Recordings {
		found = append(found, toRecording(item))
	}
	return found, nil
}

func (c *musicBrainzClient) getJSON(ctx context.Context, path string, into any) error {
	for attempt := 1; ; attempt++ {
		status, err := c.getOnce(ctx, path, into)
		switch {
		case err != nil:
			return err
		case status == http.StatusOK:
			return nil
		case status == http.StatusNotFound:
			return errCatalogUnknown
		case !isMusicBrainzThrottle(status):
			return fmt.Errorf("musicbrainz %s: status %d", path, status)
		case attempt >= musicBrainzMaxAttempts:
			return blocked("musicbrainz still throttled (HTTP %d) after %d attempts", status, attempt)
		}
		if err := sleepContext(ctx, c.backoff<<(attempt-1)); err != nil {
			return err
		}
	}
}

func isMusicBrainzThrottle(status int) bool {
	return status == http.StatusServiceUnavailable || status == http.StatusTooManyRequests
}

func (c *musicBrainzClient) getOnce(ctx context.Context, path string, into any) (int, error) {
	resp, err := c.fetch(ctx, path)
	if err != nil {
		return 0, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode == http.StatusForbidden {
		return 0, blocked("musicbrainz HTTP 403 on %s", path)
	}
	if resp.StatusCode != http.StatusOK {
		return resp.StatusCode, nil
	}
	if err := json.NewDecoder(resp.Body).Decode(into); err != nil {
		return 0, errors.Join(errors.New("decode musicbrainz response"), err)
	}
	return resp.StatusCode, nil
}

func (c *musicBrainzClient) fetch(ctx context.Context, path string) (*http.Response, error) {
	if err := c.limiter.Wait(ctx); err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+path, http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("User-Agent", musicBrainzUserAgent)
	req.Header.Set("Accept", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch %s: %w", path, err)
	}
	return resp, nil
}
