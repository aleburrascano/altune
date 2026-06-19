package providers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"altune/go-api/internal/discovery/domain"
)

// mbReleaseGroupJSON returns a release-group JSON object for test fixtures.
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
	if r.Extras["mbid"] != "abc-123-def" {
		t.Errorf("extras.mbid: got %v, want %q", r.Extras["mbid"], "abc-123-def")
	}
	if r.Extras["isrc"] != "GBAYE9700011" {
		t.Errorf("extras.isrc: got %v, want %q", r.Extras["isrc"], "GBAYE9700011")
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
	if r.Extras["mbid"] != "a74b1b7f-71a5-4011-9441-d0b5e4122711" {
		t.Errorf("extras.mbid: got %v, want the artist MBID", r.Extras["mbid"])
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
	if r.Extras["mbid"] != "rg-001-abc" {
		t.Errorf("extras.mbid: got %v, want %q", r.Extras["mbid"], "rg-001-abc")
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
	if err != nil {
		t.Fatalf("expected nil error on HTTP 500 (silent skip), got: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("expected 0 results on HTTP 500, got %d", len(results))
	}
}

func TestMusicBrainzAdapter_LookupAlbumArtist(t *testing.T) {
	const (
		artistMBID    = "a74b1b7f-71a5-4011-9441-d0b5e4122711"
		differentMBID = "ffffffff-aaaa-bbbb-cccc-000000000000"
	)

	t.Run("album found, credited to same mbid - confirmed", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"release-groups": [` +
				mbReleaseGroupJSON("rg-001", "OK Computer", "Radiohead", artistMBID) +
				`]}`))
		}))
		defer server.Close()

		adapter := NewMusicBrainzAdapter(newTestClient(server.URL), "altune-test/1.0")
		profile := domain.NewArtistIdentityProfile()
		profile.MBID = artistMBID

		verdict, creditedMBID, err := adapter.LookupAlbumArtist(
			context.Background(), "Radiohead", "OK Computer", profile,
		)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if verdict != domain.AlbumVerdictConfirmed {
			t.Errorf("verdict: got %v, want confirmed", verdict)
		}
		if creditedMBID != artistMBID {
			t.Errorf("creditedMBID: got %q, want %q", creditedMBID, artistMBID)
		}
	})

	t.Run("album found, credited to different mbid - contamination", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"release-groups": [` +
				mbReleaseGroupJSON("rg-002", "OK Computer", "Some Other Artist", differentMBID) +
				`]}`))
		}))
		defer server.Close()

		adapter := NewMusicBrainzAdapter(newTestClient(server.URL), "altune-test/1.0")
		profile := domain.NewArtistIdentityProfile()
		profile.MBID = artistMBID

		verdict, creditedMBID, err := adapter.LookupAlbumArtist(
			context.Background(), "Some Other Artist", "OK Computer", profile,
		)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if verdict != domain.AlbumVerdictContamination {
			t.Errorf("verdict: got %v, want contamination", verdict)
		}
		if creditedMBID != differentMBID {
			t.Errorf("creditedMBID: got %q, want %q", creditedMBID, differentMBID)
		}
	})

	t.Run("no mb results - unknown", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"release-groups": []}`))
		}))
		defer server.Close()

		adapter := NewMusicBrainzAdapter(newTestClient(server.URL), "altune-test/1.0")
		profile := domain.NewArtistIdentityProfile()
		profile.MBID = artistMBID

		verdict, creditedMBID, err := adapter.LookupAlbumArtist(
			context.Background(), "Radiohead", "Nonexistent Album", profile,
		)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if verdict != domain.AlbumVerdictUnknown {
			t.Errorf("verdict: got %v, want unknown", verdict)
		}
		if creditedMBID != "" {
			t.Errorf("creditedMBID: got %q, want empty", creditedMBID)
		}
	})

	t.Run("mb returns error - unknown (graceful degradation)", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		}))
		defer server.Close()

		adapter := NewMusicBrainzAdapter(newTestClient(server.URL), "altune-test/1.0")
		profile := domain.NewArtistIdentityProfile()
		profile.MBID = artistMBID

		verdict, creditedMBID, err := adapter.LookupAlbumArtist(
			context.Background(), "Radiohead", "OK Computer", profile,
		)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if verdict != domain.AlbumVerdictUnknown {
			t.Errorf("verdict: got %v, want unknown", verdict)
		}
		if creditedMBID != "" {
			t.Errorf("creditedMBID: got %q, want empty", creditedMBID)
		}
	})

	t.Run("multiple results, title match selects correct one", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"release-groups": [` +
				mbReleaseGroupJSON("rg-wrong", "Kid A", "Radiohead", differentMBID) + `,` +
				mbReleaseGroupJSON("rg-right", "OK Computer", "Radiohead", artistMBID) + `,` +
				mbReleaseGroupJSON("rg-extra", "The Bends", "Radiohead", artistMBID) +
				`]}`))
		}))
		defer server.Close()

		adapter := NewMusicBrainzAdapter(newTestClient(server.URL), "altune-test/1.0")
		profile := domain.NewArtistIdentityProfile()
		profile.MBID = artistMBID

		verdict, creditedMBID, err := adapter.LookupAlbumArtist(
			context.Background(), "Radiohead", "OK Computer", profile,
		)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if verdict != domain.AlbumVerdictConfirmed {
			t.Errorf("verdict: got %v, want confirmed", verdict)
		}
		if creditedMBID != artistMBID {
			t.Errorf("creditedMBID: got %q, want %q", creditedMBID, artistMBID)
		}
	})
}
