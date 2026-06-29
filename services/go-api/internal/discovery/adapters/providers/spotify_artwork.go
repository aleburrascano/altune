package providers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/discovery/ports"
)

// spotifyOEmbedUserAgent identifies altune to Spotify's public oEmbed endpoint.
const spotifyOEmbedUserAgent = "altune/1.0 (+https://github.com/aleburrascano/altune)"

// SpotifyArtworkResolver resolves an entity's image from Spotify by its proven
// Spotify id (bridged from MusicBrainz url-relations), via the PUBLIC oEmbed
// endpoint — no API key, no login, no token. Identity-only: it never name-searches,
// so a same-name artist ("Che") can never inherit another's face. Spotify's
// artist-image coverage is near-universal, making this the broadest identity-keyed
// image source.
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

// Resolve is a deliberate no-op: Spotify artwork is fetched only by a proven id
// (oEmbed needs a Spotify URL), never by name. The chain skips identity-only
// resolvers on the name path; this just satisfies the ArtworkResolver interface.
func (*SpotifyArtworkResolver) Resolve(context.Context, domain.ResultKind, string, string, string) (string, error) {
	return "", nil
}

// ResolveByIdentity fetches the entity's Spotify image via oEmbed, keyed by the
// bridged Spotify id. A clean "" (not an error) when no Spotify id is known or the
// kind is unsupported — the chain falls past it.
func (a *SpotifyArtworkResolver) ResolveByIdentity(ctx context.Context, kind domain.ResultKind, id ports.ArtworkIdentity) (string, error) {
	spotifyID := id.ExternalIDs["spotify"]
	seg := spotifyURLSegment(kind)
	if spotifyID == "" || seg == "" {
		return "", nil
	}

	u := fmt.Sprintf("https://open.spotify.com/oembed?url=https://open.spotify.com/%s/%s", seg, spotifyID)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", spotifyOEmbedUserAgent)

	resp, err := a.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", nil
	}

	var body struct {
		ThumbnailURL string `json:"thumbnail_url"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return "", nil
	}
	return upgradeSpotifyImageSize(body.ThumbnailURL), nil
}

// spotifyURLSegment maps a result kind to the Spotify URL path segment, or "" when
// Spotify has no oEmbed surface for it.
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

// upgradeSpotifyImageSize rewrites an oEmbed artist thumbnail (320px) to its 640px
// CDN variant when the known size token is present; otherwise returns the URL
// unchanged (best-effort — a format change degrades to 320px, never to broken).
func upgradeSpotifyImageSize(url string) string {
	return strings.Replace(url, "ab67616100005174", "ab6761610000e5eb", 1)
}
