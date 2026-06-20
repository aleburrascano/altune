package providers

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"altune/go-api/internal/discovery/domain"
)

// AIDEV-NOTE: Tidal OpenAPI v2 endpoints need verification after developer
// account registration. The URL paths and response shapes below are based on
// the official tidal-sdk-web generated types and community documentation.
// Test against the real API before relying on this adapter in production.

const (
	tidalAPIBase   = "https://openapi.tidal.com/v2"
	tidalTokenURL  = "https://auth.tidal.com/v1/oauth2/token"
	tidalCountry   = "US"
)

type TidalAdapter struct {
	client       *http.Client
	clientID     string
	clientSecret string

	mu    sync.Mutex
	token string
	expAt time.Time
}

func NewTidalAdapter(client *http.Client, clientID, clientSecret string) *TidalAdapter {
	return &TidalAdapter{
		client:       client,
		clientID:     clientID,
		clientSecret: clientSecret,
	}
}

func (a *TidalAdapter) Name() domain.ProviderName { return domain.ProviderTidal }

func (a *TidalAdapter) TidalProviderTag() string { return "tidal" }

func (a *TidalAdapter) SupportedKinds() map[domain.ResultKind]bool {
	return map[domain.ResultKind]bool{
		domain.ResultKindTrack:  true,
		domain.ResultKindAlbum:  true,
		domain.ResultKindArtist: true,
	}
}

func (a *TidalAdapter) SearchTimeout() time.Duration {
	return 3 * time.Second
}

func (a *TidalAdapter) Search(ctx context.Context, query string, kinds map[domain.ResultKind]bool) ([]domain.SearchResult, error) {
	tok, err := a.ensureToken(ctx)
	if err != nil {
		return nil, fmt.Errorf("tidal auth: %w", err)
	}

	var results []domain.SearchResult
	for kind := range kinds {
		if !a.SupportedKinds()[kind] {
			continue
		}
		items, err := a.searchKind(ctx, tok, query, kind)
		if err != nil {
			slog.WarnContext(ctx, "tidal.search_kind_failed",
				"kind", kind.String(), "query", query, "error", err)
			continue
		}
		results = append(results, items...)
	}
	return results, nil
}

func (a *TidalAdapter) searchKind(ctx context.Context, token, query string, kind domain.ResultKind) ([]domain.SearchResult, error) {
	resource := tidalResourceForKind(kind)
	if resource == "" {
		return nil, nil
	}

	u := fmt.Sprintf("%s/%s?countryCode=%s&filter[query]=%s&page[limit]=15",
		tidalAPIBase, resource, tidalCountry, url.QueryEscape(query))

	body, err := a.doGet(ctx, token, u)
	if err != nil {
		return nil, err
	}

	var resp tidalListResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("tidal unmarshal: %w", err)
	}

	var results []domain.SearchResult
	for _, item := range resp.Data {
		r := mapTidalResult(item, kind)
		if r.Title != "" {
			results = append(results, r)
		}
	}
	return results, nil
}

func (a *TidalAdapter) GetArtistTopTracks(ctx context.Context, _ domain.ProviderName, externalID string) ([]domain.SearchResult, error) {
	tok, err := a.ensureToken(ctx)
	if err != nil {
		return nil, fmt.Errorf("tidal auth: %w", err)
	}

	u := fmt.Sprintf("%s/artists/%s?countryCode=%s&include=tracks", tidalAPIBase, externalID, tidalCountry)
	body, err := a.doGet(ctx, tok, u)
	if err != nil {
		return nil, err
	}

	var resp tidalIncludeResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("tidal unmarshal: %w", err)
	}

	var results []domain.SearchResult
	for _, item := range resp.Included {
		if item.Type == "tracks" {
			r := mapTidalResult(item, domain.ResultKindTrack)
			if r.Title != "" {
				results = append(results, r)
			}
		}
	}
	if len(results) > 10 {
		results = results[:10]
	}
	return results, nil
}

func (a *TidalAdapter) GetArtistAlbums(ctx context.Context, _ domain.ProviderName, externalID string) ([]domain.SearchResult, error) {
	tok, err := a.ensureToken(ctx)
	if err != nil {
		return nil, fmt.Errorf("tidal auth: %w", err)
	}

	u := fmt.Sprintf("%s/artists/%s?countryCode=%s&include=albums", tidalAPIBase, externalID, tidalCountry)
	body, err := a.doGet(ctx, tok, u)
	if err != nil {
		return nil, err
	}

	var resp tidalIncludeResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("tidal unmarshal: %w", err)
	}

	var results []domain.SearchResult
	for _, item := range resp.Included {
		if item.Type == "albums" {
			r := mapTidalResult(item, domain.ResultKindAlbum)
			if r.Title != "" {
				results = append(results, r)
			}
		}
	}
	return results, nil
}

