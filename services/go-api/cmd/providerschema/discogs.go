package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"time"
)

const (
	discogsBaseURL   = "https://api.discogs.com"
	discogsUserAgent = "altune-providerschema/1.0"
	discogsInterval  = time.Second
)

type discogsEndpoint struct {
	decodedAs
	path func(id string) (string, error)
}

var discogsEndpoints = map[string]discogsEndpoint{
	"search_artist":   {"discogsSearchResponse", discogsSearchPath},
	"artist":          {"discogsArtistDetail", discogsArtistPath},
	"artist_releases": {"discogsReleasesResponse", discogsReleasesPath},
}

func discogsProvider() provider {
	return provider{
		adapter:   adapterRoot + "discogs",
		decodedAs: decodedTypes(discogsEndpoints),
		source: func(client *http.Client) (source, error) {
			token, err := envKey("DISCOGS_TOKEN")
			if err != nil {
				return nil, err
			}
			return newDiscogsSource(client, discogsBaseURL, token, newIntervalGate(discogsInterval)), nil
		},
	}
}

func newDiscogsSource(client *http.Client, base, token string, gate *intervalGate) liveSource {
	fetchers := liveSource{}
	for entity, endpoint := range discogsEndpoints {
		fetchers[entity] = func(ctx context.Context, id string) (json.RawMessage, error) {
			path, err := endpoint.path(id)
			if err != nil {
				return nil, err
			}
			return pacedFetch(ctx, client, gate, request{
				method: http.MethodGet,
				url:    base + path,
				header: map[string]string{"Authorization": "Discogs token=" + token, "User-Agent": discogsUserAgent},
			})
		}
	}
	return fetchers
}

func discogsSearchPath(query string) (string, error) {
	return "/database/search?type=artist&q=" + url.QueryEscape(query) + "&per_page=5", nil
}

func discogsArtistPath(id string) (string, error) {
	numbers, err := fixtureNumbers(id, "artistID")
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("/artists/%d", numbers[0]), nil
}

func discogsReleasesPath(id string) (string, error) {
	numbers, err := fixtureNumbers(id, "artistID", "perPage")
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("/artists/%d/releases?sort=year&sort_order=desc&per_page=%d", numbers[0], numbers[1]), nil
}
