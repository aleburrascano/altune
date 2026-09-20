package providers

import (
	"altune/go-api/internal/discovery/domain"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func mbReleaseGroupJSON(rgID, title, artistName, artistMBID string) string {
	return `{
		"id": "` + rgID + `",
		"title": "` + title + `",
		"artist-credit": [{"name": "` + artistName + `", "artist": {"id": "` + artistMBID + `", "name": "` + artistName + `"}}]
	}`
}

func TestMusicBrainzAdapter_Search_Recordings(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/ws/2/recording") {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{
			"recordings": [{
				"id": "abc-123-def",
				"title": "Paranoid Android",
				"isrcs": ["GBAYE9700011"],
				"artist-credit": [{"name": "Radiohead"}]
			}]
		}`))
	}))
	defer server.Close()

	adapter := NewMusicBrainzAdapter(newTestClient(server.URL), "altune-test/1.0")
	results, err := adapter.Search(context.Background(), "paranoid android", map[domain.ResultKind]bool{
		domain.ResultKindTrack: true,
	})
	if err != nil {
		t.Fatalf("Search returned error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}

	r := results[0]
	if r.Kind != domain.ResultKindTrack {
		t.Errorf("kind: got %v, want %v", r.Kind, domain.ResultKindTrack)
	}
	if r.Title != "Paranoid Android" {
		t.Errorf("title: got %q, want %q", r.Title, "Paranoid Android")
	}
	if r.Subtitle != "Radiohead" {
		t.Errorf("subtitle: got %q, want %q", r.Subtitle, "Radiohead")
	}
	if r.Confidence != domain.ConfidenceLow {
		t.Errorf("confidence: got %v, want %v", r.Confidence, domain.ConfidenceLow)
	}
	if len(r.Sources) != 1 {
		t.Fatalf("expected 1 source, got %d", len(r.Sources))
	}
	if r.Sources[0].Provider != domain.ProviderMusicBrainz {
		t.Errorf("source provider: got %v, want %v", r.Sources[0].Provider, domain.ProviderMusicBrainz)
	}
	if r.Sources[0].ExternalID != "abc-123-def" {
		t.Errorf("source externalID: got %q, want %q", r.Sources[0].ExternalID, "abc-123-def")
	}
	if r.Sources[0].URL != "https://musicbrainz.org/recording/abc-123-def" {
		t.Errorf("source URL: got %q, want musicbrainz recording URL", r.Sources[0].URL)
	}
	if r.MBID != "abc-123-def" {
		t.Errorf("MBID: got %q, want %q", r.MBID, "abc-123-def")
	}
	if r.ISRC != "GBAYE9700011" {
		t.Errorf("ISRC: got %q, want %q", r.ISRC, "GBAYE9700011")
	}
}

func TestMusicBrainzAdapter_Search_Artists(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/ws/2/artist") {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{
			"artists": [{
				"id": "a74b1b7f-71a5-4011-9441-d0b5e4122711",
				"name": "Radiohead",
				"type": "Group",
				"area": {"name": "Oxford"},
				"tags": [
					{"name": "alternative rock", "count": 15},
					{"name": "electronic", "count": 8}
				]
			}]
		}`))
	}))
	defer server.Close()

	adapter := NewMusicBrainzAdapter(newTestClient(server.URL), "altune-test/1.0")
	results, err := adapter.Search(context.Background(), "radiohead", map[domain.ResultKind]bool{
		domain.ResultKindArtist: true,
	})
	if err != nil {
		t.Fatalf("Search returned error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}

	r := results[0]
	if r.Kind != domain.ResultKindArtist {
		t.Errorf("kind: got %v, want %v", r.Kind, domain.ResultKindArtist)
	}
	if r.Title != "Radiohead" {
		t.Errorf("title: got %q, want %q", r.Title, "Radiohead")
	}
	if r.Sources[0].Provider != domain.ProviderMusicBrainz {
		t.Errorf("source provider: got %v, want %v", r.Sources[0].Provider, domain.ProviderMusicBrainz)
	}
	if r.Sources[0].URL != "https://musicbrainz.org/artist/a74b1b7f-71a5-4011-9441-d0b5e4122711" {
		t.Errorf("source URL: got %q, want musicbrainz artist URL", r.Sources[0].URL)
	}
	if r.MBID != "a74b1b7f-71a5-4011-9441-d0b5e4122711" {
		t.Errorf("MBID: got %q, want the artist MBID", r.MBID)
	}
	if r.Extras["artist_type"] != "Group" {
		t.Errorf("extras.artist_type: got %v, want %q", r.Extras["artist_type"], "Group")
	}
	if r.Extras["area"] != "Oxford" {
		t.Errorf("extras.area: got %v, want %q", r.Extras["area"], "Oxford")
	}
	if r.Extras["mb_tags"] != "alternative rock, electronic" {
		t.Errorf("extras.mb_tags: got %v, want %q", r.Extras["mb_tags"], "alternative rock, electronic")
	}
}

func TestMusicBrainzAdapter_Search_ReleaseGroups(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/ws/2/release-group") {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{
			"release-groups": [{
				"id": "rg-001-abc",
				"title": "OK Computer",
				"artist-credit": [{"name": "Radiohead"}]
			}]
		}`))
	}))
	defer server.Close()

	adapter := NewMusicBrainzAdapter(newTestClient(server.URL), "altune-test/1.0")
	results, err := adapter.Search(context.Background(), "ok computer", map[domain.ResultKind]bool{
		domain.ResultKindAlbum: true,
	})
	if err != nil {
		t.Fatalf("Search returned error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}

	r := results[0]
	if r.Kind != domain.ResultKindAlbum {
		t.Errorf("kind: got %v, want %v", r.Kind, domain.ResultKindAlbum)
	}
	if r.Title != "OK Computer" {
		t.Errorf("title: got %q, want %q", r.Title, "OK Computer")
	}
	if r.Subtitle != "Radiohead" {
		t.Errorf("subtitle: got %q, want %q", r.Subtitle, "Radiohead")
	}
	if r.Sources[0].URL != "https://musicbrainz.org/release-group/rg-001-abc" {
		t.Errorf("source URL: got %q, want musicbrainz release-group URL", r.Sources[0].URL)
	}
	if r.MBID != "rg-001-abc" {
		t.Errorf("MBID: got %q, want %q", r.MBID, "rg-001-abc")
	}
}

func TestMusicBrainzAdapter_Search_RecordingsRequestISRCs(t *testing.T) {
	var receivedQuery string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedQuery = r.URL.RawQuery
		w.Header().Set("Content-Type", "application/json")
		if strings.HasPrefix(r.URL.Path, "/ws/2/recording") {
			w.Write([]byte(`{"recordings": []}`))
		} else if strings.HasPrefix(r.URL.Path, "/ws/2/artist") {
			w.Write([]byte(`{"artists": []}`))
		} else {
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	adapter := NewMusicBrainzAdapter(newTestClient(server.URL), "altune-test/1.0")

	t.Run("recording search includes inc=isrcs", func(t *testing.T) {
		receivedQuery = ""
		adapter.Search(context.Background(), "test", map[domain.ResultKind]bool{
			domain.ResultKindTrack: true,
		})
		if !strings.Contains(receivedQuery, "inc=isrcs") {
			t.Errorf("recording search must include inc=isrcs, got query: %s", receivedQuery)
		}
	})

	t.Run("artist search omits inc=isrcs", func(t *testing.T) {
		receivedQuery = ""
		adapter.Search(context.Background(), "test", map[domain.ResultKind]bool{
			domain.ResultKindArtist: true,
		})
		if strings.Contains(receivedQuery, "inc=isrcs") {
			t.Errorf("artist search must not include inc=isrcs, got query: %s", receivedQuery)
		}
	})
}

func TestMusicBrainzAdapter_Search_HTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	adapter := NewMusicBrainzAdapter(newTestClient(server.URL), "altune-test/1.0")
	results, err := adapter.Search(context.Background(), "anything", map[domain.ResultKind]bool{
		domain.ResultKindTrack: true,
	})
	if err == nil {
		t.Fatal("expected an error when all attempted kinds fail on HTTP 500, got nil")
	}
	if len(results) != 0 {
		t.Errorf("expected 0 results on HTTP 500, got %d", len(results))
	}
}

func TestMusicBrainzAdapter_fetchReleaseGroups_paginates(t *testing.T) {
	var offsets []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/ws/2/release-group") {
			http.NotFound(w, r)
			return
		}
		offsets = append(offsets, r.URL.Query().Get("offset"))
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Query().Get("offset") == "0" {
			w.Write([]byte(`{
				"release-group-count": 3,
				"release-groups": [
					` + mbReleaseGroupJSON("rg-1", "First", "Che", "mbid-che") + `,
					` + mbReleaseGroupJSON("rg-2", "Second", "Che", "mbid-che") + `
				]
			}`))
			return
		}
		w.Write([]byte(`{
			"release-group-count": 3,
			"release-groups": [` + mbReleaseGroupJSON("rg-3", "Third", "Che", "mbid-che") + `]
		}`))
	}))
	defer server.Close()

	adapter := NewMusicBrainzAdapter(newTestClient(server.URL), "altune-test/1.0")
	rgs, err := adapter.fetchReleaseGroups(context.Background(), "mbid-che")
	if err != nil {
		t.Fatalf("fetchReleaseGroups: %v", err)
	}
	if len(offsets) != 2 || offsets[0] != "0" || offsets[1] != "100" {
		t.Errorf("offset params: got %v, want [0 100]", offsets)
	}
	if len(rgs) != 3 {
		t.Fatalf("expected 3 release-groups across pages, got %d", len(rgs))
	}
	for i, want := range []string{"First", "Second", "Third"} {
		if rgs[i].Title != want {
			t.Errorf("rg[%d]: got %q, want %q (pages appended in request order)", i, rgs[i].Title, want)
		}
	}
}
