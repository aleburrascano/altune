package providers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"altune/go-api/internal/discovery/domain"
)

// Spotify artist content rides the SAME pathfinder GraphQL API the search path
// uses (api-partner.spotify.com/pathfinder) — NOT the classic Web API
// (api.spotify.com/v1). The anonymous web-player bearer token has effectively
// zero quota on the classic Web API: every call 429s "API rate limit exceeded",
// even from a cold IP, because that endpoint is the developer OAuth API, not the
// path the web player itself uses. The earlier classic-API content path
// therefore returned nothing on every artist. Pathfinder accepts the same token
// that already works for search. Confirmed live 2026-07-23.
//
// AIDEV-WARNING: like the search persisted-query hash, these operation hashes
// rotate when Spotify redeploys its web player. Unlike a browser network
// capture, they are extractable straight from the public JS bundle — fetch
// https://open.spotify.com/, find the linked web-player.<build>.js chunk, and
// grep for the persisted-query registration:
//
//	new <mod>.l("queryArtistDiscographyAll","query","<sha256>",null)
//	new <mod>.l("queryArtistOverview","query","<sha256>",null)
//
// A stale hash returns HTTP 412 "Invalid query hash" (not an auth status), so
// isAuthStatus's retry can't mask it — content degrades to empty, the same
// graceful-degradation posture as the rest of this adapter.
const (
	spotifyDiscographyAllHash = "5e07d323febb57b4a56a42abbf781490e58764aa45feb6e3dc0591564fc56599"
	spotifyArtistOverviewHash = "ae0e2958a4ab645b35ca19ac04d0495ae12d9c5d7b7286217674801a9aab281a"
)

// spotifyContentLimit caps the discography fetch. Releases come newest-first
// (order=DATE_DESC), so the head is what the detail screen wants.
const spotifyContentLimit = 50

// GetArtistAlbums implements ports.ArtistContentProvider via the pathfinder
// queryArtistDiscographyAll operation. externalID is the Spotify artist id
// (bridged from MusicBrainz url-relations). Carries release date, cover art,
// track count, and album/single/compilation type — merges by title into the
// cross-provider discography (the empty album-artist is filled by another
// provider in the best-of merge).
func (a *SpotifyAdapter) GetArtistAlbums(ctx context.Context, _ domain.ProviderName, externalID string) ([]domain.SearchResult, error) {
	vars := map[string]any{
		"uri":    "spotify:artist:" + externalID,
		"offset": 0,
		"limit":  spotifyContentLimit,
		"order":  "DATE_DESC",
	}
	var body spotifyDiscographyResponse
	if err := a.pathfinderContent(ctx, "queryArtistDiscographyAll", spotifyDiscographyAllHash, vars, &body); err != nil {
		return nil, err
	}
	groups := body.Data.ArtistUnion.Discography.All.Items
	out := make([]domain.SearchResult, 0, len(groups))
	for _, g := range groups {
		// A group's releases are variants of one release (deluxe/clean/regional);
		// the first is the representative the web player displays.
		if len(g.Releases.Items) == 0 {
			continue
		}
		if r, ok := mapSpotifyRelease(g.Releases.Items[0]); ok {
			out = append(out, r)
		}
	}
	return out, nil
}

// GetArtistTopTracks implements ports.ArtistContentProvider via the pathfinder
// queryArtistOverview operation (its topTracks section).
func (a *SpotifyAdapter) GetArtistTopTracks(ctx context.Context, _ domain.ProviderName, externalID string) ([]domain.SearchResult, error) {
	vars := map[string]any{"uri": "spotify:artist:" + externalID, "locale": ""}
	var body spotifyOverviewResponse
	if err := a.pathfinderContent(ctx, "queryArtistOverview", spotifyArtistOverviewHash, vars, &body); err != nil {
		return nil, err
	}
	items := body.Data.ArtistUnion.Discography.TopTracks.Items
	out := make([]domain.SearchResult, 0, len(items))
	for _, it := range items {
		if r, ok := mapSpotifyOverviewTrack(it.Track); ok {
			out = append(out, r)
		}
	}
	return out, nil
}

// pathfinderContent POSTs a persisted-query operation to the pathfinder GraphQL
// endpoint and decodes the 200 body into out, re-resolving the session once on
// an auth failure (mirrors Search's rotation-tolerant retry).
func (a *SpotifyAdapter) pathfinderContent(ctx context.Context, operationName, hash string, vars map[string]any, out any) error {
	sess, err := a.resolver.get(ctx)
	if err != nil {
		return fmt.Errorf("resolve spotify session: %w", err)
	}
	status, err := a.doPathfinderContent(ctx, sess, operationName, hash, vars, out)
	if err != nil && isAuthStatus(status) {
		a.resolver.invalidate()
		sess, err = a.resolver.get(ctx)
		if err != nil {
			return fmt.Errorf("re-resolve spotify session: %w", err)
		}
		_, err = a.doPathfinderContent(ctx, sess, operationName, hash, vars, out)
	}
	return err
}

