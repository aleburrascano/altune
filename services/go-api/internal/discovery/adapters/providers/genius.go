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

const maxHintSearches = 3

type GeniusArtworkResolver struct {
	client      *http.Client
	accessToken string
}

func NewGeniusArtworkResolver(client *http.Client, accessToken string) *GeniusArtworkResolver {
	return &GeniusArtworkResolver{client: client, accessToken: accessToken}
}

func (r *GeniusArtworkResolver) Resolve(ctx context.Context, kind domain.ResultKind, title, subtitle string, mbid string) (string, error) {
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
		return "", nil
	}

	for _, hit := range hits {
		result, ok := hit["result"].(map[string]any)
		if !ok {
			continue
		}
		img := stringOr(result, "song_art_image_url", "")
		if img == "" {
			img = stringOr(result, "header_image_url", "")
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
			return "", nil
		}
		img := findArtistImageInHits(hits, artistName)
		if img != "" {
			return img, nil
		}
	}
	return "", nil
}

func (r *GeniusArtworkResolver) searchGenius(ctx context.Context, query string) ([]map[string]any, error) {
	u := fmt.Sprintf("https://api.genius.com/search?q=%s", url.QueryEscape(query))
	req, err := http.NewRequestWithContext(ctx, "GET", u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+r.accessToken)

	resp, err := r.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return nil, nil
	}

	var body struct {
		Response struct {
			Hits []map[string]any `json:"hits"`
		} `json:"response"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, nil
	}

	return body.Response.Hits, nil
}

func findArtistImageInHits(hits []map[string]any, artistName string) string {
	for _, hit := range hits {
		result, ok := hit["result"].(map[string]any)
		if !ok {
			continue
		}
		artist, ok := result["primary_artist"].(map[string]any)
		if !ok {
			continue
		}
		name := strings.TrimSpace(stringOr(artist, "name", ""))
		if strings.EqualFold(name, artistName) {
			img := stringOr(artist, "image_url", "")
			if img != "" && !strings.Contains(img, "default") && !strings.Contains(img, "no_image") {
				return img
			}
		}
	}
	return ""
}

func stringOr(m map[string]any, key, fallback string) string {
	v, ok := m[key]
	if !ok {
		return fallback
	}
	s, ok := v.(string)
	if !ok {
		return fallback
	}
	return s
}
