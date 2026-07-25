package providers

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/discovery/ports"
)

const spotifyOEmbedUserAgent = "altune/1.0 (+https://github.com/aleburrascano/altune)"

type SpotifyArtworkResolver struct {
	client *http.Client
}

func NewSpotifyArtworkResolver(client *http.Client) *SpotifyArtworkResolver {
	return &SpotifyArtworkResolver{client: client}
}

var (
	_ ports.ArtworkResolver         = (*SpotifyArtworkResolver)(nil)
	_ ports.IdentityArtworkResolver = (*SpotifyArtworkResolver)(nil)
	_ ports.SourcedArtworkResolver  = (*SpotifyArtworkResolver)(nil)
)

func (*SpotifyArtworkResolver) ArtworkSource() string { return "spotify" }

func (*SpotifyArtworkResolver) Resolve(context.Context, domain.ResultKind, string, string, string) (string, error) {
	return "", nil
}

func (a *SpotifyArtworkResolver) ResolveByIdentity(ctx context.Context, kind domain.ResultKind, id ports.ArtworkIdentity) (string, error) {
	spotifyID := id.ExternalIDs["spotify"]
	seg := spotifyURLSegment(kind)
	if spotifyID == "" || seg == "" {
		return "", nil
	}

	u := fmt.Sprintf("https://open.spotify.com/oembed?url=https://open.spotify.com/%s/%s", seg, spotifyID)
	var body struct {
		ThumbnailURL string `json:"thumbnail_url"`
	}
	if err := getJSON(ctx, a.client, u, &body, withHeader("User-Agent", spotifyOEmbedUserAgent)); err != nil {
		return "", nil
	}
	return upgradeSpotifyImageSize(body.ThumbnailURL), nil
}

func spotifyURLSegment(kind domain.ResultKind) string {
	switch kind {
	case domain.ResultKindArtist:
		return "artist"
	case domain.ResultKindAlbum:
		return "album"
	case domain.ResultKindTrack:
		return "track"
	default:
		return ""
	}
}

func upgradeSpotifyImageSize(url string) string {
	return strings.Replace(url, "ab67616100005174", "ab6761610000e5eb", 1)
}
