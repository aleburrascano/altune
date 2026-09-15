package providers

import (
	"altune/go-api/internal/discovery/domain"
	"context"
	"fmt"
	"net/url"
	"strings"
)

func upscaleArtwork(rawURL string, size int) string {
	return strings.Replace(rawURL, "100x100", fmt.Sprintf("%dx%d", size, size), 1)
}

const iTunesListArtworkSize = 600

const iTunesHeroArtworkSize = 1500

func (a *ITunesAdapter) Resolve(ctx context.Context, kind domain.ResultKind, title, subtitle, mbid string) (string, error) {
	query := title
	if subtitle != "" {
		query = subtitle + " " + title
	}
	entity := itunesEntity(kind)

	u := fmt.Sprintf("https://itunes.apple.com/search?term=%s&entity=%s&country=US&limit=1", url.QueryEscape(query), entity)
	a.rateLimit(ctx)
	var body itunesResponse
	if err := getJSON(ctx, a.client, u, &body, withHeader("User-Agent", itunesUserAgent)); err != nil {
		return "", nil //nolint:nilerr // intentional graceful degradation: artwork resolution is best-effort
	}
	for _, item := range body.Results {
		art := upscaleArtwork(item.ArtworkURL100, iTunesHeroArtworkSize)
		if art != "" {
			return art, nil
		}
	}
	return "", nil
}

func (*ITunesAdapter) ArtworkSource() domain.ProviderKey { return domain.ProviderKeyITunes }
