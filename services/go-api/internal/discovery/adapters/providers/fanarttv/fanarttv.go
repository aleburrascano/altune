package fanarttv

import (
	"altune/go-api/internal/discovery/adapters/providers/providerhttp"
	"altune/go-api/internal/discovery/adapters/providers/providersearch"
	"altune/go-api/internal/discovery/domain"
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

type FanartTvArtworkResolver struct {
	client *http.Client
	apiKey string
}

func NewFanartTvArtworkResolver(client *http.Client, apiKey string) *FanartTvArtworkResolver {
	return &FanartTvArtworkResolver{client: client, apiKey: apiKey}
}

func (r *FanartTvArtworkResolver) Resolve(ctx context.Context, kind domain.ResultKind, title, subtitle, mbid string) (string, error) {
	if mbid == "" {
		return "", nil
	}

	path := "music/" + url.PathEscape(mbid)
	if kind == domain.ResultKindAlbum {
		path = "music/albums/" + url.PathEscape(mbid)
	}
	u := fmt.Sprintf("https://webservice.fanart.tv/v3/%s?api_key=%s", path, r.apiKey)
	var payload fanartDoc
	if err := providerhttp.GetJSON(ctx, r.client, u, &payload); err != nil {
		return providerhttp.ArtworkFailure(domain.ProviderKeyFanart, err)
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
	score := providersearch.ParseListeners(img.Likes)
	if img.Lang == "en" || img.Lang == "" {
		score += 1_000_000
	}
	return score
}

func (*FanartTvArtworkResolver) ArtworkSource() domain.ProviderKey { return domain.ProviderKeyFanart }
