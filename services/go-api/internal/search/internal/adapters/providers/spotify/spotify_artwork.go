package spotify

import (
	"altune/go-api/internal/search/domain"
	"altune/go-api/internal/search/internal/adapters/providers/providerrequests"
	"altune/go-api/internal/search/internal/core/ports"
	"context"
	"fmt"
	"net/http"
	"strings"
)

const spotifyOEmbedUserAgent = "altune/1.0 (+https://github.com/aleburrascano/altune)"

type SpotifyArtworkLookup struct {
	client *http.Client
}

func NewSpotifyArtworkLookup(client *http.Client) *SpotifyArtworkLookup {
	return &SpotifyArtworkLookup{client: client}
}

var (
	_ ports.ArtworkLookup         = (*SpotifyArtworkLookup)(nil)
	_ ports.IdentityArtworkLookup = (*SpotifyArtworkLookup)(nil)
	_ ports.SourcedArtworkLookup  = (*SpotifyArtworkLookup)(nil)
)

func (*SpotifyArtworkLookup) ArtworkSource() domain.ProviderKey { return domain.ProviderKeySpotify }

func (*SpotifyArtworkLookup) Resolve(context.Context, domain.ResultKind, string, string, string) (string, error) {
	return "", nil
}

func (a *SpotifyArtworkLookup) ResolveByIdentity(ctx context.Context, kind domain.ResultKind, id ports.ArtworkIdentity) (string, error) {
	spotifyID := id.ExternalID(domain.ProviderKeySpotify)
	seg := spotifyURLSegment(kind)
	if spotifyID == "" || seg == "" {
		return "", nil
	}

	u := fmt.Sprintf("https://open.spotify.com/oembed?url=https://open.spotify.com/%s/%s", seg, spotifyID)
	var body struct {
		ThumbnailURL string `json:"thumbnail_url"`
	}
	if err := providerrequests.GetJSON(ctx, a.client, u, &body, providerrequests.WithHeader("User-Agent", spotifyOEmbedUserAgent)); err != nil {
		return providerrequests.ArtworkFailure(domain.ProviderKeySpotify, err)
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
