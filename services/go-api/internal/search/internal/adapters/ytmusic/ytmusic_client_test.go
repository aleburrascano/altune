package ytmusic_test

import (
	"altune/go-api/internal/search"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

type jsonObj = map[string]any

func ytmRun(text string) jsonObj { return jsonObj{"text": text} }

func ytmBrowseRun(text, browseID, pageType string) jsonObj {
	return jsonObj{"text": text, "navigationEndpoint": ytmBrowse(browseID, pageType)}
}

func ytmBrowse(browseID, pageType string) jsonObj {
	return jsonObj{"browseEndpoint": jsonObj{
		"browseId":                              browseID,
		"browseEndpointContextSupportedConfigs": jsonObj{"browseEndpointContextMusicConfig": jsonObj{"pageType": pageType}},
	}}
}

func ytmThumbs(urls ...string) jsonObj {
	thumbs := make([]any, 0, len(urls))
	for _, u := range urls {
		thumbs = append(thumbs, jsonObj{"url": u})
	}
	return jsonObj{"musicThumbnailRenderer": jsonObj{"thumbnail": jsonObj{"thumbnails": thumbs}}}
}

func ytmColumns(titleRun jsonObj, byline []any) []any {
	columns := []any{jsonObj{"musicResponsiveListItemFlexColumnRenderer": jsonObj{"text": jsonObj{"runs": []any{titleRun}}}}}
	if byline != nil {
		columns = append(columns, jsonObj{"musicResponsiveListItemFlexColumnRenderer": jsonObj{"text": jsonObj{"runs": byline}}})
	}
	return columns
}

func ytmPlayable(videoType, title, videoID string, byline []any, extra jsonObj) jsonObj {
	item := jsonObj{
		"overlay": jsonObj{"musicItemThumbnailOverlayRenderer": jsonObj{"content": jsonObj{"musicPlayButtonRenderer": jsonObj{
			"playNavigationEndpoint": jsonObj{"watchEndpoint": jsonObj{
				"watchEndpointMusicSupportedConfigs": jsonObj{"watchEndpointMusicConfig": jsonObj{"musicVideoType": videoType}},
			}},
		}}}},
		"flexColumns": ytmColumns(jsonObj{"text": title, "navigationEndpoint": jsonObj{"watchEndpoint": jsonObj{"videoId": videoID}}}, byline),
	}
	for k, v := range extra {
		item[k] = v
	}
	return jsonObj{"musicResponsiveListItemRenderer": item}
}

func ytmArtist(name, browseID string, thumbURLs ...string) jsonObj {
	item := jsonObj{
		"navigationEndpoint": ytmBrowse(browseID, "MUSIC_PAGE_TYPE_ARTIST"),
		"flexColumns":        ytmColumns(ytmRun(name), nil),
	}
	if len(thumbURLs) > 0 {
		item["thumbnail"] = ytmThumbs(thumbURLs...)
	}
	return jsonObj{"musicResponsiveListItemRenderer": item}
}

func ytmShelfPage(t *testing.T, items ...any) []byte {
	t.Helper()
	page := jsonObj{"contents": jsonObj{"tabbedSearchResultsRenderer": jsonObj{"tabs": []any{
		jsonObj{"tabRenderer": jsonObj{"content": jsonObj{"sectionListRenderer": jsonObj{"contents": []any{
			jsonObj{"musicShelfRenderer": jsonObj{"contents": items}},
		}}}}},
	}}}}
	body, err := json.Marshal(page)
	if err != nil {
		t.Fatalf("marshal page: %v", err)
	}
	return body
}

func servePage(t *testing.T, body []byte) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(body)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func searchTrack(t *testing.T, item any) search.SearchResult {
	t.Helper()
	results := searchTracks(t, item)
	if len(results) != 1 {
		t.Fatalf("Search = %d results, want exactly the one item", len(results))
	}
	return results[0]
}

func TestYouTubeMusicAdapter_Search_DurationIsTheTrailingBylineRun(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want int
	}{
		{"minutes seconds", "4:20", 260},
		{"hours", "1:02:03", 3723},
		{"empty", "", 0},
		{"view count not duration", "1.2M views", 0},
		{"zero", "0:00", 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			byline := []any{ytmBrowseRun("Artist", "UCa", "MUSIC_PAGE_TYPE_ARTIST"), ytmRun(" • "), ytmRun(tt.in)}
			got := searchTrack(t, ytmPlayable("MUSIC_VIDEO_TYPE_ATV", "Song", "v1", byline, nil))
			if got.Duration != tt.want {
				t.Errorf("duration run %q = %d, want %d", tt.in, got.Duration, tt.want)
			}
		})
	}
}

