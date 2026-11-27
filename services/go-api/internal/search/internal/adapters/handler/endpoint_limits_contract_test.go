package handler_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"testing"
)

func TestEndpointLimits_TheServerRejectsJustAboveEachMaximumTheClientContractNames(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "..", "..", "..", "contracts", "wire", "endpoint_limits.json"))
	if err != nil {
		t.Fatalf("read the endpoint limits contract: %v", err)
	}
	var limits map[string]struct {
		Max *int `json:"max"`
	}
	if err := json.Unmarshal(raw, &limits); err != nil {
		t.Fatalf("parse the endpoint limits contract: %v", err)
	}
	paths := map[string]string{
		"album_tracks":          "/discovery/albums/deezer/1/tracks?limit=%d",
		"artist_albums":         "/discovery/artists/deezer/1/albums?limit=%d",
		"artist_content_albums": "/discovery/artists/deezer/1/content?albums_limit=%d",
		"artist_content_tracks": "/discovery/artists/deezer/1/content?tracks_limit=%d",
		"related_tracks":        "/discovery/tracks/deezer/1/related?limit=%d",
		"search_history":        "/discovery/search-history?limit=%d",
		"suggest":               "/discovery/suggest?q=x&limit=%d",
		"top_tracks":            "/discovery/artists/deezer/1/top-tracks?limit=%d",
		"search":                "/discovery/search?q=x&limit=%d",
	}
	checked := 0
	for name, limit := range limits {
		pattern, served := paths[name]
		if limit.Max == nil || !served {
			continue
		}
		checked++
		t.Run(name, func(t *testing.T) {
			router := buildSuggestRouter(t, &fakeVocabStore{})

			atMax := discServe(t, router, http.MethodGet, fmt.Sprintf(pattern, *limit.Max), nil)
			aboveMax := discServe(t, router, http.MethodGet, fmt.Sprintf(pattern, *limit.Max+1), nil)

			if atMax.Code == http.StatusBadRequest {
				t.Errorf("limit %d, the contract maximum: status = 400 (%s), want it accepted", *limit.Max, atMax.Body)
			}
			if aboveMax.Code != http.StatusBadRequest {
				t.Errorf("limit %d, one above the contract maximum: status = %d, want 400", *limit.Max+1, aboveMax.Code)
			}
		})
	}
	if checked != len(paths) {
		t.Errorf("checked %d endpoints, want all %d with a path here; a contract entry or a path went missing", checked, len(paths))
	}
}
