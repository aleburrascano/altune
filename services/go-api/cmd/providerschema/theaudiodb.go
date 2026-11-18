package main

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
)

const (
	theAudioDBBaseURL = "https://theaudiodb.com/api/v1/json/"
	theAudioDBTestKey = "123"
)

type theAudioDBEndpoint struct {
	decodedAs
	path func(id string) (string, error)
}

var theAudioDBEndpoints = map[string]theAudioDBEndpoint{
	"search_artist": {"Search.body", theAudioDBArtistPath},
	"search_album":  {"resolveAlbum.body", theAudioDBAlbumPath},
	"artist_mb":     {"artistThumbByMBID.body", theAudioDBMBIDPath},
}

func theAudioDBProvider() provider {
	return provider{
		adapter:   adapterRoot + "theaudiodb",
		decodedAs: decodedTypes(theAudioDBEndpoints),
		source: func(client *http.Client) (source, error) {
			return newTheAudioDBSource(client, theAudioDBBaseURL, cmp.Or(os.Getenv("THEAUDIODB_API_KEY"), theAudioDBTestKey)), nil
		},
	}
}

func newTheAudioDBSource(client *http.Client, base, key string) liveSource {
	fetchers := liveSource{}
	for entity, endpoint := range theAudioDBEndpoints {
		fetchers[entity] = func(ctx context.Context, id string) (json.RawMessage, error) {
			path, err := endpoint.path(id)
			if err != nil {
				return nil, err
			}
			return fetch(ctx, client, request{method: http.MethodGet, url: base + url.PathEscape(key) + "/" + path, secret: key})
		}
	}
	return fetchers
}

func theAudioDBArtistPath(name string) (string, error) {
	return "search.php?s=" + url.QueryEscape(name), nil
}

func theAudioDBAlbumPath(id string) (string, error) {
	parts, err := fixtureParts(id, "artist", "album")
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("searchalbum.php?s=%s&a=%s", url.QueryEscape(parts[0]), url.QueryEscape(parts[1])), nil
}

func theAudioDBMBIDPath(mbid string) (string, error) {
	return "artist-mb.php?i=" + url.QueryEscape(mbid), nil
}
