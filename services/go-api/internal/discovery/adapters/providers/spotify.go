package providers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"altune/go-api/internal/discovery/domain"
)

// SpotifyAdapter searches Spotify's internal partner GraphQL API
// (api-partner.spotify.com/pathfinder) using the same anonymous-visitor
// bootstrap Spotify's own web player uses — no account login, no
// developer-registered app (see spotify_token.go, spotify_totp.go).
//
// AIDEV-DECISION: undocumented and against Spotify's ToS — accepted for
// self-hosted personal/family use, the same risk posture as the SoundCloud/
// Amazon Music/Apple Music adapters. Unlike those three, this one rides a
// genuinely hardened, actively-defended internal API (TOTP-gated access
// token, a separate client-integrity token, and a persisted-query hash
// Spotify can rotate without notice) — confirmed live 2026-07-22 to be a
// materially harder and more fragile integration than the other three.
// Accepted anyway per explicit product decision: redundancy across the
// three other providers already covers what this adds, so a breakage here
// degrades gracefully rather than losing coverage.
type SpotifyAdapter struct {
	client        *http.Client
	resolver      *spotifyTokenResolver
	pathfinderURL string // GraphQL endpoint shared by search + artist content; overridable in tests
}

const (
	spotifyUserAgent         = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0.0.0 Safari/537.36"
	spotifyWebPlayerClientID = "d8a5ed958d274c2e8ee717e6a4b0971d"
	spotifyClientVersion     = "1.2.95.397.gd60c55ec"
	spotifySearchTimeout     = 5 * time.Second
	spotifyPathfinderURL     = "https://api-partner.spotify.com/pathfinder/v2/query"
	// spotifySearchQueryHash is the persisted-query sha256 for the
	// "searchDesktop" pathfinder operation, captured live 2026-07-22.
	//
	// AIDEV-WARNING: Spotify can rotate this hash without notice — a stale
	// hash fails with a "PersistedQueryNotFound"-shaped GraphQL error, not an
	// auth error, so isAuthStatus's retry-on-401/403 will NOT catch or fix
	// it. Re-capture the current hash from a live open.spotify.com/search
	// page's network traffic (operationName "searchDesktop") if search
	// starts failing with no auth-related status code.
	spotifySearchQueryHash = "db61238974d27839a136c9dc02bfdbe3fab7635f21cf85976ebff9a1ee281345"
)

func NewSpotifyAdapter(client *http.Client) *SpotifyAdapter {
	return &SpotifyAdapter{
		client:        client,
		resolver:      newSpotifyTokenResolver(client),
		pathfinderURL: spotifyPathfinderURL,
	}
}

func (a *SpotifyAdapter) Name() domain.ProviderName { return domain.ProviderSpotify }

func (a *SpotifyAdapter) SupportedKinds() map[domain.ResultKind]bool {
	return map[domain.ResultKind]bool{
		domain.ResultKindTrack:  true,
		domain.ResultKindAlbum:  true,
		domain.ResultKindArtist: true,
	}
}

func (a *SpotifyAdapter) SearchTimeout() time.Duration { return spotifySearchTimeout }

func (a *SpotifyAdapter) Search(ctx context.Context, query string, kinds map[domain.ResultKind]bool) ([]domain.SearchResult, error) {
	sess, err := a.resolver.get(ctx)
	if err != nil {
		return nil, fmt.Errorf("resolve spotify session: %w", err)
	}

	results, status, err := a.doSearch(ctx, sess, query)
	if err != nil && isAuthStatus(status) {
		a.resolver.invalidate()
		sess, err = a.resolver.get(ctx)
		if err != nil {
			return nil, fmt.Errorf("re-resolve spotify session: %w", err)
		}
		results, _, err = a.doSearch(ctx, sess, query)
	}
	if err != nil {
		return nil, err
	}

	out := make([]domain.SearchResult, 0, len(results))
	for _, r := range results {
		if kinds[r.Kind] {
			out = append(out, r)
		}
	}
	return out, nil
}

