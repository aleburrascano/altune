package providers

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"altune/go-api/internal/discovery/domain"
)

func TestSoundCloudAPIAdapter_ResolveArtistID(t *testing.T) {
	t.Run("top user hit wins", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !strings.Contains(r.URL.Path, "/search/users") {
				t.Errorf("unexpected path %q", r.URL.Path)
			}
			_, _ = w.Write([]byte(`{"collection":[
				{"id":909010162,"kind":"user","username":"Che","permalink_url":"https://soundcloud.com/che"},
				{"id":42,"kind":"user","username":"Che Fan Page","permalink_url":"https://soundcloud.com/chefan"}
			]}`))
		}))
		defer srv.Close()

		a := newTestSoundCloudAPI(srv, nil)
		id, ok := a.ResolveArtistID(context.Background(), "Che")
		if !ok || id != "909010162" {
			t.Errorf("ResolveArtistID = (%q, %v), want the top hit's numeric id", id, ok)
		}
	})

	t.Run("blank name sits out without a request", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			t.Error("no HTTP request expected for a blank name")
			w.WriteHeader(http.StatusInternalServerError)
		}))
		defer srv.Close()

		a := newTestSoundCloudAPI(srv, nil)
		if id, ok := a.ResolveArtistID(context.Background(), "   "); ok || id != "" {
			t.Errorf("ResolveArtistID = (%q, %v), want a silent miss", id, ok)
		}
	})

	t.Run("search error is a miss not an error", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		}))
		defer srv.Close()

		a := newTestSoundCloudAPI(srv, nil)
		if id, ok := a.ResolveArtistID(context.Background(), "Che"); ok || id != "" {
			t.Errorf("ResolveArtistID = (%q, %v), want ok=false so the provider sits out", id, ok)
		}
	})
}

func TestSoundCloudAPIAdapter_ResolvePermalink(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/resolve") {
			t.Errorf("unexpected path %q", r.URL.Path)
		}
		if got := r.URL.Query().Get("url"); got != "https://soundcloud.com/che/los-santos" {
			t.Errorf("url param = %q", got)
		}
		_, _ = w.Write([]byte(`{
			"id": 111, "kind": "track", "title": "Los Santos",
			"permalink_url": "https://soundcloud.com/che/los-santos",
			"duration": 125000,
			"user": {"username": "Che"}
		}`))
	}))
	defer srv.Close()

	a := newTestSoundCloudAPI(srv, nil)
	r, err := a.ResolvePermalink(context.Background(), "https://soundcloud.com/che/los-santos")
	if err != nil {
		t.Fatalf("ResolvePermalink: %v", err)
	}
	if r.Title != "Los Santos" || r.Subtitle != "Che" || r.Duration != 125 {
		t.Errorf("result = %+v", r)
	}
	if r.Sources[0].ExternalID != "111" {
		t.Errorf("ExternalID = %q, want 111", r.Sources[0].ExternalID)
	}
}

func TestSoundCloudAPIAdapter_ResolvePermalink_reResolvesClientIDOnAuth(t *testing.T) {
	const freshID = "abcdefabcdefabcdefabcdefabcdef12"
	var resolveHits, staleHits, freshHits int
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, ".js"):
			_, _ = w.Write([]byte(`client_id:"` + freshID + `"`))
		case strings.HasSuffix(r.URL.Path, "/resolve"):
			if r.URL.Query().Get("client_id") == freshID {
				freshHits++
				_, _ = w.Write([]byte(`{"id": 111, "kind": "track", "title": "Los Santos", "user": {"username": "Che"}}`))
				return
			}
			staleHits++
			w.WriteHeader(http.StatusUnauthorized)
		default: // homepage scrape for the re-resolve
			resolveHits++
			_, _ = w.Write([]byte(`<script src="` + srv.URL + `/assets/app-1.js"></script>`))
		}
	}))
	defer srv.Close()

	a := newTestSoundCloudAPI(srv, nil)
	a.resolver.siteURL = srv.URL

	r, err := a.ResolvePermalink(context.Background(), "https://soundcloud.com/che/los-santos")
	if err != nil {
		t.Fatalf("ResolvePermalink after auth retry: %v", err)
	}
	if r.Title != "Los Santos" {
		t.Errorf("result = %+v", r)
	}
	if staleHits != 1 || resolveHits != 1 || freshHits != 1 {
		t.Errorf("stale=%d resolve=%d fresh=%d, want exactly one 401 → one re-resolve → one retry",
			staleHits, resolveHits, freshHits)
	}
}

