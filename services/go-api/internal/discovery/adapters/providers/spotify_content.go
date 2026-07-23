package providers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"

	"altune/go-api/internal/discovery/domain"
)

// spotifyContentLimit caps the artist-content endpoints. Albums come newest-first
// and top-tracks by popularity, so the head is what the detail screen wants.
const spotifyContentLimit = 50

// GetArtistAlbums implements ports.ArtistContentProvider via Spotify's classic Web
// API (api.spotify.com/v1) with the same anonymous web-player bearer token the
// search path resolves — no pathfinder persisted-query hash needed. externalID is
// the Spotify artist id (bridged from MusicBrainz url-relations). Carries release
// date + cover art.
func (a *SpotifyAdapter) GetArtistAlbums(ctx context.Context, _ domain.ProviderName, externalID string) ([]domain.SearchResult, error) {
	u := fmt.Sprintf("%s/artists/%s/albums?include_groups=album,single&market=US&limit=%d",
		a.apiBase, url.PathEscape(externalID), spotifyContentLimit)
	var body spotifyArtistAlbumsResponse
	if err := a.apiGet(ctx, u, &body); err != nil {
		return nil, err
	}
	out := make([]domain.SearchResult, 0, len(body.Items))
	for _, al := range body.Items {
		if r, ok := mapSpotifyAPIAlbum(al); ok {
			out = append(out, r)
		}
	}
	return out, nil
}

// GetArtistTopTracks implements ports.ArtistContentProvider via the classic Web
// API artist top-tracks endpoint.
func (a *SpotifyAdapter) GetArtistTopTracks(ctx context.Context, _ domain.ProviderName, externalID string) ([]domain.SearchResult, error) {
	u := fmt.Sprintf("%s/artists/%s/top-tracks?market=US", a.apiBase, url.PathEscape(externalID))
	var body spotifyArtistTopTracksResponse
	if err := a.apiGet(ctx, u, &body); err != nil {
		return nil, err
	}
	out := make([]domain.SearchResult, 0, len(body.Tracks))
	for _, t := range body.Tracks {
		if r, ok := mapSpotifyAPITrack(t); ok {
			out = append(out, r)
		}
	}
	return out, nil
}

// apiGet GETs an authorized classic-API URL into out, re-resolving the session
// once on an auth failure (the rotation-tolerant shape Search uses).
func (a *SpotifyAdapter) apiGet(ctx context.Context, u string, out any) error {
	sess, err := a.resolver.get(ctx)
	if err != nil {
		return fmt.Errorf("resolve spotify session: %w", err)
	}
	status, err := a.doAPIGet(ctx, sess, u, out)
	if err != nil && isAuthStatus(status) {
		a.resolver.invalidate()
		sess, err = a.resolver.get(ctx)
		if err != nil {
			return fmt.Errorf("re-resolve spotify session: %w", err)
		}
		_, err = a.doAPIGet(ctx, sess, u, out)
	}
	return err
}

func (a *SpotifyAdapter) doAPIGet(ctx context.Context, sess *spotifySession, u string, out any) (int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return 0, err
	}
	req.Header.Set("Authorization", "Bearer "+sess.accessToken)
	req.Header.Set("client-token", sess.clientToken)
	req.Header.Set("app-platform", "WebPlayer")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", spotifyUserAgent)

	resp, err := a.client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return resp.StatusCode, fmt.Errorf("http status %d", resp.StatusCode)
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return resp.StatusCode, fmt.Errorf("decode spotify api response: %w", err)
	}
	return resp.StatusCode, nil
}

// --- classic Web API shapes ---------------------------------------------

type spotifyArtistAlbumsResponse struct {
	Items []spotifyAPIAlbum `json:"items"`
}

type spotifyAPIAlbum struct {
	ID          string         `json:"id"`
	Name        string         `json:"name"`
	AlbumType   string         `json:"album_type"`
	ReleaseDate string         `json:"release_date"`
	TotalTracks int            `json:"total_tracks"`
	Images      []spotifyImage `json:"images"`
	URI         string         `json:"uri"`
	ExternalURL struct {
		Spotify string `json:"spotify"`
	} `json:"external_urls"`
}

type spotifyArtistTopTracksResponse struct {
	Tracks []spotifyAPITrack `json:"tracks"`
}

type spotifyAPITrack struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Explicit bool   `json:"explicit"`
	Duration int64  `json:"duration_ms"`
	Album    struct {
		Name   string         `json:"name"`
		Images []spotifyImage `json:"images"`
	} `json:"album"`
	Artists []struct {
		Name string `json:"name"`
	} `json:"artists"`
	ExternalURL struct {
		Spotify string `json:"spotify"`
	} `json:"external_urls"`
}

func spotifyAPIURL(explicitURL, kind, id string) string {
	if explicitURL != "" {
		return explicitURL
	}
	return "https://open.spotify.com/" + kind + "/" + id
}

func mapSpotifyAPIAlbum(al spotifyAPIAlbum) (domain.SearchResult, bool) {
	if al.Name == "" || al.ID == "" {
		return domain.SearchResult{}, false
	}
	var extras map[string]any
	if al.AlbumType == "single" {
		extras = map[string]any{"record_type": "single"}
	}
	r := domain.NewProviderResult(domain.ResultKindAlbum, al.Name, "",
		spotifyBestImage(al.Images),
		domain.SourceRef{Provider: domain.ProviderSpotify, ExternalID: al.ID, URL: spotifyAPIURL(al.ExternalURL.Spotify, "album", al.ID)},
		extras)
	r.ReleaseDate = al.ReleaseDate
	r.TrackCount = al.TotalTracks
	return r, true
}

func mapSpotifyAPITrack(t spotifyAPITrack) (domain.SearchResult, bool) {
	if t.Name == "" || t.ID == "" {
		return domain.SearchResult{}, false
	}
	artist := ""
	if len(t.Artists) > 0 {
		artist = t.Artists[0].Name
	}
	var extras map[string]any
	if t.Explicit {
		extras = map[string]any{"explicit": true}
	}
	r := domain.NewProviderResult(domain.ResultKindTrack, t.Name, artist,
		spotifyBestImage(t.Album.Images),
		domain.SourceRef{Provider: domain.ProviderSpotify, ExternalID: t.ID, URL: spotifyAPIURL(t.ExternalURL.Spotify, "track", t.ID)},
		extras)
	r.Album = t.Album.Name
	if t.Duration > 0 {
		r.Duration = int(t.Duration / 1000)
	}
	return r, true
}
