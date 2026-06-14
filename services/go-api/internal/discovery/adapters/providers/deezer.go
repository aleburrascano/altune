package providers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"altune/go-api/internal/discovery/domain"
)

type DeezerAdapter struct {
	client *http.Client
}

func NewDeezerAdapter(client *http.Client) *DeezerAdapter {
	return &DeezerAdapter{client: client}
}

func (a *DeezerAdapter) Name() domain.ProviderName { return domain.ProviderDeezer }

func (a *DeezerAdapter) SupportedKinds() map[domain.ResultKind]bool {
	return map[domain.ResultKind]bool{
		domain.ResultKindTrack:  true,
		domain.ResultKindAlbum:  true,
		domain.ResultKindArtist: true,
	}
}

func (a *DeezerAdapter) Search(ctx context.Context, query string, kinds map[domain.ResultKind]bool) ([]domain.SearchResult, error) {
	var results []domain.SearchResult

	for kind := range kinds {
		if !a.SupportedKinds()[kind] {
			continue
		}

		endpoint := deezerSearchEndpoint(kind)
		if endpoint == "" {
			continue
		}

		u := fmt.Sprintf("https://api.deezer.com/search/%s?q=%s&limit=10", endpoint, url.QueryEscape(query))

		req, err := http.NewRequestWithContext(ctx, "GET", u, nil)
		if err != nil {
			continue
		}

		resp, err := a.client.Do(req)
		if err != nil {
			continue
		}
		defer resp.Body.Close()

		if resp.StatusCode != 200 {
			continue
		}

		var body deezerSearchResponse
		if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
			continue
		}

		for _, item := range body.Data {
			sr := mapDeezerResult(item, kind)
			results = append(results, sr)
		}
	}

	return results, nil
}

func deezerSearchEndpoint(kind domain.ResultKind) string {
	switch kind {
	case domain.ResultKindTrack:
		return "track"
	case domain.ResultKindAlbum:
		return "album"
	case domain.ResultKindArtist:
		return "artist"
	default:
		return ""
	}
}

func mapDeezerResult(item deezerItem, kind domain.ResultKind) domain.SearchResult {
	var title, subtitle, imageURL string
	extras := make(map[string]any)

	switch kind {
	case domain.ResultKindTrack:
		title = item.Title
		if item.Artist != nil {
			subtitle = item.Artist.Name
		}
		if item.Album != nil {
			imageURL = item.Album.CoverBig
			extras["album"] = item.Album.Title
		}
		if item.ISRC != "" {
			extras["isrc"] = item.ISRC
		}
		extras["duration"] = item.Duration
	case domain.ResultKindAlbum:
		title = item.Title
		if item.Artist != nil {
			subtitle = item.Artist.Name
		}
		imageURL = item.CoverBig
		if item.RecordType != "" {
			extras["record_type"] = item.RecordType
		}
		if item.ReleaseDate != "" {
			extras["release_date"] = item.ReleaseDate
		}
		if item.NbTracks > 0 {
			extras["track_count"] = item.NbTracks
		}
	case domain.ResultKindArtist:
		title = item.Name
		imageURL = item.PictureBig
		if item.NbFan > 0 {
			extras["nb_fan"] = item.NbFan
		}
	}

	return domain.SearchResult{
		Kind:       kind,
		Title:      title,
		Subtitle:   subtitle,
		ImageURL:   imageURL,
		Confidence: domain.ConfidenceLow,
		Sources: []domain.SourceRef{{
			Provider:   domain.ProviderDeezer,
			ExternalID: fmt.Sprintf("%d", item.ID),
			URL:        item.Link,
		}},
		Extras: extras,
	}
}

// Deezer API response types

type deezerSearchResponse struct {
	Data []deezerItem `json:"data"`
}

