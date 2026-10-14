package genius

import (
	"altune/go-api/internal/discovery/adapters/providers/providerhttp"
	"altune/go-api/internal/discovery/domain"
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

const maxHintSearches = 3

type geniusHit struct {
	Result struct {
		SongArtImageURL string `json:"song_art_image_url"`
		HeaderImageURL  string `json:"header_image_url"`
		PrimaryArtist   struct {
			Name     string `json:"name"`
			ImageURL string `json:"image_url"`
		} `json:"primary_artist"`
	} `json:"result"`
}

type GeniusArtworkResolver struct {
	client      *http.Client
	accessToken string
}

func NewGeniusArtworkResolver(client *http.Client, accessToken string) *GeniusArtworkResolver {
	return &GeniusArtworkResolver{client: client, accessToken: accessToken}
}

func (r *GeniusArtworkResolver) Resolve(ctx context.Context, kind domain.ResultKind, title, subtitle, mbid string) (string, error) {
	return r.ResolveWithHints(ctx, kind, title, subtitle, nil)
}

func (r *GeniusArtworkResolver) ResolveWithHints(ctx context.Context, kind domain.ResultKind, title, subtitle string, trackHints []string) (string, error) {
	if kind == domain.ResultKindArtist {
		return r.resolveArtistImage(ctx, title, trackHints)
	}
	if subtitle != "" {
		return r.resolveSongImage(ctx, title, subtitle)
	}
	return "", nil
}

func (r *GeniusArtworkResolver) resolveSongImage(ctx context.Context, title, artist string) (string, error) {
	q := fmt.Sprintf("%s %s", artist, title)
	hits, err := r.searchGenius(ctx, q)
	if err != nil {
		return providerhttp.ArtworkFailure(r.ArtworkSource(), err)
	}

	for _, hit := range hits {
		img := hit.Result.SongArtImageURL
		if img == "" {
			img = hit.Result.HeaderImageURL
		}
		if img != "" && !strings.Contains(img, "default") && !strings.Contains(img, "no_image") {
			return img, nil
		}
	}
	return "", nil
}

func (r *GeniusArtworkResolver) resolveArtistImage(ctx context.Context, artistName string, trackHints []string) (string, error) {
	artistName = strings.TrimSpace(artistName)
	queries := []string{artistName, artistName + " songs"}
	for i, hint := range trackHints {
		if i >= maxHintSearches {
			break
		}
		queries = append(queries, artistName+" "+hint)
	}

	for _, q := range queries {
		hits, err := r.searchGenius(ctx, q)
		if err != nil {
			return providerhttp.ArtworkFailure(r.ArtworkSource(), err)
		}
		img := findArtistImageInHits(hits, artistName)
		if img != "" {
			return img, nil
		}
	}
	return "", nil
}

func (r *GeniusArtworkResolver) searchGenius(ctx context.Context, query string) ([]geniusHit, error) {
	u := fmt.Sprintf("https://api.genius.com/search?q=%s", url.QueryEscape(query))
	var body struct {
		Response struct {
			Hits []geniusHit `json:"hits"`
		} `json:"response"`
	}
	if err := providerhttp.GetJSON(ctx, r.client, u, &body, providerhttp.WithHeader("Authorization", "Bearer "+r.accessToken)); err != nil {
		return nil, err
	}

	return body.Response.Hits, nil
}

func findArtistImageInHits(hits []geniusHit, artistName string) string {
	for _, hit := range hits {
		artist := hit.Result.PrimaryArtist
		name := strings.TrimSpace(artist.Name)
		if strings.EqualFold(name, artistName) {
			img := artist.ImageURL
			if img != "" && !strings.Contains(img, "default") && !strings.Contains(img, "no_image") {
				return img
			}
		}
	}
	return ""
}

func (*GeniusArtworkResolver) ArtworkSource() domain.ProviderKey { return domain.ProviderKeyGenius }
