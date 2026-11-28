package main

import (
	"altune/go-api/internal/platform/outbound"
	"context"
	"encoding/json"
	"fmt"
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
		sharedDirs: []string{"services/go-api/internal/platform/outbound/internal/adapters/youtubemusic"},
		source: func(client *http.Client) (source, error) {
			return newYTMusicSource(client, outbound.YouTubeMusicBaseURL), nil
		},
	}
}

func newYTMusicSource(client *http.Client, base string) liveSource {
	fetchers := liveSource{}
	for entity, endpoint := range ytMusicEndpoints {
		fetchers[entity] = func(ctx context.Context, query string) (json.RawMessage, error) {
			body, err := json.Marshal(outbound.YouTubeMusicBody(ytMusicSearchFields(query, endpoint.filter)))
			if err != nil {
				return nil, fmt.Errorf("marshal: %w", err)
			}
			return fetch(ctx, client, request{
				method: http.MethodPost,
				url:    outbound.YouTubeMusicEndpointURL(base, "search"),
				body:   body,
				header: map[string]string{
					"Content-Type": "application/json; charset=utf-8",
					"Referer":      ytMusicReferer,
					"User-Agent":   outbound.YouTubeMusicUserAgent,
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
