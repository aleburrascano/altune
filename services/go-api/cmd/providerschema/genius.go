package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
)

const (
	geniusBaseURL      = "https://api.genius.com"
	geniusSongsPerPage = 50
)

type geniusEndpoint struct {
	decodedAs
	path func(id string) (string, error)
}

var geniusEndpoints = map[string]geniusEndpoint{
	"search":       {"searchGenius.body", geniusSearchPath},
	"search_songs": {"searchHits.body", geniusSearchPath},
	"artist_songs": {"songsPage.body", geniusArtistSongsPath},
	"song":         {"geniusSongDetailBody", geniusSongPath},
}

func geniusProvider() provider {
	return provider{
		adapter:   adapterRoot + "genius",
		decodedAs: decodedTypes(geniusEndpoints),
		source: func(client *http.Client) (source, error) {
			token, err := envKey("GENIUS_ACCESS_TOKEN")
			if err != nil {
				return nil, err
			}
			return newGeniusSource(client, geniusBaseURL, token), nil
		},
	}
}

func newGeniusSource(client *http.Client, base, token string) liveSource {
	fetchers := liveSource{}
	for entity, endpoint := range geniusEndpoints {
		fetchers[entity] = func(ctx context.Context, id string) (json.RawMessage, error) {
			path, err := endpoint.path(id)
			if err != nil {
				return nil, err
			}
			return fetch(ctx, client, request{
				method: http.MethodGet,
				url:    base + path,
				header: map[string]string{"Authorization": "Bearer " + token},
			})
		}
	}
	return fetchers
}

func geniusSearchPath(query string) (string, error) {
	return "/search?q=" + url.QueryEscape(query), nil
}

func geniusArtistSongsPath(id string) (string, error) {
	numbers, err := fixtureNumbers(id, "artistID", "page")
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("/artists/%d/songs?sort=popularity&per_page=%d&page=%d", numbers[0], geniusSongsPerPage, numbers[1]), nil
}

func geniusSongPath(id string) (string, error) {
	numbers, err := fixtureNumbers(id, "songID")
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("/songs/%d", numbers[0]), nil
}
