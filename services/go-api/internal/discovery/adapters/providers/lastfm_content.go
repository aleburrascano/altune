package providers

import (
	"altune/go-api/internal/discovery/adapters/providers/providerhttp"
	"altune/go-api/internal/discovery/domain"
	"context"
	"fmt"
	"net/url"
)

const lastfmAlbumsLimit = 50

func (a *LastFmAdapter) GetArtistAlbums(ctx context.Context, _ domain.ProviderName, artistName string) ([]domain.SearchResult, error) {
	u := fmt.Sprintf(lastfmAPIBaseURL+"?method=artist.gettopalbums&artist=%s&api_key=%s&format=json&limit=%d",
		url.QueryEscape(artistName), a.apiKey, lastfmAlbumsLimit)

	var body struct {
		TopAlbums struct {
			Album []struct {
				Name      string `json:"name"`
				PlayCount int    `json:"playcount"`
				MBID      string `json:"mbid"`
				URL       string `json:"url"`
				Artist    struct {
					Name string `json:"name"`
				} `json:"artist"`
				Image []lastfmImage `json:"image"`
			} `json:"album"`
		} `json:"topalbums"`
	}
	if err := providerhttp.GetJSON(ctx, a.client, u, &body); err != nil {
		return nil, err
	}

	results := make([]domain.SearchResult, 0, len(body.TopAlbums.Album))
	for _, al := range body.TopAlbums.Album {
		if al.Name == "(null)" || al.Name == "" {
			continue
		}
		extras := make(map[string]any)
		if al.PlayCount > 0 {
			extras[domain.ExtraPlaycount] = int64(al.PlayCount)
		}
		r := domain.NewProviderResult(domain.ResultKindAlbum, al.Name, al.Artist.Name, lastfmExtraLargeImage(al.Image),
			domain.SourceRef{Provider: domain.ProviderLastFM, ExternalID: lastfmExternalID(al.URL), URL: al.URL},
			extras)
		r.MBID = al.MBID
		results = append(results, r)
	}
	return results, nil
}