func (a *SpotifyAdapter) doSearch(ctx context.Context, sess *spotifySession, query string) ([]domain.SearchResult, int, error) {
	payload, err := json.Marshal(spotifySearchRequest{
		Variables: spotifySearchVariables{
			SearchTerm:                     query,
			Limit:                          10,
			NumberOfTopResults:             5,
			IncludeAudiobooks:              false,
			IncludeArtistHasConcertsField:  false,
			IncludePreReleases:             false,
			IncludeAlbumPreReleases:        false,
			IncludeAuthors:                 false,
			IncludeEpisodeContentRatingsV2: false,
			SectionFilters:                 []string{"GENERIC"},
		},
		OperationName: "searchDesktop",
		Extensions: spotifyExtensions{
			PersistedQuery: spotifyPersistedQuery{Version: 1, Sha256Hash: spotifySearchQueryHash},
		},
	})
	if err != nil {
		return nil, 0, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, a.pathfinderURL, bytes.NewReader(payload))
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Content-Type", "application/json;charset=UTF-8")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+sess.accessToken)
	req.Header.Set("client-token", sess.clientToken)
	req.Header.Set("app-platform", "WebPlayer")
	req.Header.Set("User-Agent", spotifyUserAgent)

	resp, err := a.client.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, resp.StatusCode, fmt.Errorf("http status %d", resp.StatusCode)
	}

	var body spotifySearchResponse
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, resp.StatusCode, fmt.Errorf("decode search response: %w", err)
	}

	// Pathfinder signals failure at the GraphQL layer with a top-level "errors"
	// array while still returning HTTP 200 — a stale persisted-query hash
	// ("PersistedQueryNotFound") or a rejected integrity token both land here.
	// Without this check the empty data.searchV2 decodes cleanly and the search
	// silently returns zero results while reporting success, so Spotify vanishes
	// from every query and looks healthy on the provider board (see the
	// persisted-query AIDEV-WARNING above). Surface it as a real error instead.
	if len(body.Errors) > 0 {
		return nil, resp.StatusCode, fmt.Errorf("spotify graphql error: %s", body.Errors[0].Message)
	}

	sv2 := body.Data.SearchV2
	results := make([]domain.SearchResult, 0, len(sv2.TracksV2.Items)+len(sv2.AlbumsV2.Items)+len(sv2.Artists.Items))
	for _, t := range sv2.TracksV2.Items {
		if r, ok := mapSpotifyTrack(t.Item.Data); ok {
			results = append(results, r)
		}
	}
	for _, al := range sv2.AlbumsV2.Items {
		if r, ok := mapSpotifyAlbum(al.Data); ok {
			results = append(results, r)
		}
	}
	for _, ar := range sv2.Artists.Items {
		if r, ok := mapSpotifyArtist(ar.Data); ok {
			results = append(results, r)
		}
	}
	return results, resp.StatusCode, nil
}

// --- request/response shapes --------------------------------------------
//
// The "searchDesktop" persisted-query response nests each section
// inconsistently: tracksV2 wraps its entity at items[].item.data (the extra
// "item" hop) while albumsV2/artists wrap at items[].data directly — matches
// the live-captured traffic exactly, not a guess.

type spotifySearchRequest struct {
	Variables     spotifySearchVariables `json:"variables"`
	OperationName string                 `json:"operationName"`
	Extensions    spotifyExtensions      `json:"extensions"`
}

type spotifySearchVariables struct {
	SearchTerm                     string   `json:"searchTerm"`
	Offset                         int      `json:"offset"`
	Limit                          int      `json:"limit"`
	NumberOfTopResults             int      `json:"numberOfTopResults"`
	IncludeAudiobooks              bool     `json:"includeAudiobooks"`
	IncludeArtistHasConcertsField  bool     `json:"includeArtistHasConcertsField"`
	IncludePreReleases             bool     `json:"includePreReleases"`
	IncludeAlbumPreReleases        bool     `json:"includeAlbumPreReleases"`
	IncludeAuthors                 bool     `json:"includeAuthors"`
	IncludeEpisodeContentRatingsV2 bool     `json:"includeEpisodeContentRatingsV2"`
	SectionFilters                 []string `json:"sectionFilters"`
}

type spotifyExtensions struct {
	PersistedQuery spotifyPersistedQuery `json:"persistedQuery"`
}

type spotifyPersistedQuery struct {
	Version    int    `json:"version"`
	Sha256Hash string `json:"sha256Hash"`
}

type spotifyImage struct {
	URL    string `json:"url"`
	Width  int    `json:"width"`
	Height int    `json:"height"`
}

type spotifySearchResponse struct {
	Data struct {
		SearchV2 struct {
			TracksV2 struct {
				Items []struct {
					Item struct {
						Data spotifyTrackData `json:"data"`
					} `json:"item"`
				} `json:"items"`
			} `json:"tracksV2"`
			AlbumsV2 struct {
				Items []struct {
					Data spotifyAlbumData `json:"data"`
				} `json:"items"`
			} `json:"albumsV2"`
			Artists struct {
				Items []struct {
					Data spotifyArtistData `json:"data"`
				} `json:"items"`
			} `json:"artists"`
		} `json:"searchV2"`
	} `json:"data"`
	// Errors carries pathfinder's GraphQL-layer failures (returned alongside HTTP
	// 200). A stale persisted-query hash reports "PersistedQueryNotFound" here.
	Errors []struct {
		Message string `json:"message"`
	} `json:"errors"`
}

