package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

const lastfmBaseURL = "https://ws.audioscrobbler.com/2.0/"

type lastfmEndpoint struct {
	decodedAs
	query lastfmQuery
}

type lastfmQuery func(id, key string) (string, error)

var lastfmEndpoints = map[string]lastfmEndpoint{
	"search_track":      {"parseLastFmTracks.resp", lastfmSearch("track")},
	"search_album":      {"parseLastFmAlbums.resp", lastfmSearch("album")},
	"search_artist":     {"parseLastFmArtists.resp", lastfmSearch("artist")},
	"chart_top_artists": {"parseLastFmChartArtists.resp", lastfmChart("chart.gettopartists")},
	"chart_top_tracks":  {"parseLastFmChartTracks.resp", lastfmChart("chart.gettoptracks")},
	"artist_top_tracks": {"GetArtistTopTracks.body", lastfmArtistTopTracks},
	"artist_top_albums": {"GetArtistAlbums.body", lastfmArtistTopAlbums},
	"artist_info":       {"lookupArtistInfo.resp", lastfmArtistInfo},
	"track_info":        {"lookupTrackInfo.resp", lastfmPairInfo("track")},
	"album_info":        {"lookupAlbumInfo.resp", lastfmPairInfo("album")},
}

func lastfmProvider() provider {
	return provider{
		adapter:   adapterRoot + "lastfm",
		decodedAs: decodedTypes(lastfmEndpoints),
		decodedLater: []rawField{
			{entity: "artist_info", path: "artist.tags", decodedAs: "parseLastFmTags.parsed"},
			{entity: "artist_info", path: "artist.similar", decodedAs: "parseLastFmSimilarArtists.parsed"},
			{entity: "track_info", path: "track.album", decodedAs: "parseLastFmAlbumTitle.parsed"},
			{entity: "track_info", path: "track.toptags", decodedAs: "parseLastFmTags.parsed"},
			{entity: "album_info", path: "album.tags", decodedAs: "parseLastFmTags.parsed"},
		},
		source: func(client *http.Client) (source, error) {
			key, err := envKey("LASTFM_API_KEY")
			if err != nil {
				return nil, err
			}
			return newLastfmSource(client, lastfmBaseURL, key), nil
		},
	}
}

func newLastfmSource(client *http.Client, base, key string) liveSource {
	fetchers := liveSource{}
	for entity, endpoint := range lastfmEndpoints {
		fetchers[entity] = func(ctx context.Context, id string) (json.RawMessage, error) {
			query, err := endpoint.query(id, key)
			if err != nil {
				return nil, err
			}
			return lastfmGet(ctx, client, base+"?"+query)
		}
	}
	return fetchers
}

func lastfmGet(ctx context.Context, client *http.Client, target string) (json.RawMessage, error) {
	body, err := fetch(ctx, client, request{method: http.MethodGet, url: target})
	if err != nil {
		return nil, err
	}
	var envelope struct {
		Code int `json:"error"`
	}
	if json.Unmarshal(body, &envelope) == nil && envelope.Code != 0 {
		return nil, fmt.Errorf("lastfm api error %d", envelope.Code)
	}
	return body, nil
}

func lastfmSearch(kind string) lastfmQuery {
	return func(query, key string) (string, error) {
		return fmt.Sprintf("method=%s.search&%s=%s&api_key=%s&format=json&limit=15",
			kind, kind, url.QueryEscape(query), key), nil
	}
}

func lastfmChart(method string) lastfmQuery {
	return func(limit, key string) (string, error) {
		return fmt.Sprintf("method=%s&limit=%s&api_key=%s&format=json", method, url.QueryEscape(limit), key), nil
	}
}

func lastfmArtistTopTracks(artist, key string) (string, error) {
	return fmt.Sprintf("method=artist.gettoptracks&%s&api_key=%s&format=json&limit=10&autocorrect=1",
		lastfmArtistParam(artist), key), nil
}

func lastfmArtistTopAlbums(artist, key string) (string, error) {
	return fmt.Sprintf("method=artist.gettopalbums&artist=%s&api_key=%s&format=json&limit=50",
		url.QueryEscape(artist), key), nil
}

func lastfmArtistInfo(artist, key string) (string, error) {
	return fmt.Sprintf("method=artist.getinfo&artist=%s&autocorrect=1&api_key=%s&format=json",
		url.QueryEscape(artist), key), nil
}

func lastfmPairInfo(kind string) lastfmQuery {
	return func(id, key string) (string, error) {
		parts, err := fixtureParts(id, "artist", "title")
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("method=%s.getinfo&artist=%s&%s=%s&autocorrect=1&api_key=%s&format=json",
			kind, url.QueryEscape(parts[0]), kind, url.QueryEscape(parts[1]), key), nil
	}
}

func lastfmArtistParam(artist string) string {
	if lastfmLooksLikeMBID(artist) {
		return "mbid=" + artist
	}
	return "artist=" + url.QueryEscape(artist)
}

func lastfmLooksLikeMBID(s string) bool {
	if len(s) != 36 {
		return false
	}
	for i, c := range s {
		if !lastfmMBIDRune(i, c) {
			return false
		}
	}
	return true
}

func lastfmMBIDRune(i int, c rune) bool {
	if i == 8 || i == 13 || i == 18 || i == 23 {
		return c == '-'
	}
	return strings.ContainsRune("0123456789abcdefABCDEF", c)
}
