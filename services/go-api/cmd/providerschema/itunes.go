package main

import (
	"altune/go-api/internal/shared/clock"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"time"
)

const (
	itunesBaseURL   = "https://itunes.apple.com"
	itunesUserAgent = "Altune/1.0 (music manager; self-hosted)"
	itunesInterval  = 3 * time.Second
)

type itunesEndpoint struct {
	decodedAs
	path string
}

var itunesEndpoints = map[string]itunesEndpoint{
	"search_track":  {"itunesResponse", "/search?term=%s&entity=song&country=US&limit=200"},
	"search_album":  {"itunesResponse", "/search?term=%s&entity=album&country=US&limit=200"},
	"search_artist": {"itunesResponse", "/search?term=%s&entity=musicArtist&country=US&limit=200"},
	"lookup_song":   {"itunesResponse", "/lookup?id=%s&entity=song&country=US&limit=50"},
	"lookup_album":  {"itunesResponse", "/lookup?id=%s&entity=album&country=US&limit=50"},
}

func itunesProvider() provider {
	return provider{
		adapter:   adapterRoot + "itunes",
		decodedAs: decodedTypes(itunesEndpoints),
		source: func(client *http.Client) (source, error) {
			return newITunesSource(client, itunesBaseURL, newIntervalGate(clock.Now, itunesInterval)), nil
		},
	}
}

func newITunesSource(client *http.Client, base string, gate *intervalGate) liveSource {
	fetchers := liveSource{}
	for entity, endpoint := range itunesEndpoints {
		fetchers[entity] = func(ctx context.Context, id string) (json.RawMessage, error) {
			return pacedFetch(ctx, client, gate, request{
				method: http.MethodGet,
				url:    base + fmt.Sprintf(endpoint.path, url.QueryEscape(id)),
				header: map[string]string{"User-Agent": itunesUserAgent},
			})
		}
	}
	return fetchers
}
