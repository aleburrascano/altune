package main

import (
	"altune/go-api/internal/shared/clock"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	deezerAPIURL   = "https://api.deezer.com"
	deezerAuthURL  = "https://auth.deezer.com/login/anonymous?jo=p&rto=c&i=c"
	deezerPipeURL  = "https://pipe.deezer.com/api"
	deezerInterval = 200 * time.Millisecond
)

const deezerLyricsQuery = `query SynchronizedLyrics($trackId: String!) {
  track(trackId: $trackId) {
    id
    lyrics {
      id
      copyright
      text
      writers
      synchronizedLines { lrcTimestamp line milliseconds duration }
    }
  }
}`

type deezerEndpoint struct {
	decodedAs
	fetch deezerFetch
}

type deezerFetch func(deezer *deezerSession, ctx context.Context, id string) (json.RawMessage, error)

var deezerEndpoints = map[string]deezerEndpoint{
	"search_track":   {"deezerSearchResponse", deezerAPI("/search/track?q=%s&limit=15&order=RANKING", deezerSearchTerms("track"))},
	"search_album":   {"deezerSearchResponse", deezerAPI("/search/album?q=%s&limit=15&order=RANKING", deezerSearchTerms("album"))},
	"search_artist":  {"deezerSearchResponse", deezerAPI("/search/artist?q=%s&limit=15&order=RANKING", deezerSearchTerms("artist"))},
	"chart_tracks":   {"deezerSearchResponse", deezerAPI("/chart/0/tracks?limit=%s", url.QueryEscape)},
	"chart_albums":   {"deezerSearchResponse", deezerAPI("/chart/0/albums?limit=%s", url.QueryEscape)},
	"chart_artists":  {"deezerSearchResponse", deezerAPI("/chart/0/artists?limit=%s", url.QueryEscape)},
	"album_tracks":   {"deezerSearchResponse", deezerAPI("/album/%s/tracks?limit=100&index=0", url.PathEscape)},
	"artist_albums":  {"deezerSearchResponse", deezerAPI("/artist/%s/albums?limit=100&index=0", url.PathEscape)},
	"artist_top":     {"deezerSearchResponse", deezerAPI("/artist/%s/top?limit=10", url.PathEscape)},
	"track":          {"lookupTrackDetail.detail", deezerAPI("/track/%s", url.PathEscape)},
	"album":          {"lookupAlbumDetail.detail", deezerAPI("/album/%s", url.PathEscape)},
	"auth_anonymous": {"resolve.out", (*deezerSession).anonymous},
	"lyrics":         {"parseSynchronizedLyrics.env", (*deezerSession).lyrics},
}

func deezerProvider() provider {
	return provider{
		adapter:   adapterRoot + "deezer",
		decodedAs: decodedTypes(deezerEndpoints),
		source: func(client *http.Client) (source, error) {
			urls := deezerURLs{api: deezerAPIURL, auth: deezerAuthURL, pipe: deezerPipeURL}
			return newDeezerSource(client, urls, newIntervalGate(clock.Now, deezerInterval)), nil
		},
	}
}

type deezerURLs struct {
	api  string
	auth string
	pipe string
}

type deezerSession struct {
	client *http.Client
	urls   deezerURLs
	gate   *intervalGate
	jwt    string
}

func newDeezerSource(client *http.Client, urls deezerURLs, gate *intervalGate) liveSource {
	deezer := &deezerSession{client: client, urls: urls, gate: gate}
	fetchers := liveSource{}
	for entity, endpoint := range deezerEndpoints {
		fetchers[entity] = func(ctx context.Context, id string) (json.RawMessage, error) {
			return endpoint.fetch(deezer, ctx, id)
		}
	}
	return fetchers
}

func deezerAPI(path string, escape func(string) string) deezerFetch {
	return func(deezer *deezerSession, ctx context.Context, id string) (json.RawMessage, error) {
		return deezer.api(ctx, deezer.urls.api+fmt.Sprintf(path, escape(id)))
	}
}

func deezerSearchTerms(kind string) func(id string) string {
	return func(id string) string { return url.QueryEscape(deezerSearchQuery(kind, id)) }
}

func deezerSearchQuery(kind, id string) string {
	artist, title, structured := strings.Cut(id, "|")
	if !structured {
		return id
	}
	field := map[string]string{"track": "track", "album": "album"}[kind]
	if field == "" {
		return artist
	}
	return "artist:" + deezerQuote(artist) + " " + field + ":" + deezerQuote(title)
}

func deezerQuote(s string) string {
	return `"` + strings.ReplaceAll(s, `"`, "") + `"`
}

func (deezer *deezerSession) api(ctx context.Context, target string) (json.RawMessage, error) {
	body, err := pacedFetch(ctx, deezer.client, deezer.gate, request{method: http.MethodGet, url: target})
	if err != nil {
		return nil, err
	}
	var envelope struct {
		Error *struct {
			Type string `json:"type"`
			Code int    `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		return nil, fmt.Errorf("deezer api: %w", err)
	}
	if envelope.Error != nil {
		return nil, fmt.Errorf("deezer api error %d (%s)", envelope.Error.Code, envelope.Error.Type)
	}
	return body, nil
}

func (deezer *deezerSession) lyrics(ctx context.Context, trackID string) (json.RawMessage, error) {
	jwt, err := deezer.token(ctx)
	if err != nil {
		return nil, err
	}
	payload, err := json.Marshal(map[string]any{
		"operationName": "SynchronizedLyrics",
		"query":         deezerLyricsQuery,
		"variables":     map[string]string{"trackId": trackID},
	})
	if err != nil {
		return nil, err
	}
	return pacedFetch(ctx, deezer.client, deezer.gate, request{method: http.MethodPost, url: deezer.urls.pipe, body: payload, header: map[string]string{
		"Content-Type": "application/json", "Authorization": "Bearer " + jwt,
	}})
}

func (deezer *deezerSession) anonymous(ctx context.Context, _ string) (json.RawMessage, error) {
	return pacedFetch(ctx, deezer.client, deezer.gate, request{method: http.MethodGet, url: deezer.urls.auth})
}

func (deezer *deezerSession) token(ctx context.Context) (string, error) {
	if deezer.jwt != "" {
		return deezer.jwt, nil
	}
	body, err := deezer.anonymous(ctx, "")
	if err != nil {
		return "", err
	}
	var out struct {
		JWT string `json:"jwt"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return "", fmt.Errorf("deezer anonymous jwt: %w", err)
	}
	if out.JWT = strings.TrimSpace(out.JWT); out.JWT == "" {
		return "", errors.New("deezer anonymous auth returned no jwt")
	}
	deezer.jwt = out.JWT
	return deezer.jwt, nil
}
