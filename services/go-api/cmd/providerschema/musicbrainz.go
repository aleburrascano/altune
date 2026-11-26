package main

import (
	"altune/go-api/internal/shared/clock"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"time"
)

const (
	musicBrainzBaseURL   = "https://musicbrainz.org/ws/2"
	musicBrainzUserAgent = "altune-providerschema/1.0"
	musicBrainzInterval  = 1100 * time.Millisecond
	musicBrainzRetries   = 5
	musicBrainzBackoff   = 2 * time.Second
)

type musicBrainzEndpoint struct {
	decodedAs
	path   string
	escape func(string) string
}

var musicBrainzEndpoints = map[string]musicBrainzEndpoint{
	"search_recording":      {"mbRecordingResponse", "/recording/?query=%s&fmt=json&limit=15&inc=isrcs", url.QueryEscape},
	"search_artist":         {"mbArtistResponse", "/artist/?query=%s&fmt=json&limit=15", url.QueryEscape},
	"search_release_group":  {"mbReleaseGroupResponse", "/release-group/?query=%s&fmt=json&limit=15", url.QueryEscape},
	"isrc":                  {"mbRecordingResponse", "/isrc/%s?fmt=json", url.PathEscape},
	"recording_detail":      {"mbRecordingDetailResponse", "/recording/%s?inc=isrcs+artist-credits+releases&fmt=json", url.PathEscape},
	"recording":             {"mbRecording", "/recording/%s?inc=isrcs+artist-credits&fmt=json", url.PathEscape},
	"recording_works":       {"mbRecordingWorkRels", "/recording/%s?inc=work-rels+artist-credits&fmt=json", url.PathEscape},
	"work":                  {"mbWorkRecordingRels", "/work/%s?inc=recording-rels+artist-credits&fmt=json", url.PathEscape},
	"artist":                {"mbLookupArtist", "/artist/%s?inc=genres+ratings+url-rels&fmt=json", url.PathEscape},
	"release_group":         {"mbLookupReleaseGroup", "/release-group/%s?inc=genres+ratings&fmt=json", url.PathEscape},
	"browse_release_groups": {"mbReleaseGroupResponse", "/release-group?artist=%s&type=album%%7Cep%%7Csingle&fmt=json&limit=100&offset=0", url.QueryEscape},
}

func musicBrainzProvider() provider {
	return provider{
		adapter:   adapterRoot + "musicbrainz",
		decodedAs: decodedTypes(musicBrainzEndpoints),
		source: func(client *http.Client) (source, error) {
			return newMusicBrainzSource(client, musicBrainzBaseURL, newIntervalGate(musicBrainzInterval), musicBrainzBackoff), nil
		},
	}
}

func newMusicBrainzSource(client *http.Client, base string, gate *intervalGate, backoff time.Duration) liveSource {
	musicBrainz := &musicBrainzClient{http: client, gate: gate, backoff: backoff}
	fetchers := liveSource{}
	for entity, endpoint := range musicBrainzEndpoints {
		fetchers[entity] = func(ctx context.Context, id string) (json.RawMessage, error) {
			return musicBrainz.get(ctx, base+fmt.Sprintf(endpoint.path, endpoint.escape(id)))
		}
	}
	return fetchers
}

type musicBrainzClient struct {
	http    *http.Client
	gate    *intervalGate
	backoff time.Duration
}

func (musicBrainz *musicBrainzClient) get(ctx context.Context, target string) (json.RawMessage, error) {
	for attempt := 0; ; attempt++ {
		body, err := pacedFetch(ctx, musicBrainz.http, musicBrainz.gate, request{
			method: http.MethodGet,
			url:    target,
			header: map[string]string{"User-Agent": musicBrainzUserAgent, "Accept": "application/json"},
		})
		if !musicBrainzBusy(err) || attempt == musicBrainzRetries {
			return body, err
		}
		if err := clock.Sleep(ctx, musicBrainz.backoff<<attempt); err != nil {
			return nil, err
		}
	}
}

func musicBrainzBusy(err error) bool {
	var failure statusError
	return errors.As(err, &failure) && failure.status == http.StatusServiceUnavailable
}
