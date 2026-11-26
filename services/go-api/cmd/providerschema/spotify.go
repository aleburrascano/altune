package main

import (
	"altune/go-api/internal/shared/clock"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

const (
	spotifySiteURL         = "https://open.spotify.com"
	spotifyClientTokenURL  = "https://clienttoken.spotify.com/v1/clienttoken"
	spotifyPathfinderURL   = "https://api-partner.spotify.com/pathfinder/v2/query"
	spotifyUserAgent       = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0.0.0 Safari/537.36"
	spotifyOEmbedUserAgent = "altune/1.0 (+https://github.com/aleburrascano/altune)"
	spotifyWebClientID     = "d8a5ed958d274c2e8ee717e6a4b0971d"
	spotifyWebClientVer    = "1.2.95.397.gd60c55ec"
	spotifyContentPageSize = 50
)

type spotifyEndpoint struct {
	decodedAs
	fetch spotifyFetch
}

type spotifyFetch func(spotify *spotifySession, ctx context.Context, id string) (json.RawMessage, error)

var spotifyEndpoints = map[string]spotifyEndpoint{
	"search":             {"spotifySearchResponse", spotifyPathfinder("search")},
	"artist_discography": {"spotifyDiscographyResponse", spotifyPathfinder("artist_discography")},
	"artist_overview":    {"spotifyOverviewResponse", spotifyPathfinder("artist_overview")},
	"album_tracks":       {"spotifyAlbumTracksResponse", spotifyPathfinder("album_tracks")},
	"content_error":      {"doPathfinderContent.envelope", (*spotifySession).contentError},
	"oembed":             {"ResolveByIdentity.body", (*spotifySession).oembed},
	"server_time":        {"fetchServerTime.body", (*spotifySession).serverTime},
	"access_token":       {"resolveAccessToken.body", (*spotifySession).accessTokenBody},
	"client_token":       {"resolveClientToken.body", (*spotifySession).clientTokenBody},
}

func spotifyProvider() provider {
	return provider{
		adapter:   adapterRoot + "spotify",
		decodedAs: decodedTypes(spotifyEndpoints),
		source: func(client *http.Client) (source, error) {
			urls := spotifyURLs{site: spotifySiteURL, clientToken: spotifyClientTokenURL, pathfinder: spotifyPathfinderURL}
			return newSpotifySource(clock.Now, client, urls), nil
		},
	}
}

type spotifyURLs struct {
	site        string
	clientToken string
	pathfinder  string
}

type spotifySession struct {
	client      *http.Client
	urls        spotifyURLs
	accessToken string
	clientToken string
	now         clock.Func
}

func newSpotifySource(now clock.Func, client *http.Client, urls spotifyURLs) liveSource {
	spotify := &spotifySession{client: client, urls: urls, now: now}
	fetchers := liveSource{}
	for entity, endpoint := range spotifyEndpoints {
		fetchers[entity] = func(ctx context.Context, id string) (json.RawMessage, error) {
			return endpoint.fetch(spotify, ctx, id)
		}
	}
	return fetchers
}

type spotifyOperation struct {
	name string
	hash string
	vars func(id string) map[string]any
}

var spotifyOperations = map[string]spotifyOperation{
	"search":             {"searchDesktop", "db61238974d27839a136c9dc02bfdbe3fab7635f21cf85976ebff9a1ee281345", spotifySearchVars},
	"artist_discography": {"queryArtistDiscographyAll", "5e07d323febb57b4a56a42abbf781490e58764aa45feb6e3dc0591564fc56599", spotifyDiscographyVars},
	"artist_overview":    {"queryArtistOverview", "ae0e2958a4ab645b35ca19ac04d0495ae12d9c5d7b7286217674801a9aab281a", spotifyOverviewVars},
	"album_tracks":       {"queryAlbumTracks", "b9bfabef66ed756e5e13f68a942deb60bd4125ec1f1be8cc42769dc0259b4b10", spotifyAlbumTracksVars},
}

var spotifyOEmbedKinds = map[string]bool{"track": true, "album": true, "artist": true}

func spotifyPathfinder(operation string) spotifyFetch {
	return func(spotify *spotifySession, ctx context.Context, id string) (json.RawMessage, error) {
		return spotify.pathfinder(ctx, spotifyOperations[operation], id)
	}
}

func spotifySearchVars(term string) map[string]any {
	return map[string]any{
		"searchTerm":                     term,
		"offset":                         0,
		"limit":                          10,
		"numberOfTopResults":             5,
		"includeAudiobooks":              false,
		"includeArtistHasConcertsField":  false,
		"includePreReleases":             false,
		"includeAlbumPreReleases":        false,
		"includeAuthors":                 false,
		"includeEpisodeContentRatingsV2": false,
		"sectionFilters":                 []string{"GENERIC"},
	}
}

func spotifyDiscographyVars(id string) map[string]any {
	return map[string]any{"uri": "spotify:artist:" + id, "offset": 0, "limit": spotifyContentPageSize, "order": "DATE_DESC"}
}

func spotifyOverviewVars(id string) map[string]any {
	return map[string]any{"uri": "spotify:artist:" + id, "locale": ""}
}

func spotifyAlbumTracksVars(id string) map[string]any {
	return map[string]any{"uri": "spotify:album:" + id, "offset": 0, "limit": spotifyContentPageSize}
}

type spotifyQuery struct {
	Variables     map[string]any `json:"variables"`
	OperationName string         `json:"operationName"`
	Extensions    struct {
		PersistedQuery struct {
			Version    int    `json:"version"`
			Sha256Hash string `json:"sha256Hash"`
		} `json:"persistedQuery"`
	} `json:"extensions"`
}

func (spotify *spotifySession) pathfinder(ctx context.Context, op spotifyOperation, id string) (json.RawMessage, error) {
	if err := spotify.ensureSession(ctx); err != nil {
		return nil, err
	}
	query := spotifyQuery{Variables: op.vars(id), OperationName: op.name}
	query.Extensions.PersistedQuery.Version = 1
	query.Extensions.PersistedQuery.Sha256Hash = op.hash
	payload, err := json.Marshal(query)
	if err != nil {
		return nil, err
	}
	return fetch(ctx, spotify.client, request{method: http.MethodPost, url: spotify.urls.pathfinder, body: payload, header: map[string]string{
		"Content-Type":  "application/json;charset=UTF-8",
		"Accept":        "application/json",
		"Authorization": "Bearer " + spotify.accessToken,
		"client-token":  spotify.clientToken,
		"app-platform":  "WebPlayer",
		"User-Agent":    spotifyUserAgent,
	}})
}

func (spotify *spotifySession) contentError(ctx context.Context, id string) (json.RawMessage, error) {
	parts, err := fixtureParts(id, "content entity", "id")
	if err != nil {
		return nil, err
	}
	op, ok := spotifyOperations[parts[0]]
	if !ok || parts[0] == "search" {
		return nil, fmt.Errorf("spotify content_error fixture %q does not name a content entity", id)
	}
	return spotify.pathfinder(ctx, op, parts[1])
}

func (spotify *spotifySession) oembed(ctx context.Context, id string) (json.RawMessage, error) {
	kind, spotifyID, ok := strings.Cut(id, "/")
	if !spotifyOEmbedKinds[kind] || !ok || spotifyID == "" {
		return nil, fmt.Errorf("spotify oembed fixture %q is not track/<id>, album/<id> or artist/<id>", id)
	}
	rawURL := fmt.Sprintf("%s/oembed?url=https://open.spotify.com/%s/%s", spotify.urls.site, kind, spotifyID)
	return fetch(ctx, spotify.client, request{method: http.MethodGet, url: rawURL, header: map[string]string{"User-Agent": spotifyOEmbedUserAgent}})
}