// --- Auth ---

func (a *TidalAdapter) ensureToken(ctx context.Context) (string, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.token != "" && time.Now().Before(a.expAt) {
		return a.token, nil
	}

	data := url.Values{
		"grant_type":    {"client_credentials"},
		"client_id":     {a.clientID},
		"client_secret": {a.clientSecret},
	}

	req, err := http.NewRequestWithContext(ctx, "POST", tidalTokenURL, strings.NewReader(data.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := a.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return "", fmt.Errorf("tidal token: status %d", resp.StatusCode)
	}

	var tokenResp struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
		return "", err
	}

	a.token = tokenResp.AccessToken
	a.expAt = time.Now().Add(time.Duration(tokenResp.ExpiresIn-60) * time.Second)
	return a.token, nil
}

// --- HTTP ---

func (a *TidalAdapter) doGet(ctx context.Context, token, u string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/vnd.api+json")

	resp, err := a.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == 401 {
		a.mu.Lock()
		a.token = ""
		a.mu.Unlock()
		return nil, fmt.Errorf("tidal: unauthorized (token expired)")
	}

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("tidal: status %d", resp.StatusCode)
	}

	buf := make([]byte, 0, 4096)
	tmp := make([]byte, 4096)
	for {
		n, readErr := resp.Body.Read(tmp)
		if n > 0 {
			buf = append(buf, tmp[:n]...)
		}
		if readErr != nil {
			break
		}
	}
	return buf, nil
}

// --- Response types (JSON:API) ---

type tidalListResponse struct {
	Data []tidalResource `json:"data"`
}

type tidalIncludeResponse struct {
	Data     tidalResource   `json:"data"`
	Included []tidalResource `json:"included"`
}

type tidalResource struct {
	ID         string              `json:"id"`
	Type       string              `json:"type"`
	Attributes tidalAttributes     `json:"attributes"`
}

type tidalAttributes struct {
	Title       string `json:"title"`
	Name        string `json:"name"`
	ISRC        string `json:"isrc"`
	Duration    int    `json:"duration"`
	Popularity  int    `json:"popularity"`
	ReleaseDate string `json:"releaseDate"`
	ImageURL    string `json:"imageUrl"`
	ArtistName  string `json:"artistName"`
	AlbumName   string `json:"albumName"`
	TrackCount  int    `json:"numberOfTracks"`
}

func tidalResourceForKind(kind domain.ResultKind) string {
	switch kind {
	case domain.ResultKindTrack:
		return "tracks"
	case domain.ResultKindAlbum:
		return "albums"
	case domain.ResultKindArtist:
		return "artists"
	default:
		return ""
	}
}

func mapTidalResult(item tidalResource, kind domain.ResultKind) domain.SearchResult {
	attr := item.Attributes
	var title, subtitle, imageURL string
	extras := make(map[string]any)

	switch kind {
	case domain.ResultKindTrack:
		title = attr.Title
		subtitle = attr.ArtistName
		imageURL = attr.ImageURL
		if attr.ISRC != "" {
			extras["isrc"] = attr.ISRC
		}
		if attr.Duration > 0 {
			extras["duration"] = attr.Duration
		}
		if attr.AlbumName != "" {
			extras["album"] = attr.AlbumName
		}
	case domain.ResultKindAlbum:
		title = attr.Title
		subtitle = attr.ArtistName
		imageURL = attr.ImageURL
		if attr.ReleaseDate != "" {
			extras["release_date"] = attr.ReleaseDate
		}
		if attr.TrackCount > 0 {
			extras["track_count"] = attr.TrackCount
		}
	case domain.ResultKindArtist:
		title = attr.Name
		imageURL = attr.ImageURL
		if attr.Popularity > 0 {
			extras["popularity"] = int64(attr.Popularity)
		}
	}

	return domain.SearchResult{
		Kind:       kind,
		Title:      title,
		Subtitle:   subtitle,
		ImageURL:   imageURL,
		Confidence: domain.ConfidenceLow,
		Sources: []domain.SourceRef{{
			Provider:   domain.ProviderTidal,
			ExternalID: item.ID,
			URL:        "",
		}},
		Extras: extras,
	}
}
