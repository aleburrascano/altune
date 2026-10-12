package ytmsearch

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

type VideoType string

const (
	VideoTypeATV VideoType = "MUSIC_VIDEO_TYPE_ATV"
	VideoTypeOMV VideoType = "MUSIC_VIDEO_TYPE_OMV"
	VideoTypeUGC VideoType = "MUSIC_VIDEO_TYPE_UGC"
)

const (
	explicitBadgeIcon      = "MUSIC_EXPLICIT_BADGE"
	artistPageType         = "MUSIC_PAGE_TYPE_ARTIST"
	userChannelPageType    = "MUSIC_PAGE_TYPE_USER_CHANNEL"
	bylineSeparatorRunText = " • "
)

var (
	ErrNoWatchTrack    = errors.New("ytmsearch: next response has no track")
	ErrUnexpectedShape = errors.New("ytmsearch: response is not a JSON object")
)

type Watch struct {
	VideoID  string
	Title    string
	Artists  []string
	Type     VideoType
	Explicit bool
}

type Song struct {
	VideoID  string
	Title    string
	Artists  []string
	Type     VideoType
	Explicit bool
}

type OEmbed struct {
	Title  string
	Author string
}

type node struct{ value any }

func (n node) at(path ...any) node {
	cur := n.value
	for _, step := range path {
		switch key := step.(type) {
		case string:
			object, ok := cur.(map[string]any)
			if !ok {
				return node{}
			}
			cur = object[key]
		case int:
			array, ok := cur.([]any)
			if !ok || key >= len(array) {
				return node{}
			}
			cur = array[key]
		}
	}
	return node{cur}
}

func (n node) text() string {
	s, _ := n.value.(string)
	return s
}

func (n node) items() []node {
	array, _ := n.value.([]any)
	nodes := make([]node, len(array))
	for i, v := range array {
		nodes[i] = node{v}
	}
	return nodes
}

func decodeObject(raw []byte) (node, error) {
	var page any
	if err := json.Unmarshal(raw, &page); err != nil {
		return node{}, fmt.Errorf("ytmsearch: decode: %w", err)
	}
	if _, ok := page.(map[string]any); !ok {
		return node{}, ErrUnexpectedShape
	}
	return node{page}, nil
}

func ParseWatch(raw []byte) (Watch, error) {
	page, err := decodeObject(raw)
	if err != nil {
		return Watch{}, err
	}
	panel := page.at("contents", "singleColumnMusicWatchNextResultsRenderer", "tabbedRenderer",
		"watchNextTabbedResultsRenderer", "tabs", 0, "tabRenderer", "content", "musicQueueRenderer",
		"content", "playlistPanelRenderer", "contents")
	for _, entry := range panel.items() {
		if video := entry.at("playlistPanelVideoRenderer"); video.at("videoId").text() != "" {
			return watchFromPanelVideo(video), nil
		}
	}
	return Watch{}, ErrNoWatchTrack
}

func watchFromPanelVideo(video node) Watch {
	return Watch{
		VideoID:  video.at("videoId").text(),
		Title:    joinedRuns(video.at("title", "runs")),
		Artists:  bylineArtists(video.at("longBylineText", "runs").items()),
		Type:     VideoType(video.at("navigationEndpoint", "watchEndpoint", "watchEndpointMusicSupportedConfigs", "watchEndpointMusicConfig", "musicVideoType").text()),
		Explicit: hasExplicitBadge(video.at("badges")),
	}
}

func ParseSongs(raw []byte) ([]Song, error) {
	page, err := decodeObject(raw)
	if err != nil {
		return nil, err
	}
	sections := page.at("contents", "tabbedSearchResultsRenderer", "tabs", 0, "tabRenderer",
		"content", "sectionListRenderer", "contents").items()
	songs := []Song{}
	for _, section := range sections {
		for _, entry := range section.at("musicShelfRenderer", "contents").items() {
			if song, ok := songFromListItem(entry.at("musicResponsiveListItemRenderer")); ok {
				songs = append(songs, song)
			}
		}
	}
	return songs, nil
}

func songFromListItem(item node) (Song, bool) {
	play := item.at("overlay", "musicItemThumbnailOverlayRenderer", "content", "musicPlayButtonRenderer",
		"playNavigationEndpoint", "watchEndpoint")
	videoID := play.at("videoId").text()
	if videoID == "" {
		return Song{}, false
	}
	return Song{
		VideoID:  videoID,
		Title:    joinedRuns(item.at("flexColumns", 0, "musicResponsiveListItemFlexColumnRenderer", "text", "runs")),
		Artists:  bylineArtists(item.at("flexColumns", 1, "musicResponsiveListItemFlexColumnRenderer", "text", "runs").items()),
		Type:     VideoType(play.at("watchEndpointMusicSupportedConfigs", "watchEndpointMusicConfig", "musicVideoType").text()),
		Explicit: hasExplicitBadge(item.at("badges")),
	}, true
}

func ParseOEmbed(raw []byte) (OEmbed, error) {
	var body struct {
		Title  string `json:"title"`
		Author string `json:"author_name"`
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		return OEmbed{}, fmt.Errorf("ytmsearch: decode oembed: %w", err)
	}
	return OEmbed{Title: body.Title, Author: body.Author}, nil
}

func joinedRuns(runs node) string {
	var b strings.Builder
	for _, run := range runs.items() {
		b.WriteString(run.at("text").text())
	}
	return b.String()
}

func bylineArtists(runs []node) []string {
	var artists []string
	for _, run := range runs {
		pageType := run.at("navigationEndpoint", "browseEndpoint", "browseEndpointContextSupportedConfigs",
			"browseEndpointContextMusicConfig", "pageType").text()
		if pageType == artistPageType || pageType == userChannelPageType {
			artists = append(artists, run.at("text").text())
		}
	}
	if len(artists) == 0 && len(runs) > 0 {
		if first := runs[0].at("text").text(); first != "" && first != bylineSeparatorRunText {
			artists = []string{first}
		}
	}
	return artists
}

func hasExplicitBadge(badges node) bool {
	for _, badge := range badges.items() {
		if badge.at("musicInlineBadgeRenderer", "icon", "iconType").text() == explicitBadgeIcon {
			return true
		}
	}
	return false
}
