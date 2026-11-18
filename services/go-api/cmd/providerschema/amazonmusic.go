package main

import (
	"context"
	"encoding/json"
	"net/http"
)

const (
	amazonMusicBaseURL   = "https://music.amazon.com"
	amazonMusicUserAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0.0.0 Safari/537.36"
)

type amazonMusicEndpoint struct {
	decodedAs
	path string
}

var amazonMusicEndpoints = map[string]amazonMusicEndpoint{
	"config": {"amazonMusicSession", "/config.json"},
}

func amazonMusicProvider() provider {
	return provider{
		adapter:   adapterRoot + "amazonmusic",
		decodedAs: decodedTypes(amazonMusicEndpoints),
		source: func(client *http.Client) (source, error) {
			return newAmazonMusicSource(client, amazonMusicBaseURL), nil
		},
	}
}

func newAmazonMusicSource(client *http.Client, base string) liveSource {
	fetchers := liveSource{}
	for entity, endpoint := range amazonMusicEndpoints {
		fetchers[entity] = func(ctx context.Context, _ string) (json.RawMessage, error) {
			return fetch(ctx, client, request{
				method: http.MethodGet,
				url:    base + endpoint.path,
				header: map[string]string{"User-Agent": amazonMusicUserAgent},
			})
		}
	}
	return fetchers
}
