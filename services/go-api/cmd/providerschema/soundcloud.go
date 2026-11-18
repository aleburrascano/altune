package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

const (
	soundCloudSiteURL   = "https://soundcloud.com/"
	soundCloudAPIURL    = "https://api-v2.soundcloud.com"
	soundCloudUserAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0.0.0 Safari/537.36"
)

var (
	soundCloudAsset    = regexp.MustCompile(`https?://[^"' ]+/assets/[^"' ]+\.js`)
	soundCloudClientID = regexp.MustCompile(`client_id\s*[:=]\s*"?([a-zA-Z0-9]{32})"?`)
	soundCloudDigits   = regexp.MustCompile(`^\d+$`)
)

type soundCloudEndpoint struct {
	decodedAs
	fetch soundCloudFetch
}

type soundCloudFetch func(soundCloud *soundCloudSession, ctx context.Context, id string) (json.RawMessage, error)

var soundCloudEndpoints = map[string]soundCloudEndpoint{
	"search_track":   {"scSearchResponse", soundCloudAPI(soundCloudSearch("tracks", "&offset=0"))},
	"search_album":   {"scFetchList.body[scAPIAlbum]", soundCloudAPI(soundCloudSearch("albums", ""))},
	"search_user":    {"scFetchList.body[scAPIUser]", soundCloudAPI(soundCloudSearch("users", ""))},
	"resolve":        {"scAPIUser", soundCloudAPI((*soundCloudSession).resolveURL)},
	"user_toptracks": {"scFetchList.body[scAPITrack]", soundCloudUserList("toptracks")},
	"user_albums":    {"fetchArtistPlaylists.body", soundCloudUserList("albums")},
	"user_tracks":    {"fetchArtistStandaloneSingles.body", soundCloudUserList("tracks")},
	"playlist":       {"fetchPlaylistTracks.pl", soundCloudAPI(soundCloudItem("playlists", "", ""))},
	"track":          {"scAPITrack", soundCloudAPI(soundCloudItem("tracks", "", ""))},
	"track_related":  {"scFetchList.body[scAPITrack]", soundCloudAPI(soundCloudItem("tracks", "/related", "&limit=20"))},
	"ytdlp_search":   {"Search.entry", (*soundCloudSession).ytdlpSearch},
}

func soundCloudProvider() provider {
	return provider{
		adapter:   adapterRoot + "soundcloud",
		decodedAs: decodedTypes(soundCloudEndpoints),
		source: func(client *http.Client) (source, error) {
			return newSoundCloudSource(client, soundCloudSiteURL, soundCloudAPIURL, execYtdlp), nil
		},
	}
}

type soundCloudSession struct {
	client   *http.Client
	site     string
	api      string
	ytdlp    ytdlpRunner
	clientID string
}

func newSoundCloudSource(client *http.Client, site, api string, ytdlp ytdlpRunner) liveSource {
	soundCloud := &soundCloudSession{client: client, site: site, api: api, ytdlp: ytdlp}
	fetchers := liveSource{}
	for entity, endpoint := range soundCloudEndpoints {
		fetchers[entity] = func(ctx context.Context, id string) (json.RawMessage, error) {
			return endpoint.fetch(soundCloud, ctx, id)
		}
	}
	return fetchers
}

type soundCloudURL func(soundCloud *soundCloudSession, id, clientID string) string

func soundCloudSearch(kind, tail string) soundCloudURL {
	return func(soundCloud *soundCloudSession, query, clientID string) string {
		return fmt.Sprintf("%s/search/%s?q=%s&client_id=%s&limit=20%s",
			soundCloud.api, kind, url.QueryEscape(query), url.QueryEscape(clientID), tail)
	}
}

func soundCloudItem(kind, sub, tail string) soundCloudURL {
	return func(soundCloud *soundCloudSession, id, clientID string) string {
		return fmt.Sprintf("%s/%s/%s%s?client_id=%s%s",
			soundCloud.api, kind, url.PathEscape(id), sub, url.QueryEscape(clientID), tail)
	}
}

func (soundCloud *soundCloudSession) resolveURL(ref, clientID string) string {
	permalink := ref
	if !strings.HasPrefix(permalink, "http") {
		permalink = soundCloud.site + ref
	}
	return fmt.Sprintf("%s/resolve?url=%s&client_id=%s", soundCloud.api, url.QueryEscape(permalink), url.QueryEscape(clientID))
}

func soundCloudAPI(build soundCloudURL) soundCloudFetch {
	return func(soundCloud *soundCloudSession, ctx context.Context, id string) (json.RawMessage, error) {
		clientID, err := soundCloud.ensureClientID(ctx)
		if err != nil {
			return nil, err
		}
		return soundCloud.get(ctx, build(soundCloud, id, clientID))
	}
}

func soundCloudUserList(list string) soundCloudFetch {
	return func(soundCloud *soundCloudSession, ctx context.Context, ref string) (json.RawMessage, error) {
		userID, err := soundCloud.userID(ctx, ref)
		if err != nil {
			return nil, err
		}
		return soundCloudAPI(soundCloudItem("users", "/"+list, "&limit=50"))(soundCloud, ctx, userID)
	}
}

func (soundCloud *soundCloudSession) userID(ctx context.Context, ref string) (string, error) {
	if soundCloudDigits.MatchString(ref) {
		return ref, nil
	}
	raw, err := soundCloudAPI((*soundCloudSession).resolveURL)(soundCloud, ctx, ref)
	if err != nil {
		return "", err
	}
	var user struct {
		ID int64 `json:"id"`
	}
	if err := json.Unmarshal(raw, &user); err != nil || user.ID == 0 {
		return "", fmt.Errorf("soundcloud resolve %q did not yield a user", ref)
	}
	return strconv.FormatInt(user.ID, 10), nil
}

func (soundCloud *soundCloudSession) get(ctx context.Context, rawURL string) ([]byte, error) {
	return fetch(ctx, soundCloud.client, request{method: http.MethodGet, url: rawURL, header: map[string]string{"User-Agent": soundCloudUserAgent}})
}

func (soundCloud *soundCloudSession) ensureClientID(ctx context.Context) (string, error) {
	if soundCloud.clientID != "" {
		return soundCloud.clientID, nil
	}
	page, err := soundCloud.get(ctx, soundCloud.site)
	if err != nil {
		return "", fmt.Errorf("soundcloud home: %w", err)
	}
	soundCloud.clientID, err = soundCloud.scrapeClientID(ctx, firstOccurrences(soundCloudAsset.FindAllString(string(page), -1)))
	return soundCloud.clientID, err
}

func (soundCloud *soundCloudSession) scrapeClientID(ctx context.Context, assets []string) (string, error) {
	for _, asset := range slices.Backward(assets) {
		bundle, err := soundCloud.get(ctx, asset)
		if err != nil {
			continue
		}
		if m := soundCloudClientID.FindSubmatch(bundle); m != nil {
			return string(m[1]), nil
		}
	}
	return "", errors.New("no client_id in any soundcloud asset bundle")
}

func firstOccurrences(in []string) []string {
	var out []string
	for _, s := range in {
		if !slices.Contains(out, s) {
			out = append(out, s)
		}
	}
	return out
}
