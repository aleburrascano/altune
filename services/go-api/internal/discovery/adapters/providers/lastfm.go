package providers

import (
	"altune/go-api/internal/discovery/domain"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type LastFmAdapter struct {
	client *http.Client
	apiKey string
}

func NewLastFmAdapter(client *http.Client, apiKey string) *LastFmAdapter {
	return &LastFmAdapter{client: client, apiKey: apiKey}
}

func (a *LastFmAdapter) Name() domain.ProviderName { return domain.ProviderLastFM }

func (a *LastFmAdapter) SearchTimeout() time.Duration { return 4 * time.Second }

func (a *LastFmAdapter) SupportedKinds() map[domain.ResultKind]bool {
	return allSearchKinds()
}

func (a *LastFmAdapter) Search(ctx context.Context, query string, kinds map[domain.ResultKind]bool) ([]domain.SearchResult, error) {
	return searchAcrossKinds(ctx, a.Name().String(), query, kinds, a.SupportedKinds(),
		func(ctx context.Context, kind domain.ResultKind) ([]domain.SearchResult, error) {
			return a.searchKind(ctx, query, kind)
		})
}

func (a *LastFmAdapter) searchKind(ctx context.Context, query string, kind domain.ResultKind) ([]domain.SearchResult, error) {
	method := lastfmMethod(kind)
	u := fmt.Sprintf("https://ws.audioscrobbler.com/2.0/?method=%s&%s=%s&api_key=%s&format=json&limit=15",
		method, lastfmQueryParam(kind), url.QueryEscape(query), a.apiKey)

	var raw json.RawMessage
	if err := getJSON(ctx, a.client, u, &raw); err != nil {
		return nil, err
	}
	return parseLastFmResponse(raw, kind), nil
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

type lastfmImage struct {
	Text string `json:"#text"`
	Size string `json:"size"`
}

func lastfmExtraLargeImage(images []lastfmImage) string {
	imageURL := ""
	for _, img := range images {
		if img.Size == "extralarge" {
			imageURL = img.Text
		}
	}
	return imageURL
}

func parseLastFmResponse(raw json.RawMessage, kind domain.ResultKind) []domain.SearchResult {
	switch kind {
	case domain.ResultKindTrack:
		return parseLastFmTracks(raw)
	case domain.ResultKindAlbum:
		return parseLastFmAlbums(raw)
	case domain.ResultKindArtist:
		return parseLastFmArtists(raw)
	default:
		return nil
	}
}

func parseLastFmTracks(raw json.RawMessage) []domain.SearchResult {
	var resp struct {
		Results struct {
			TrackMatches struct {
				Track []struct {
					Name      string        `json:"name"`
					Artist    string        `json:"artist"`
					MBID      string        `json:"mbid"`
					URL       string        `json:"url"`
					Listeners string        `json:"listeners"`
					Image     []lastfmImage `json:"image"`
				} `json:"track"`
			} `json:"trackmatches"`
		} `json:"results"`
	}
	if json.Unmarshal(raw, &resp) != nil {
		return nil
	}
	var results []domain.SearchResult
	for _, t := range resp.Results.TrackMatches.Track {
		extras := make(map[string]any)
		if t.Listeners != "" {
			extras[domain.ExtraListeners] = t.Listeners
		}
		r := domain.NewProviderResult(domain.ResultKindTrack, t.Name, t.Artist, lastfmExtraLargeImage(t.Image),
			domain.SourceRef{Provider: domain.ProviderLastFM, ExternalID: lastfmExternalID(t.URL), URL: t.URL},
			extras)
		r.MBID = t.MBID
		results = append(results, r)
	}
	return results
}

func parseLastFmAlbums(raw json.RawMessage) []domain.SearchResult {
	var resp struct {
		Results struct {
			AlbumMatches struct {
				Album []struct {
					Name   string        `json:"name"`
					Artist string        `json:"artist"`
					MBID   string        `json:"mbid"`
					URL    string        `json:"url"`
					Image  []lastfmImage `json:"image"`
				} `json:"album"`
			} `json:"albummatches"`
		} `json:"results"`
	}
	if json.Unmarshal(raw, &resp) != nil {
		return nil
	}
	var results []domain.SearchResult
	for _, a := range resp.Results.AlbumMatches.Album {
		r := domain.NewProviderResult(domain.ResultKindAlbum, a.Name, a.Artist, lastfmExtraLargeImage(a.Image),
			domain.SourceRef{Provider: domain.ProviderLastFM, ExternalID: lastfmExternalID(a.URL), URL: a.URL},
			nil)
		results = append(results, r)
	}
	return results
}

func parseLastFmArtists(raw json.RawMessage) []domain.SearchResult {
	var resp struct {
		Results struct {
			ArtistMatches struct {
				Artist []struct {
					Name      string        `json:"name"`
					MBID      string        `json:"mbid"`
					URL       string        `json:"url"`
					Listeners string        `json:"listeners"`
					Image     []lastfmImage `json:"image"`
				} `json:"artist"`
			} `json:"artistmatches"`
		} `json:"results"`
	}
	if json.Unmarshal(raw, &resp) != nil {
		return nil
	}
	var results []domain.SearchResult
	for _, a := range resp.Results.ArtistMatches.Artist {
		extras := make(map[string]any)
		if a.Listeners != "" {
			extras[domain.ExtraListeners] = a.Listeners
		}
		r := domain.NewProviderResult(domain.ResultKindArtist, a.Name, "", lastfmExtraLargeImage(a.Image),
			domain.SourceRef{Provider: domain.ProviderLastFM, ExternalID: lastfmExternalID(a.URL), URL: a.URL},
			extras)
		r.MBID = a.MBID
		results = append(results, r)
	}
	return results
}

func parseListeners(s string) int64 {
	var n int64
	for _, c := range s {
		if c >= '0' && c <= '9' {
			n = n*10 + int64(c-'0')
		}
	}
	return n
}

func lastfmExternalID(u string) string {
	const prefix = "/music/"
	idx := strings.Index(u, prefix)
	if idx < 0 {
		if u != "" {
			return u
		}
		return ""
	}
	id := u[idx+len(prefix):]
	id = strings.TrimRight(id, "/")
	if id == "" {
		return u
	}
	return id
}
