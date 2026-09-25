package providers

import (
	"altune/go-api/internal/discovery/domain"
	"context"
	"fmt"
	"net/url"
)

func looksLikeMBID(s string) bool {
	if len(s) != 36 {
		return false
	}
	for i, c := range s {
		if i == 8 || i == 13 || i == 18 || i == 23 {
			if c != '-' {
				return false
			}
			continue
		}
		isHex := (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')
		if !isHex {
			return false
		}
	}
	return true
}

func (a *LastFmAdapter) GetArtistTopTracks(ctx context.Context, _ domain.ProviderName, artistRef string) ([]domain.SearchResult, error) {
	idParam := "artist=" + url.QueryEscape(artistRef)
	if looksLikeMBID(artistRef) {
		idParam = "mbid=" + artistRef
	}
	u := fmt.Sprintf("https://ws.audioscrobbler.com/2.0/?method=artist.gettoptracks&%s&api_key=%s&format=json&limit=10&autocorrect=1",
		idParam, a.apiKey)

	var body struct {
		TopTracks struct {
			Track []struct {
				Name      string `json:"name"`
				PlayCount string `json:"playcount"`
				Listeners string `json:"listeners"`
				URL       string `json:"url"`
				Artist    struct {
					Name string `json:"name"`
				} `json:"artist"`
				Image []lastfmImage `json:"image"`
			} `json:"track"`
		} `json:"toptracks"`
	}
	if err := getJSON(ctx, a.client, u, &body); err != nil {
		return nil, err
	}

	results := make([]domain.SearchResult, 0, len(body.TopTracks.Track))
	for _, t := range body.TopTracks.Track {
		extras := make(map[string]any)
		if t.PlayCount != "" {
			extras[domain.ExtraPlaycount] = parseListeners(t.PlayCount)
		}
		if t.Listeners != "" {
			extras[domain.ExtraListeners] = parseListeners(t.Listeners)
		}
		results = append(results, domain.NewProviderResult(domain.ResultKindTrack, t.Name, t.Artist.Name, lastfmExtraLargeImage(t.Image),
			domain.SourceRef{Provider: domain.ProviderLastFM, ExternalID: lastfmExternalID(t.URL), URL: t.URL},
			extras))
	}
	return results, nil
}

const lastfmAlbumsLimit = 50

func (a *LastFmAdapter) GetArtistAlbums(ctx context.Context, _ domain.ProviderName, artistName string) ([]domain.SearchResult, error) {
	u := fmt.Sprintf("https://ws.audioscrobbler.com/2.0/?method=artist.gettopalbums&artist=%s&api_key=%s&format=json&limit=%d",
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
	if err := getJSON(ctx, a.client, u, &body); err != nil {
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
