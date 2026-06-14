package providers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"altune/go-api/internal/discovery/domain"
)

type FanartTvArtworkResolver struct {
	client *http.Client
	apiKey string
}

func NewFanartTvArtworkResolver(client *http.Client, apiKey string) *FanartTvArtworkResolver {
	return &FanartTvArtworkResolver{client: client, apiKey: apiKey}
}

func (r *FanartTvArtworkResolver) Resolve(ctx context.Context, kind domain.ResultKind, title, subtitle string, mbid string) (string, error) {
	if mbid == "" {
		return "", nil
	}

	url := fmt.Sprintf("https://webservice.fanart.tv/v3/music/%s?api_key=%s", mbid, r.apiKey)
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return "", nil
	}

	resp, err := r.client.Do(req)
	if err != nil {
		return "", nil
	}
	defer resp.Body.Close()

	if resp.StatusCode == 404 {
		return "", nil
	}
	if resp.StatusCode != 200 {
		return "", nil
	}

	var data map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return "", nil
	}

	if kind == domain.ResultKindArtist {
		if img := firstImageURL(data, "artistthumb"); img != "" {
			return img, nil
		}
		if img := firstImageURL(data, "artistbackground"); img != "" {
			return img, nil
		}
	}

	if img := firstImageURL(data, "albumcover"); img != "" {
		return img, nil
	}

	return "", nil
}

func firstImageURL(data map[string]any, key string) string {
	items, ok := data[key]
	if !ok {
		return ""
	}
	arr, ok := items.([]any)
	if !ok || len(arr) == 0 {
		return ""
	}
	first, ok := arr[0].(map[string]any)
	if !ok {
		return ""
	}
	url, ok := first["url"].(string)
	if !ok {
		return ""
	}
	return url
}
