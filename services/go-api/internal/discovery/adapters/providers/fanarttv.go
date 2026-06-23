package providers

import (
	"context"
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
	var data map[string]any
	if err := getJSON(ctx, r.client, url, &data); err != nil {
		return "", nil
	}

	if kind == domain.ResultKindArtist {
		if img := bestFanartImage(data, "artistthumb"); img != "" {
			return img, nil
		}
		if img := bestFanartImage(data, "artistbackground"); img != "" {
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
	return bestFanartImage(entry, "albumcover")
}

// bestFanartImage picks the best image of a Fanart.tv type instead of blindly
// taking the first: it prefers community-favored art (highest `likes`) and
// English/neutral `lang`, which avoids an arbitrary or wrong-locale first entry.
// Each image object is {id, url, lang, likes(string)}.
func bestFanartImage(data map[string]any, key string) string {
	arr, ok := data[key].([]any)
	if !ok || len(arr) == 0 {
		return ""
	}
	bestURL := ""
	bestScore := int64(-1)
	for _, it := range arr {
		m, ok := it.(map[string]any)
		if !ok {
			continue
		}
		u, _ := m["url"].(string)
		if u == "" {
			continue
		}
		var score int64
		if likes, ok := m["likes"].(string); ok {
			score = parseListeners(likes) // reuse the digit parser
		}
		if lang, _ := m["lang"].(string); lang == "en" || lang == "" {
			score += 1_000_000 // language preference dominates the likes tiebreak
		}
		if score > bestScore {
			bestScore = score
			bestURL = u
		}
	}
	return bestURL
}
