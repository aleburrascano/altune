package main

import (
	"altune/go-api/internal/shared/ytminnertube"
	"context"
	"encoding/json"
	"net/http"
)

const ytMusicReferer = "https://music.youtube.com/search"

type ytMusicEndpoint struct {
	decodedAs
	filter string
}

var ytMusicEndpoints = map[string]ytMusicEndpoint{
	"search":        {decodedByKeys, ""},
	"search_album":  {decodedByKeys, "EgWKAQIYAWoMEA4QChADEAQQCRAF"},
	"search_artist": {decodedByKeys, "EgWKAQIgAWoMEA4QChADEAQQCRAF"},
}

func ytMusicProvider() provider {
	return provider{
		adapter:    adapterRoot + "ytmusic",
		decodedAs:  decodedTypes(ytMusicEndpoints),
		sharedDirs: []string{"services/go-api/internal/shared/ytminnertube"},
		source: func(client *http.Client) (source, error) {
			return newYTMusicSource(client, ytminnertube.MusicURL), nil
		},
	}
}

func newYTMusicSource(client *http.Client, base string) liveSource {
	fetchers := liveSource{}
	for entity, endpoint := range ytMusicEndpoints {
		fetchers[entity] = func(ctx context.Context, query string) (json.RawMessage, error) {
			body, err := json.Marshal(ytminnertube.Body(ytMusicSearchFields(query, endpoint.filter)))
			if err != nil {
				return nil, err
			}
			return fetch(ctx, client, request{
				method: http.MethodPost,
				url:    ytminnertube.URL(base, "search"),
				body:   body,
				header: map[string]string{
					"Content-Type": "application/json; charset=utf-8",
					"Referer":      ytMusicReferer,
					"User-Agent":   ytminnertube.UserAgent,
				},
			})
		}
	}
	return fetchers
}

func ytMusicSearchFields(query, filter string) map[string]any {
	fields := map[string]any{"query": query}
	if filter != "" {
		fields["params"] = filter
	}
	return fields
}
