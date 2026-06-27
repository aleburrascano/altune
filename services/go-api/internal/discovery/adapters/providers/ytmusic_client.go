package providers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// This file is a small, self-owned YouTube Music client + response parser. It
// replaces github.com/raitonoberu/ytmusic, whose parser silently returned zero
// results after YouTube Music restructured its search response (the unfiltered
// search moved from a single `musicShelfRenderer` to a `musicCardShelfRenderer`
// "top result" card plus many `itemSectionRenderer` sections; the library only
// knew `musicShelfRenderer`). The request itself never broke — the keyless
// internal endpoint, the static WEB_REMIX context, and the public key all still
// work — so we keep the proven request and own the parse, which is the part that
// drifts. Categorisation is done per item (by pageType / musicVideoType), which
// handles the new card+section layout and the old single-shelf layout (still
// used by filtered searches) uniformly.
//
// AIDEV-NOTE: when YouTube drifts again and results go empty, probe the raw
// endpoint (POST music.youtube.com/youtubei/v1/search) and re-inspect the
// renderer containers under sectionListRenderer.contents — the item-level field
// paths below are stable; the outer container names are what change.

const (
	ytmEndpoint      = "https://music.youtube.com/youtubei/v1/search"
	ytmClientName    = "WEB_REMIX"
	ytmClientVersion = "1.20220715.04.00"
	// ytmSearchKey is the public innertube key the YouTube Music web client ships
	// with — not a secret, not account-scoped.
	ytmSearchKey = "AIzaSyC9XL3ZjWddXya6X74dJoCTL-WEYFDNX30"
)

// ytmFilter is the opaque `params` value that scopes a search to one category.
// Empty means an unfiltered ("everything") search. Values credited to
// github.com/sigma67/ytmusicapi via the raitonoberu/ytmusic library.
type ytmFilter string

const (
	ytmNoFilter     ytmFilter = ""
	ytmTrackFilter  ytmFilter = "EgWKAQIIAWoMEA4QChADEAQQCRAF"
	ytmAlbumFilter  ytmFilter = "EgWKAQIYAWoMEA4QChADEAQQCRAF"
	ytmArtistFilter ytmFilter = "EgWKAQIgAWoMEA4QChADEAQQCRAF"
)

// ytm result types — deliberately parallel to the shapes the adapter's
// map* functions consume, so mapping logic stays unchanged.

type ytmThumbnail struct {
	URL    string
	Width  int
	Height int
}

type ytmArtistRef struct {
	Name string
	ID   string
}

type ytmAlbumRef struct {
	Name string
	ID   string
}

type ytmTrack struct {
	VideoID    string
	Title      string
	Artists    []ytmArtistRef
	Album      ytmAlbumRef
	Duration   int
	IsExplicit bool
	Thumbnails []ytmThumbnail
}

type ytmVideo struct {
	VideoID    string
	Title      string
	Artists    []ytmArtistRef
	Duration   int
	Thumbnails []ytmThumbnail
}

type ytmAlbum struct {
	BrowseID   string
	Title      string
	Type       string
	Artists    []ytmArtistRef
	Year       string
	IsExplicit bool
	Thumbnails []ytmThumbnail
}

type ytmArtistItem struct {
	BrowseID   string
	Artist     string
	Thumbnails []ytmThumbnail
}

type ytmResult struct {
	Tracks  []*ytmTrack
	Videos  []*ytmVideo
	Albums  []*ytmAlbum
	Artists []*ytmArtistItem
}

// ytmSearch performs one YouTube Music search and parses the response. It uses
// the request context directly (the library ignored context, forcing a goroutine
// bridge), so cancellation and the fan-out deadline are honoured natively.
func ytmSearch(ctx context.Context, client *http.Client, query string, filter ytmFilter) (*ytmResult, error) {
	body := map[string]any{
		"context": map[string]any{
			"client": map[string]any{
				"clientName":    ytmClientName,
				"clientVersion": ytmClientVersion,
				"hl":            "en",
				"gl":            "US",
			},
			"user": map[string]any{"lockedSafetyMode": false},
		},
		"query": query,
	}
	if filter != ytmNoFilter {
		body["params"] = string(filter)
	}

	payload, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("ytmusic marshal: %w", err)
	}

	params := url.Values{}
	params.Add("key", ytmSearchKey)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, ytmEndpoint+"?"+params.Encode(), bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("ytmusic request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json; charset=utf-8")
	req.Header.Set("Referer", "https://music.youtube.com/search")
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/70.0.3538.77 Safari/537.36")

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("ytmusic do: %w", err)
	}
	defer resp.Body.Close()

	// A rate-limit (HTTP 403) returns an HTML body that is not valid JSON; surface
	// it as an error so the adapter's retry can fire.
	var page any
	if err := json.NewDecoder(resp.Body).Decode(&page); err != nil {
		return nil, fmt.Errorf("ytmusic decode (status %d): %w", resp.StatusCode, err)
	}

	return parseYTMSearch(page), nil
}

