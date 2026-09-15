package providers

import (
	"altune/go-api/internal/discovery/domain"
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

	u := fmt.Sprintf("https://api.deezer.com/search/%s?q=%s&limit=1", endpoint, url.QueryEscape(query))
	var body deezerSearchResponse
	if err := a.getJSON(ctx, u, &body); err != nil {
		return "", nil //nolint:nilerr // intentional graceful degradation: artwork resolution is best-effort
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
		if img != "" && !IsDeezerPlaceholder(img) {
			return img, nil
		}
	}
	return "", nil
}

const DeezerPlaceholderImage = "https://e-cdns-images.dzcdn.net/images/artist//500x500-000000-80-0-0.jpg"

func IsDeezerPlaceholder(u string) bool {
	return strings.Contains(u, "/images/artist//") || strings.Contains(u, "d41d8cd98f00b204e9800998ecf8427e")
}

func (*DeezerAdapter) ArtworkSource() domain.ProviderKey { return domain.ProviderKeyDeezer }