func TestYouTubeMusicAdapter_Search_ExplicitBadgeAfterAnotherBadge(t *testing.T) {
	badge := func(icon string) jsonObj {
		return jsonObj{"musicInlineBadgeRenderer": jsonObj{"icon": jsonObj{"iconType": icon}}}
	}
	got := searchTrack(t, ytmPlayable("MUSIC_VIDEO_TYPE_ATV", "Song", "v1", nil,
		jsonObj{"badges": []any{badge("MUSIC_LIVE_BADGE"), badge("MUSIC_EXPLICIT_BADGE")}}))

	if got.Explicit == nil || !*got.Explicit {
		t.Errorf("explicit = %v, want true for a track with the explicit badge second", got.Explicit)
	}
}

func TestYouTubeMusicAdapter_Search_MapsAMusicVideo(t *testing.T) {
	byline := []any{ytmBrowseRun("Underground Artist", "UCu", "MUSIC_PAGE_TYPE_ARTIST"), ytmRun(" • "), ytmRun("3:20")}
	got := searchTrack(t, ytmPlayable("MUSIC_VIDEO_TYPE_OMV", "Obscure Track", "abc123", byline,
		jsonObj{"thumbnail": ytmThumbs("https://img/small", "https://img/large")}))

	if got.Kind != search.ResultKindTrack {
		t.Errorf("Kind = %v, want track", got.Kind)
	}
	if got.Title != "Obscure Track" {
		t.Errorf("Title = %q, want %q", got.Title, "Obscure Track")
	}
	if got.Subtitle != "Underground Artist" {
		t.Errorf("Subtitle = %q, want %q", got.Subtitle, "Underground Artist")
	}
	if got.ImageURL != "https://img/large" {
		t.Errorf("ImageURL = %q, want the largest thumbnail", got.ImageURL)
	}
	if len(got.Sources) != 1 {
		t.Fatalf("Sources = %d, want 1", len(got.Sources))
	}
	src := got.Sources[0]
	if src.Provider != search.ProviderYouTube || src.ExternalID != "abc123" {
		t.Errorf("source = %+v, want youtube/abc123", src)
	}
	if src.URL != "https://music.youtube.com/watch?v=abc123" {
		t.Errorf("source URL = %q", src.URL)
	}
	if got.Duration != 200 {
		t.Errorf("Duration = %d, want 200", got.Duration)
	}
}

func TestYouTubeMusicAdapter_Search_FallsBackToThePlainThirdBylineRun(t *testing.T) {
	tests := []struct {
		name   string
		byline []any
		want   string
	}{
		{"plain third run", []any{ytmRun("Song"), ytmRun(" • "), ytmRun("Plain Artist")}, "Plain Artist"},
		{"short runs", []any{ytmRun("Song"), ytmRun(" • ")}, ""},
		{"bare divider third run", []any{ytmRun("Song"), ytmRun(" • "), ytmRun(" • ")}, ""},
		{"no byline column", nil, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := searchTrack(t, ytmPlayable("MUSIC_VIDEO_TYPE_ATV", "Song", "v1", tt.byline, nil))
			if got.Subtitle != tt.want {
				t.Errorf("Subtitle = %q, want %q", got.Subtitle, tt.want)
			}
			if got.Duration != 0 {
				t.Errorf("Duration = %d, want 0 without a duration run", got.Duration)
			}
		})
	}
}