type spotifyTrackData struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	URI      string `json:"uri"`
	Duration struct {
		TotalMilliseconds int64 `json:"totalMilliseconds"`
	} `json:"duration"`
	ContentRating struct {
		Label string `json:"label"`
	} `json:"contentRating"`
	AlbumOfTrack struct {
		ID       string `json:"id"`
		Name     string `json:"name"`
		URI      string `json:"uri"`
		CoverArt struct {
			Sources []spotifyImage `json:"sources"`
		} `json:"coverArt"`
	} `json:"albumOfTrack"`
	Artists struct {
		Items []struct {
			Profile struct {
				Name string `json:"name"`
			} `json:"profile"`
			URI string `json:"uri"`
		} `json:"items"`
	} `json:"artists"`
}

type spotifyAlbumData struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	URI      string `json:"uri"`
	CoverArt struct {
		Sources []spotifyImage `json:"sources"`
	} `json:"coverArt"`
}

type spotifyArtistData struct {
	ID      string `json:"id"`
	URI     string `json:"uri"`
	Profile struct {
		Name string `json:"name"`
	} `json:"profile"`
	Visuals struct {
		AvatarImage struct {
			Sources []spotifyImage `json:"sources"`
		} `json:"avatarImage"`
	} `json:"visuals"`
}

// spotifyBestImage picks the widest available image — search responses
// carry several fixed sizes, not a URL template like Apple/Amazon.
func spotifyBestImage(sources []spotifyImage) string {
	best, bestWidth := "", -1
	for _, s := range sources {
		if s.Width > bestWidth {
			bestWidth, best = s.Width, s.URL
		}
	}
	return best
}

// spotifyIDFromURI recovers the entity id from a "spotify:kind:id" URI, for
// the rare case a search hit carries no bare id field.
func spotifyIDFromURI(uri string) string {
	i := strings.LastIndex(uri, ":")
	if i < 0 {
		return uri
	}
	return uri[i+1:]
}

func mapSpotifyTrack(t spotifyTrackData) (domain.SearchResult, bool) {
	if strings.TrimSpace(t.Name) == "" {
		return domain.SearchResult{}, false
	}
	id := t.ID
	if id == "" {
		id = spotifyIDFromURI(t.URI)
	}
	artist := ""
	if len(t.Artists.Items) > 0 {
		artist = t.Artists.Items[0].Profile.Name
	}

	var extras map[string]any
	if t.ContentRating.Label == "EXPLICIT" {
		extras = map[string]any{"explicit": true}
	}

	r := domain.NewProviderResult(domain.ResultKindTrack, t.Name, artist,
		spotifyBestImage(t.AlbumOfTrack.CoverArt.Sources),
		domain.SourceRef{Provider: domain.ProviderSpotify, ExternalID: id, URL: "https://open.spotify.com/track/" + id},
		extras)
	r.Album = t.AlbumOfTrack.Name
	if t.Duration.TotalMilliseconds > 0 {
		r.Duration = int(t.Duration.TotalMilliseconds / 1000)
	}
	return r, true
}

func mapSpotifyAlbum(al spotifyAlbumData) (domain.SearchResult, bool) {
	if strings.TrimSpace(al.Name) == "" {
		return domain.SearchResult{}, false
	}
	id := al.ID
	if id == "" {
		id = spotifyIDFromURI(al.URI)
	}
	return domain.NewProviderResult(domain.ResultKindAlbum, al.Name, "",
		spotifyBestImage(al.CoverArt.Sources),
		domain.SourceRef{Provider: domain.ProviderSpotify, ExternalID: id, URL: "https://open.spotify.com/album/" + id},
		nil), true
}

func mapSpotifyArtist(ar spotifyArtistData) (domain.SearchResult, bool) {
	if strings.TrimSpace(ar.Profile.Name) == "" {
		return domain.SearchResult{}, false
	}
	id := ar.ID
	if id == "" {
		id = spotifyIDFromURI(ar.URI)
	}
	return domain.NewProviderResult(domain.ResultKindArtist, ar.Profile.Name, "",
		spotifyBestImage(ar.Visuals.AvatarImage.Sources),
		domain.SourceRef{Provider: domain.ProviderSpotify, ExternalID: id, URL: "https://open.spotify.com/artist/" + id},
		nil), true
}

func (*SpotifyAdapter) ArtworkSource() string { return "spotify" }
