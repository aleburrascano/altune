package fanarttv

import (
	"altune/go-api/internal/platform/outbound"
	"altune/go-api/internal/search/internal/adapters/providers"
	"altune/go-api/internal/search/internal/domain"
	"context"
	"fmt"
	"net/http"
	"net/url"
)

type fanartImage struct {
	URL   string `json:"url"`
	Likes string `json:"likes"`
	Lang  string `json:"lang"`
}

type fanartDoc struct {
	ArtistThumb      []fanartImage        `json:"artistthumb"`
	ArtistBackground []fanartImage        `json:"artistbackground"`
	AlbumCover       []fanartImage        `json:"albumcover"`
	Albums           map[string]fanartDoc `json:"albums"`
}

type FanartTvArtworkLookup struct {
	client *http.Client
	apiKey string
}

func NewFanartTvArtworkLookup(client *http.Client, apiKey string) *FanartTvArtworkLookup {
	return &FanartTvArtworkLookup{client: client, apiKey: apiKey}
}

func (r *FanartTvArtworkLookup) Resolve(ctx context.Context, kind domain.ResultKind, title, subtitle, mbid string) (string, error) {
	if mbid == "" {
		return "", nil
	}

	path := "music/" + url.PathEscape(mbid)
	if kind == domain.ResultKindAlbum {
		path = "music/albums/" + url.PathEscape(mbid)
	}
	u := fmt.Sprintf("https://webservice.fanart.tv/v3/%s?api_key=%s", path, r.apiKey)
	var payload fanartDoc
	if err := outbound.GetJSON(ctx, r.client, u, &payload); err != nil {
		return providers.ArtworkFailure(domain.ProviderKeyFanart, err)
	}

	if kind == domain.ResultKindArtist {
		if img := bestFanartImage(payload.ArtistThumb); img != "" {
			return img, nil
		}
		if img := bestFanartImage(payload.ArtistBackground); img != "" {
			return img, nil
		}
		return "", nil
	}

	return albumCoverURL(payload, mbid), nil
}

func albumCoverURL(payload fanartDoc, mbid string) string {
	return bestFanartImage(payload.Albums[mbid].AlbumCover)
}

func bestFanartImage(images []fanartImage) string {
	bestURL := ""
	bestScore := int64(-1)
	for _, img := range images {
		if img.URL == "" {
			continue
		}
		if score := fanartImageScore(img); score > bestScore {
			bestScore = score
			bestURL = img.URL
		}
	}
	return bestURL
}

func fanartImageScore(img fanartImage) int64 {
	score := providers.ParseListeners(img.Likes)
	if img.Lang == "en" || img.Lang == "" {
		score += 1_000_000
	}
	return score
}

func (*FanartTvArtworkLookup) ArtworkSource() domain.ProviderKey { return domain.ProviderKeyFanart }
