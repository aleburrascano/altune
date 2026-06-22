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

	// Endpoint + shape verified live 2026-06-21: artist art is top-level on
	// /v3/music/{mbid}; album art lives behind the dedicated /v3/music/albums/{mbid}
	// endpoint, nested under albums[mbid].albumcover (the plain music endpoint
	// returns {} for an album mbid).
	path := "music/" + mbid
	if kind == domain.ResultKindAlbum {
		path = "music/albums/" + mbid
	}
	url := fmt.Sprintf("https://webservice.fanart.tv/v3/%s?api_key=%s", path, r.apiKey)
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return "", nil
	}

	resp, err := r.client.Do(req)
	if err != nil {
		return "", nil
	}
	defer resp.Body.Close()

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
		return "", nil
	}

	return albumCoverURL(data, mbid), nil
}

// albumCoverURL digs the album cover out of the albums endpoint's nested shape:
// {"albums": {"<mbid>": {"albumcover": [{"url": ...}]}}}. Keyed by the queried
// mbid (the endpoint returns the album under its own id).
func albumCoverURL(data map[string]any, mbid string) string {
	albums, ok := data["albums"].(map[string]any)
	if !ok {
		return ""
	}
	entry, ok := albums[mbid].(map[string]any)
	if !ok {
		return ""
	}
	return firstImageURL(entry, "albumcover")
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