type deezerItem struct {
	ID          int64        `json:"id"`
	Title       string       `json:"title"`
	Name        string       `json:"name"`
	Link        string       `json:"link"`
	Duration    int          `json:"duration"`
	ISRC        string       `json:"isrc"`
	CoverBig    string       `json:"cover_big"`
	PictureBig  string       `json:"picture_big"`
	Artist      *deezerRef   `json:"artist"`
	Album       *deezerAlbum `json:"album"`
	RecordType  string       `json:"record_type"`
	ReleaseDate string       `json:"release_date"`
	NbTracks    int          `json:"nb_tracks"`
	Rank        int64        `json:"rank"`
	NbFan       int64        `json:"nb_fan"`
}

type deezerRef struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

type deezerAlbum struct {
	ID       int64  `json:"id"`
	Title    string `json:"title"`
	CoverBig string `json:"cover_big"`
}

// Resolve implements ArtworkResolver — best-effort cover lookup by search.
func (a *DeezerAdapter) Resolve(ctx context.Context, kind domain.ResultKind, title, subtitle string, mbid string) (string, error) {
	query := title
	if subtitle != "" {
		query = subtitle + " " + title
	}
	endpoint := deezerSearchEndpoint(kind)
	if endpoint == "" {
		endpoint = "track"
	}

	u := fmt.Sprintf("https://api.deezer.com/search/%s?q=%s&limit=1", endpoint, url.QueryEscape(query))
	req, err := http.NewRequestWithContext(ctx, "GET", u, nil)
	if err != nil {
		return "", nil
	}
	resp, err := a.client.Do(req)
	if err != nil {
		return "", nil
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return "", nil
	}

	var body deezerSearchResponse
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return "", nil
	}
	for _, item := range body.Data {
		var img string
		if item.Album != nil && item.Album.CoverBig != "" {
			img = item.Album.CoverBig
		} else if item.CoverBig != "" {
			img = item.CoverBig
		} else if item.PictureBig != "" {
			img = item.PictureBig
		}
		if img != "" && !IsDeezerPlaceholder(img) {
			return img, nil
		}
	}
	return "", nil
}

// --- AlbumContentProvider + ArtistContentProvider ---

func (a *DeezerAdapter) GetAlbumTracks(ctx context.Context, _ domain.ProviderName, externalID string) ([]domain.SearchResult, error) {
	u := fmt.Sprintf("https://api.deezer.com/album/%s/tracks?limit=50", externalID)
	return a.fetchList(ctx, u, func(item deezerItem) domain.SearchResult {
		return mapDeezerResult(item, domain.ResultKindTrack)
	})
}

func (a *DeezerAdapter) GetArtistTopTracks(ctx context.Context, _ domain.ProviderName, externalID string) ([]domain.SearchResult, error) {
	u := fmt.Sprintf("https://api.deezer.com/artist/%s/top?limit=10", externalID)
	return a.fetchList(ctx, u, func(item deezerItem) domain.SearchResult {
		return mapDeezerResult(item, domain.ResultKindTrack)
	})
}

func (a *DeezerAdapter) GetArtistAlbums(ctx context.Context, _ domain.ProviderName, externalID string) ([]domain.SearchResult, error) {
	u := fmt.Sprintf("https://api.deezer.com/artist/%s/albums?limit=25", externalID)
	return a.fetchList(ctx, u, func(item deezerItem) domain.SearchResult {
		return mapDeezerResult(item, domain.ResultKindAlbum)
	})
}

func (a *DeezerAdapter) fetchList(ctx context.Context, u string, mapper func(deezerItem) domain.SearchResult) ([]domain.SearchResult, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", u, nil)
	if err != nil {
		return nil, err
	}
	resp, err := a.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("deezer api returned %d", resp.StatusCode)
	}

	var body deezerSearchResponse
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, err
	}

	var results []domain.SearchResult
	for _, item := range body.Data {
		results = append(results, mapper(item))
	}
	return results, nil
}

const DeezerPlaceholderImage = "https://e-cdns-images.dzcdn.net/images/artist//500x500-000000-80-0-0.jpg"

func IsDeezerPlaceholder(u string) bool {
	return strings.Contains(u, "/images/artist//")
}