// ytmPath navigates a decoded JSON tree by a sequence of string keys and int
// indices, returning nil on any miss. JSON of this depth and dynamism is the one
// place `any` navigation beats typed structs.
type ytmPath []any

func ytmGet(source any, path ytmPath) any {
	cur := source
	for _, step := range path {
		switch key := step.(type) {
		case string:
			m, ok := cur.(map[string]any)
			if !ok {
				return nil
			}
			cur = m[key]
		case int:
			arr, ok := cur.([]any)
			if !ok || key >= len(arr) {
				return nil
			}
			cur = arr[key]
		}
		if cur == nil {
			return nil
		}
	}
	return cur
}

func ytmStr(source any, path ytmPath) string {
	if s, ok := ytmGet(source, path).(string); ok {
		return s
	}
	return ""
}

func ytmList(v any) []any {
	if arr, ok := v.([]any); ok {
		return arr
	}
	return nil
}

// parseYTMSearch walks sectionListRenderer.contents and collects results from
// whichever container wraps them: musicShelfRenderer (filtered/legacy),
// itemSectionRenderer (new per-item sections), or musicCardShelfRenderer (the
// "top result" card — its header is itself a result, its contents are items).
func parseYTMSearch(page any) *ytmResult {
	res := &ytmResult{}
	sections := ytmList(ytmGet(page, ytmPath{
		"contents", "tabbedSearchResultsRenderer", "tabs", 0, "tabRenderer",
		"content", "sectionListRenderer", "contents",
	}))

	for _, sec := range sections {
		if card := ytmGet(sec, ytmPath{"musicCardShelfRenderer"}); card != nil {
			res.addCardHeader(card)
			for _, it := range ytmList(ytmGet(card, ytmPath{"contents"})) {
				res.addItem(it)
			}
			continue
		}
		var items any
		switch {
		case ytmGet(sec, ytmPath{"musicShelfRenderer", "contents"}) != nil:
			items = ytmGet(sec, ytmPath{"musicShelfRenderer", "contents"})
		case ytmGet(sec, ytmPath{"itemSectionRenderer", "contents"}) != nil:
			items = ytmGet(sec, ytmPath{"itemSectionRenderer", "contents"})
		}
		for _, it := range ytmList(items) {
			res.addItem(it)
		}
	}
	return res
}

// addCardHeader emits the top-result card's primary entity (artist or album)
// when its title run navigates to one. Track/song cards are covered by the
// card's contents items, so only artist/album headers are emitted here.
func (r *ytmResult) addCardHeader(card any) {
	pageType := ytmStr(card, ytmPath{"title", "runs", 0, "navigationEndpoint", "browseEndpoint", "browseEndpointContextSupportedConfigs", "browseEndpointContextMusicConfig", "pageType"})
	title := ytmStr(card, ytmPath{"title", "runs", 0, "text"})
	browseID := ytmStr(card, ytmPath{"title", "runs", 0, "navigationEndpoint", "browseEndpoint", "browseId"})
	thumbs := parseYTMThumbnails(ytmGet(card, ytmPath{"thumbnail", "musicThumbnailRenderer", "thumbnail", "thumbnails"}))
	switch pageType {
	case "MUSIC_PAGE_TYPE_ARTIST":
		r.Artists = append(r.Artists, &ytmArtistItem{BrowseID: browseID, Artist: title, Thumbnails: thumbs})
	case "MUSIC_PAGE_TYPE_ALBUM":
		r.Albums = append(r.Albums, &ytmAlbum{BrowseID: browseID, Title: title, Thumbnails: thumbs})
	}
}