func TestSoundCloudAPIAdapter_ResolvePermalink_nonTrackIsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"id": 909, "kind": "user", "username": "Che"}`))
	}))
	defer srv.Close()

	a := newTestSoundCloudAPI(srv, nil)
	_, err := a.ResolvePermalink(context.Background(), "https://soundcloud.com/che")
	if err == nil || !strings.Contains(err.Error(), "did not yield a track") {
		t.Fatalf("err = %v, want the non-track rejection", err)
	}
}

func TestSoundCloud_doSearch_capsAtMaxResults(t *testing.T) {
	var pages int
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		pages++
		items := make([]string, scSearchLimit)
		for i := range items {
			items[i] = fmt.Sprintf(`{"id": %d, "kind": "track", "title": "T%d", "user": {"username": "U"}}`,
				pages*1000+i, i)
		}
		// next_href always present — the cap, not the server, must stop the walk.
		_, _ = w.Write([]byte(`{"collection":[` + strings.Join(items, ",") + `],"next_href":"` +
			srv.URL + `/search/tracks?offset=next"}`))
	}))
	defer srv.Close()

	a := newTestSoundCloudAPI(srv, nil)
	results, err := a.Search(context.Background(), "prolific", trackKinds())
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) != scMaxResults {
		t.Errorf("results = %d, want the %d cap", len(results), scMaxResults)
	}
	if pages != scMaxResults/scSearchLimit {
		t.Errorf("pages fetched = %d, want %d (stop as soon as the cap is reached)", pages, scMaxResults/scSearchLimit)
	}
}

func TestSCBestReleaseDate(t *testing.T) {
	tests := []struct {
		name                               string
		release, display, created, want string
	}{
		{"release wins", "2020-01-01", "2020-02-02", "2020-03-03", "2020-01-01"},
		{"display fallback", "", "2020-02-02", "2020-03-03", "2020-02-02"},
		{"created fallback", "", "  ", "2020-03-03", "2020-03-03"},
		{"all empty", "", "", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := scBestReleaseDate(tt.release, tt.display, tt.created); got != tt.want {
				t.Errorf("scBestReleaseDate = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestMapSoundCloudStandaloneSingle(t *testing.T) {
	r, ok := mapSoundCloudStandaloneSingle(scAPITrack{
		ID: 99, Kind: "track", Title: "14 HAHAHA LOL",
		Genre:       "rage",
		DisplayDate: "2026-07-20T00:00:00Z",
		User:        struct {
			Username string `json:"username"`
		}{Username: "Che"},
	})
	if !ok {
		t.Fatal("expected a mapped single")
	}
	if r.Kind != domain.ResultKindAlbum || r.Extras["record_type"] != "single" || r.TrackCount != 1 {
		t.Errorf("result = %+v, want an album-kind single with one track", r)
	}
	if r.ReleaseDate != "2026-07-20T00:00:00Z" {
		t.Errorf("ReleaseDate = %q, want display_date fallback", r.ReleaseDate)
	}
	if r.Extras["genre"] != "rage" {
		t.Errorf("genre = %v", r.Extras["genre"])
	}

	if _, ok := mapSoundCloudStandaloneSingle(scAPITrack{ID: 1, Kind: "playlist", Title: "X"}); ok {
		t.Error("non-track kind must be rejected")
	}
	if _, ok := mapSoundCloudStandaloneSingle(scAPITrack{ID: 1, Kind: "track", Title: "  "}); ok {
		t.Error("blank title must be rejected")
	}
	if _, ok := mapSoundCloudStandaloneSingle(scAPITrack{Kind: "track", Title: "No ID"}); ok {
		t.Error("zero id must be rejected")
	}
}