func (a *SpotifyAdapter) doPathfinderContent(ctx context.Context, sess *spotifySession, operationName, hash string, vars map[string]any, out any) (int, error) {
	payload, err := json.Marshal(spotifyPFRequest{
		Variables:     vars,
		OperationName: operationName,
		Extensions:    spotifyExtensions{PersistedQuery: spotifyPersistedQuery{Version: 1, Sha256Hash: hash}},
	})
	if err != nil {
		return 0, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, a.pathfinderURL, bytes.NewReader(payload))
	if err != nil {
		return 0, err
	}
	req.Header.Set("Content-Type", "application/json;charset=UTF-8")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+sess.accessToken)
	req.Header.Set("client-token", sess.clientToken)
	req.Header.Set("app-platform", "WebPlayer")
	req.Header.Set("User-Agent", spotifyUserAgent)

	resp, err := a.client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return resp.StatusCode, fmt.Errorf("http status %d", resp.StatusCode)
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, providerBodyCap))
	if err != nil {
		return resp.StatusCode, err
	}
	// Pathfinder signals a stale hash / rejected token with a top-level "errors"
	// array while still returning HTTP 200 (same shape doSearch guards against);
	// without this the empty data decodes cleanly and content silently vanishes.
	var envelope struct {
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}
	if err := json.Unmarshal(raw, &envelope); err == nil && len(envelope.Errors) > 0 {
		return resp.StatusCode, fmt.Errorf("spotify graphql error: %s", envelope.Errors[0].Message)
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return resp.StatusCode, fmt.Errorf("decode %s response: %w", operationName, err)
	}
	return resp.StatusCode, nil
}

// --- pathfinder request/response shapes ---------------------------------

type spotifyPFRequest struct {
	Variables     map[string]any    `json:"variables"`
	OperationName string            `json:"operationName"`
	Extensions    spotifyExtensions `json:"extensions"`
}

// spotifyDiscographyResponse decodes queryArtistDiscographyAll: releases are
// grouped (all.items[]) with per-group variants (releases.items[]).
type spotifyDiscographyResponse struct {
	Data struct {
		ArtistUnion struct {
			Discography struct {
				All struct {
					Items []struct {
						Releases struct {
							Items []spotifyPFRelease `json:"items"`
						} `json:"releases"`
					} `json:"items"`
				} `json:"all"`
			} `json:"discography"`
		} `json:"artistUnion"`
	} `json:"data"`
}

type spotifyPFRelease struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Type     string `json:"type"` // ALBUM | SINGLE | COMPILATION
	URI      string `json:"uri"`
	CoverArt struct {
		Sources []spotifyImage `json:"sources"`
	} `json:"coverArt"`
	Date struct {
		ISOString string `json:"isoString"`
		Year      int    `json:"year"`
	} `json:"date"`
	Tracks struct {
		TotalCount int `json:"totalCount"`
	} `json:"tracks"`
	SharingInfo struct {
		ShareURL string `json:"shareUrl"`
	} `json:"sharingInfo"`
}

// spotifyOverviewResponse decodes only queryArtistOverview's topTracks section
// (the operation returns far more, but this is all the top-tracks endpoint needs).
type spotifyOverviewResponse struct {
	Data struct {
		ArtistUnion struct {
			Discography struct {
				TopTracks struct {
					Items []struct {
						Track spotifyPFTrack `json:"track"`
					} `json:"items"`
				} `json:"topTracks"`
			} `json:"discography"`
		} `json:"artistUnion"`
	} `json:"data"`
}

type spotifyPFTrack struct {
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
		CoverArt struct {
			Sources []spotifyImage `json:"sources"`
		} `json:"coverArt"`
	} `json:"albumOfTrack"`
	Artists struct {
		Items []struct {
			Profile struct {
				Name string `json:"name"`
			} `json:"profile"`
		} `json:"items"`
	} `json:"artists"`
}

// --- mapping -------------------------------------------------------------

func mapSpotifyRelease(rel spotifyPFRelease) (domain.SearchResult, bool) {
	if rel.Name == "" || rel.ID == "" {
		return domain.SearchResult{}, false
	}
	var extras map[string]any
	// type is ALBUM|SINGLE|COMPILATION — record all of it so the discography
	// buckets correctly (the pipeline's record-type normalizer lowercases and
	// maps compilation onto the same key the other providers use).
	if rt := strings.ToLower(rel.Type); rt != "" {
		extras = map[string]any{"record_type": rt}
	}
	// No album-artist here (the discography query omits it), so subtitle is empty:
	// V2 clusters releases by canonical title, and the best-of merge adopts the
	// album artist from whichever other provider carries it.
	r := domain.NewProviderResult(domain.ResultKindAlbum, rel.Name, "",
		spotifyBestImage(rel.CoverArt.Sources),
		domain.SourceRef{Provider: domain.ProviderSpotify, ExternalID: rel.ID, URL: spotifyReleaseURL(rel.SharingInfo.ShareURL, rel.ID)},
		extras)
	r.ReleaseDate = spotifyReleaseDate(rel.Date.ISOString, rel.Date.Year)
	r.TrackCount = rel.Tracks.TotalCount
	return r, true
}

func mapSpotifyOverviewTrack(t spotifyPFTrack) (domain.SearchResult, bool) {
	if t.Name == "" || t.ID == "" {
		return domain.SearchResult{}, false
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
		domain.SourceRef{Provider: domain.ProviderSpotify, ExternalID: t.ID, URL: "https://open.spotify.com/track/" + t.ID},
		extras)
	if t.Duration.TotalMilliseconds > 0 {
		r.Duration = int(t.Duration.TotalMilliseconds / 1000)
	}
	return r, true
}

// spotifyReleaseURL prefers the canonical share URL (stripped of its ?si=
// tracking suffix), falling back to a constructed album link.
func spotifyReleaseURL(shareURL, id string) string {
	if shareURL != "" {
		if i := strings.IndexByte(shareURL, '?'); i >= 0 {
			shareURL = shareURL[:i]
		}
		return shareURL
	}
	return "https://open.spotify.com/album/" + id
}

// spotifyReleaseDate normalizes pathfinder's ISO date to the YYYY-MM-DD form the
// other providers use, falling back to the bare year when no full date is given.
func spotifyReleaseDate(iso string, year int) string {
	if len(iso) >= 10 {
		return iso[:10]
	}
	if year > 0 {
		return strconv.Itoa(year)
	}
	return ""
}