// addItem categorises one musicResponsiveListItemRenderer by its own pageType
// (artist/album navigation) or musicVideoType (ATV track / OMV-UGC video).
// Playlists, podcasts, and topic user-channels are intentionally ignored.
func (r *ytmResult) addItem(item any) {
	mr := ytmGet(item, ytmPath{"musicResponsiveListItemRenderer"})
	if mr == nil {
		return
	}
	if pageType := ytmStr(mr, ytmPath{"navigationEndpoint", "browseEndpoint", "browseEndpointContextSupportedConfigs", "browseEndpointContextMusicConfig", "pageType"}); pageType != "" {
		switch pageType {
		case "MUSIC_PAGE_TYPE_ARTIST":
			r.Artists = append(r.Artists, parseYTMArtist(mr))
		case "MUSIC_PAGE_TYPE_ALBUM":
			r.Albums = append(r.Albums, parseYTMAlbum(mr))
		}
		return
	}
	switch ytmStr(mr, ytmPath{"overlay", "musicItemThumbnailOverlayRenderer", "content", "musicPlayButtonRenderer", "playNavigationEndpoint", "watchEndpoint", "watchEndpointMusicSupportedConfigs", "watchEndpointMusicConfig", "musicVideoType"}) {
	case "MUSIC_VIDEO_TYPE_ATV":
		r.Tracks = append(r.Tracks, parseYTMTrack(mr))
	case "MUSIC_VIDEO_TYPE_OMV", "MUSIC_VIDEO_TYPE_UGC":
		r.Videos = append(r.Videos, parseYTMVideo(mr))
	}
}

func parseYTMTrack(mr any) *ytmTrack {
	t := &ytmTrack{}
	info1 := ytmGet(mr, ytmPath{"flexColumns", 0, "musicResponsiveListItemFlexColumnRenderer", "text", "runs", 0})
	t.Title = ytmStr(info1, ytmPath{"text"})
	t.VideoID = ytmStr(info1, ytmPath{"navigationEndpoint", "watchEndpoint", "videoId"})

	info2 := ytmList(ytmGet(mr, ytmPath{"flexColumns", 1, "musicResponsiveListItemFlexColumnRenderer", "text", "runs"}))
	t.Artists, t.Album = parseYTMByline(info2)
	t.Duration = parseYTMTrailingDuration(info2)
	if len(t.Artists) == 0 {
		t.Artists = fallbackByline(info2)
	}
	t.IsExplicit = ytmHasExplicitBadge(mr)
	t.Thumbnails = parseYTMThumbnails(ytmGet(mr, ytmPath{"thumbnail", "musicThumbnailRenderer", "thumbnail", "thumbnails"}))
	return t
}

func parseYTMVideo(mr any) *ytmVideo {
	v := &ytmVideo{}
	info1 := ytmGet(mr, ytmPath{"flexColumns", 0, "musicResponsiveListItemFlexColumnRenderer", "text", "runs", 0})
	v.Title = ytmStr(info1, ytmPath{"text"})
	v.VideoID = ytmStr(info1, ytmPath{"navigationEndpoint", "watchEndpoint", "videoId"})

	info2 := ytmList(ytmGet(mr, ytmPath{"flexColumns", 1, "musicResponsiveListItemFlexColumnRenderer", "text", "runs"}))
	v.Artists, _ = parseYTMByline(info2)
	v.Duration = parseYTMTrailingDuration(info2)
	v.Thumbnails = parseYTMThumbnails(ytmGet(mr, ytmPath{"thumbnail", "musicThumbnailRenderer", "thumbnail", "thumbnails"}))
	return v
}

func parseYTMAlbum(mr any) *ytmAlbum {
	a := &ytmAlbum{}
	a.Title = ytmStr(mr, ytmPath{"flexColumns", 0, "musicResponsiveListItemFlexColumnRenderer", "text", "runs", 0, "text"})
	a.BrowseID = ytmStr(mr, ytmPath{"navigationEndpoint", "browseEndpoint", "browseId"})

	runs := ytmList(ytmGet(mr, ytmPath{"flexColumns", 1, "musicResponsiveListItemFlexColumnRenderer", "text", "runs"}))
	if len(runs) > 0 {
		a.Type = ytmStr(runs[0], ytmPath{"text"})
		a.Artists, _ = parseYTMByline(runs)
		if len(a.Artists) == 0 && len(runs) > 2 {
			if name := ytmStr(runs[2], ytmPath{"text"}); name != "" {
				a.Artists = []ytmArtistRef{{Name: name}}
			}
		}
		a.Year = ytmStr(runs[len(runs)-1], ytmPath{"text"})
	}
	a.IsExplicit = ytmHasExplicitBadge(mr)
	a.Thumbnails = parseYTMThumbnails(ytmGet(mr, ytmPath{"thumbnail", "musicThumbnailRenderer", "thumbnail", "thumbnails"}))
	return a
}

