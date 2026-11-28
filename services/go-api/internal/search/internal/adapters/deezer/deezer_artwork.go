package deezer

import (
	"altune/go-api/internal/search/internal/adapters/providers"
	"altune/go-api/internal/search/internal/domain"
	"context"
	"fmt"
	"net/url"
	"strings"
)

func (a *DeezerAdapter) Resolve(ctx context.Context, kind domain.ResultKind, title, subtitle, mbid string) (string, error) {
	query := title
	if subtitle != "" {
		query = subtitle + " " + title
	}
	endpoint := deezerSearchEndpoint(kind)
	if endpoint == "" {
		endpoint = "track"
	}

	u := fmt.Sprintf(deezerAPIBaseURL+"/search/%s?q=%s&limit=1", endpoint, url.QueryEscape(query))
	var body deezerSearchResponse
	if err := a.getJSON(ctx, u, &body); err != nil {
		return providers.ArtworkFailure(domain.ProviderKeyDeezer, err)
	}
	for _, item := range body.Data {
		var img string
		switch {
		case item.Album != nil && item.Album.CoverXL != "":
			img = item.Album.CoverXL
		case item.Album != nil && item.Album.CoverBig != "":
			img = item.Album.CoverBig
		case item.CoverXL != "":
			img = item.CoverXL
		case item.CoverBig != "":
			img = item.CoverBig
		case item.PictureXL != "":
			img = item.PictureXL
		case item.PictureBig != "":
			img = item.PictureBig
		}
		if img != "" && !isDeezerPlaceholder(img) {
			return img, nil
		}
	}
	return "", nil
}

func isDeezerPlaceholder(u string) bool {
	return strings.Contains(u, "/images/artist//") || strings.Contains(u, "d41d8cd98f00b204e9800998ecf8427e")
}

func (*DeezerAdapter) ArtworkSource() domain.ProviderKey { return domain.ProviderKeyDeezer }
