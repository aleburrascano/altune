package providers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"

	"altune/go-api/internal/discovery/domain"
)

type LastFmAdapter struct {
	client *http.Client
	apiKey string
}

func NewLastFmAdapter(client *http.Client, apiKey string) *LastFmAdapter {
	return &LastFmAdapter{client: client, apiKey: apiKey}
}

func (a *LastFmAdapter) Name() domain.ProviderName { return domain.ProviderLastFM }

func (a *LastFmAdapter) SupportedKinds() map[domain.ResultKind]bool {
	return map[domain.ResultKind]bool{
		domain.ResultKindTrack:  true,
		domain.ResultKindAlbum:  true,
		domain.ResultKindArtist: true,
	}
}

func (a *LastFmAdapter) Search(ctx context.Context, query string, kinds map[domain.ResultKind]bool) ([]domain.SearchResult, error) {
	var results []domain.SearchResult

	for kind := range kinds {
		if !a.SupportedKinds()[kind] {
			continue
		}

		method := lastfmMethod(kind)
		u := fmt.Sprintf("https://ws.audioscrobbler.com/2.0/?method=%s&%s=%s&api_key=%s&format=json&limit=10",
			method, lastfmQueryParam(kind), url.QueryEscape(query), a.apiKey)

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

		var raw json.RawMessage
		if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
			continue
		}

		parsed := parseLastFmResponse(raw, kind)
		results = append(results, parsed...)
	}

	return results, nil
}

func lastfmMethod(kind domain.ResultKind) string {
	switch kind {
	case domain.ResultKindTrack:
		return "track.search"
	case domain.ResultKindAlbum:
		return "album.search"
	case domain.ResultKindArtist:
		return "artist.search"
	default:
		return "track.search"
	}
}

func lastfmQueryParam(kind domain.ResultKind) string {
	switch kind {
	case domain.ResultKindTrack:
		return "track"
	case domain.ResultKindAlbum:
		return "album"
	case domain.ResultKindArtist:
		return "artist"
	default:
		return "track"
	}
}

func parseLastFmResponse(raw json.RawMessage, kind domain.ResultKind) []domain.SearchResult {
	var results []domain.SearchResult

	switch kind {
	case domain.ResultKindTrack:
		var resp struct {
			Results struct {
				TrackMatches struct {
					Track []struct {
						Name   string `json:"name"`
						Artist string `json:"artist"`
						URL    string `json:"url"`
						Image  []struct {
							Text string `json:"#text"`
							Size string `json:"size"`
						} `json:"image"`
					} `json:"track"`
				} `json:"trackmatches"`
			} `json:"results"`
		}
		if json.Unmarshal(raw, &resp) == nil {
			for _, t := range resp.Results.TrackMatches.Track {
				imageURL := ""
				for _, img := range t.Image {
					if img.Size == "extralarge" {
						imageURL = img.Text
					}
				}
				results = append(results, domain.SearchResult{
					Kind:       domain.ResultKindTrack,
					Title:      t.Name,
					Subtitle:   t.Artist,
					ImageURL:   imageURL,
					Confidence: domain.ConfidenceLow,
					Sources: []domain.SourceRef{{
						Provider: domain.ProviderLastFM,
						URL:      t.URL,
					}},
					Extras: make(map[string]any),
				})
			}
		}
	case domain.ResultKindArtist:
		var resp struct {
			Results struct {
				ArtistMatches struct {
					Artist []struct {
						Name  string `json:"name"`
						URL   string `json:"url"`
						Image []struct {
							Text string `json:"#text"`
							Size string `json:"size"`
						} `json:"image"`
					} `json:"artist"`
				} `json:"artistmatches"`
			} `json:"results"`
		}
		if json.Unmarshal(raw, &resp) == nil {
			for _, a := range resp.Results.ArtistMatches.Artist {
				imageURL := ""
				for _, img := range a.Image {
					if img.Size == "extralarge" {
						imageURL = img.Text
					}
				}
				results = append(results, domain.SearchResult{
					Kind:       domain.ResultKindArtist,
					Title:      a.Name,
					ImageURL:   imageURL,
					Confidence: domain.ConfidenceLow,
					Sources: []domain.SourceRef{{
						Provider: domain.ProviderLastFM,
						URL:      a.URL,
					}},
					Extras: make(map[string]any),
				})
			}
		}
	}

	return results
}