func parseYTMArtist(mr any) *ytmArtistItem {
	return &ytmArtistItem{
		BrowseID:   ytmStr(mr, ytmPath{"navigationEndpoint", "browseEndpoint", "browseId"}),
		Artist:     ytmStr(mr, ytmPath{"flexColumns", 0, "musicResponsiveListItemFlexColumnRenderer", "text", "runs", 0, "text"}),
		Thumbnails: parseYTMThumbnails(ytmGet(mr, ytmPath{"thumbnail", "musicThumbnailRenderer", "thumbnail", "thumbnails"})),
	}
}

// parseYTMByline extracts artists (and, for tracks, the album) from the second
// flex-column runs by reading each run's browse pageType.
func parseYTMByline(runs []any) ([]ytmArtistRef, ytmAlbumRef) {
	var artists []ytmArtistRef
	var album ytmAlbumRef
	for _, run := range runs {
		pageType := ytmStr(run, ytmPath{"navigationEndpoint", "browseEndpoint", "browseEndpointContextSupportedConfigs", "browseEndpointContextMusicConfig", "pageType"})
		switch pageType {
		case "MUSIC_PAGE_TYPE_ARTIST":
			artists = append(artists, ytmArtistRef{
				Name: ytmStr(run, ytmPath{"text"}),
				ID:   ytmStr(run, ytmPath{"navigationEndpoint", "browseEndpoint", "browseId"}),
			})
		case "MUSIC_PAGE_TYPE_ALBUM":
			album = ytmAlbumRef{
				Name: ytmStr(run, ytmPath{"text"}),
				ID:   ytmStr(run, ytmPath{"navigationEndpoint", "browseEndpoint", "browseId"}),
			}
		}
	}
	return artists, album
}

// fallbackByline handles rows whose artist run carries no browse link: the
// third run (index 2) is the plain artist name, sometimes a bare " • " divider.
func fallbackByline(runs []any) []ytmArtistRef {
	if len(runs) <= 2 {
		return nil
	}
	name := ytmStr(runs[2], ytmPath{"text"})
	if name == "" || name == " • " {
		return nil
	}
	return []ytmArtistRef{{Name: name}}
}

func parseYTMTrailingDuration(runs []any) int {
	if len(runs) == 0 {
		return 0
	}
	return ytmDurationToSeconds(ytmStr(runs[len(runs)-1], ytmPath{"text"}))
}

func ytmHasExplicitBadge(mr any) bool {
	return ytmStr(mr, ytmPath{"badges", 0, "musicInlineBadgeRenderer", "icon", "iconType"}) == "MUSIC_EXPLICIT_BADGE"
}

func parseYTMThumbnails(v any) []ytmThumbnail {
	arr := ytmList(v)
	if len(arr) == 0 {
		return nil
	}
	thumbs := make([]ytmThumbnail, 0, len(arr))
	for _, t := range arr {
		thumb := ytmThumbnail{URL: ytmStr(t, ytmPath{"url"})}
		if w, ok := ytmGet(t, ytmPath{"width"}).(float64); ok {
			thumb.Width = int(w)
		}
		if h, ok := ytmGet(t, ytmPath{"height"}).(float64); ok {
			thumb.Height = int(h)
		}
		thumbs = append(thumbs, thumb)
	}
	return thumbs
}

// ytmDurationToSeconds converts a "4:20" duration to seconds (260). Non-duration
// text (e.g. a view count) yields 0.
func ytmDurationToSeconds(duration string) int {
	if duration == "" || !strings.Contains(duration, ":") {
		return 0
	}
	parts := strings.Split(duration, ":")
	total := 0
	for i, p := range parts {
		n, err := strconv.Atoi(strings.TrimSpace(p))
		if err != nil {
			return 0
		}
		total += n * int(math.Pow(60, float64(len(parts)-i-1)))
	}
	return total
}
