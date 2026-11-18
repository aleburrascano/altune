package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
)

const fanartTVBaseURL = "https://webservice.fanart.tv/v3/"

type fanartTVEndpoint struct {
	decodedAs
	path string
}

var fanartTVEndpoints = map[string]fanartTVEndpoint{
	"artist": {"fanartDoc", "music/%s"},
	"album":  {"fanartDoc", "music/albums/%s"},
}

func fanartTVProvider() provider {
	return provider{
		adapter:   adapterRoot + "fanarttv",
		decodedAs: decodedTypes(fanartTVEndpoints),
		source: func(client *http.Client) (source, error) {
			key, err := envKey("FANARTTV_API_KEY")
			if err != nil {
				return nil, err
			}
			return newFanartTVSource(client, fanartTVBaseURL, key), nil
		},
	}
}

func newFanartTVSource(client *http.Client, base, key string) liveSource {
	fetchers := liveSource{}
	for entity, endpoint := range fanartTVEndpoints {
		fetchers[entity] = func(ctx context.Context, mbid string) (json.RawMessage, error) {
			path := fmt.Sprintf(endpoint.path, url.PathEscape(mbid))
			return fetch(ctx, client, request{method: http.MethodGet, url: base + path + "?api_key=" + key})
		}
	}
	return fetchers
}
