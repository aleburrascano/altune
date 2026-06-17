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

type ITunesAdapter struct {
	client *http.Client
}

func NewITunesAdapter(client *http.Client) *ITunesAdapter {
	return &ITunesAdapter{client: client}
}

func (a *ITunesAdapter) Name() domain.ProviderName { return domain.ProviderITunes }

func (a *ITunesAdapter) SupportedKinds() map[domain.ResultKind]bool {
	return map[domain.ResultKind]bool{
		domain.ResultKindTrack:  true,
		domain.ResultKindAlbum:  true,
		domain.ResultKindArtist: true,
	}
}

func (a *ITunesAdapter) Search(ctx context.Context, query string, kinds map[domain.ResultKind]bool) ([]domain.SearchResult, error) {
	var results []domain.SearchResult

	for kind := range kinds {
		if !a.SupportedKinds()[kind] {
			continue
		}

		items, err := a.searchKind(ctx, query, kind)
		if err != nil {
			continue
		}
		results = append(results, items...)
	}

	return results, nil
}

func (a *ITunesAdapter) searchKind(ctx context.Context, query string, kind domain.ResultKind) ([]domain.SearchResult, error) {
	entity := itunesEntity(kind)
	u := fmt.Sprintf("https://itunes.apple.com/search?term=%s&entity=%s&limit=15", url.QueryEscape(query), entity)

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
		return nil, fmt.Errorf("itunes returned %d", resp.StatusCode)
	}

	var body itunesResponse
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, err
	}

	var results []domain.SearchResult
	for _, item := range body.Results {
		results = append(results, mapITunesResult(item, kind))
	}
	return results, nil
}

func itunesEntity(kind domain.ResultKind) string {
	switch kind {
	case domain.ResultKindTrack:
		return "song"
	case domain.ResultKindAlbum:
		return "album"
	case domain.ResultKindArtist:
		return "musicArtist"
	default:
		return "song"
	}
}

func mapITunesResult(item itunesItem, kind domain.ResultKind) domain.SearchResult {
	artworkURL := upscaleArtwork(item.ArtworkURL100, 600)

	extras := make(map[string]any)
	if item.TrackTimeMillis > 0 {
		extras["duration"] = item.TrackTimeMillis / 1000
	}

	var title, subtitle string
	switch kind {
	case domain.ResultKindTrack:
		title = item.TrackName
		subtitle = item.ArtistName
		extras["album"] = item.CollectionName
	case domain.ResultKindAlbum:
		title = item.CollectionName
		subtitle = item.ArtistName
	case domain.ResultKindArtist:
		title = item.ArtistName
	}

	return domain.SearchResult{
		Kind:       kind,
		Title:      title,
		Subtitle:   subtitle,
		ImageURL:   artworkURL,
		Confidence: domain.ConfidenceLow,
		Sources: []domain.SourceRef{{
			Provider:   domain.ProviderITunes,
			ExternalID: fmt.Sprintf("%d", item.TrackID),
			URL:        item.TrackViewURL,
		}},
		Extras: extras,
	}
}

func upscaleArtwork(url string, size int) string {
	return strings.Replace(url, "100x100", fmt.Sprintf("%dx%d", size, size), 1)
}

type itunesResponse struct {
	Results []itunesItem `json:"results"`
}

type itunesItem struct {
	TrackID         int64  `json:"trackId"`
	TrackName       string `json:"trackName"`
	ArtistName      string `json:"artistName"`
	CollectionName  string `json:"collectionName"`
	TrackViewURL    string `json:"trackViewUrl"`
	ArtworkURL100   string `json:"artworkUrl100"`
	TrackTimeMillis int64  `json:"trackTimeMillis"`
}
