package providers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"altune/go-api/internal/discovery/domain"
)

// fixtures mirror the live-probed DAMN. shapes (docs/providers/discogs.md §4).

func writeJSON(w http.ResponseWriter, v any) {
	_ = json.NewEncoder(w).Encode(v)
}

func damnMaster() discogsMaster {
	return discogsMaster{
		ID:          1164779,
		Year:        2017,
		Genres:      []string{"Hip Hop", "Funk / Soul"},
		Styles:      []string{"Conscious", "Contemporary R&B", "Trap"},
		MainRelease: 10133538,
		Tracklist: []discogsTrack{
			{
				Title: "Blood",
				ExtraArtists: []discogsExtraArtist{
					{Name: "Bēkon", Role: "Producer", ID: 5689043},
					{Name: "Kendrick Duckworth", Role: "Written-By", ID: 3062364},
				},
			},
		},
	}
}

func damnRelease() discogsReleaseDetail {
	return discogsReleaseDetail{
		Country: "US",
		Genres:  []string{"Hip Hop"},
		Styles:  []string{"Conscious"},
		ExtraArtists: []discogsExtraArtist{
			{Name: "Anthony Tiffith", Role: "Executive-Producer", ID: 3368939},
			{Name: "Bernie Grundman", Role: "Mastered By", ID: 1},
			{Name: "Anthony Tiffith", Role: "Executive-Producer", ID: 3368939}, // dup
		},
		Labels: []discogsLabelRef{
			{Name: "Top Dawg Entertainment", Catno: "B0026716-02"},
			{Name: "Aftermath Entertainment", Catno: "B0026716-02"},
		},
		Companies: []discogsCompany{
			{Name: "Bernie Grundman Mastering", EntityTypeName: "Mastered At"},
			{Name: "Henson Recording Studios", EntityTypeName: "Recorded At"},
		},
		Formats: []discogsFormat{
			{Name: "CD", Descriptions: []string{"Album"}},
		},
		Community: discogsCommunity{
			Have: 2980,
			Want: 1946,
			Rating: struct {
				Count   int     `json:"count"`
				Average float64 `json:"average"`
			}{Count: 313, Average: 4.27},
		},
	}
}

func TestDiscogsAdapter_ResolveMasterID(t *testing.T) {
	t.Parallel()

	t.Run("matches master by artist and album", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/database/search" {
				t.Fatalf("unexpected path %q", r.URL.Path)
			}
			writeJSON(w, discogsSearchResponse{Results: []discogsSearchResult{
				{ID: 1164779, Title: "Kendrick Lamar - Damn", Type: "master"},
			}})
		}))
		defer srv.Close()

		adapter := newTestDiscogsAdapter(srv)
		overrideDiscogsBaseURL(adapter, srv.URL)

		id, err := adapter.ResolveMasterID(context.Background(), "Kendrick Lamar", "DAMN")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if id != 1164779 {
			t.Errorf("expected master id 1164779, got %d", id)
		}
	})

	t.Run("returns 0 when no title contains the album", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			writeJSON(w, discogsSearchResponse{Results: []discogsSearchResult{
				{ID: 99, Title: "Kendrick Lamar - To Pimp A Butterfly", Type: "master"},
			}})
		}))
		defer srv.Close()

		adapter := newTestDiscogsAdapter(srv)
		overrideDiscogsBaseURL(adapter, srv.URL)

		id, err := adapter.ResolveMasterID(context.Background(), "Kendrick Lamar", "DAMN")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if id != 0 {
			t.Errorf("expected 0 for no match, got %d", id)
		}
	})

	t.Run("returns 0 for empty album", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			t.Fatal("should not call the API for an empty album")
		}))
		defer srv.Close()

		adapter := newTestDiscogsAdapter(srv)
		overrideDiscogsBaseURL(adapter, srv.URL)

		id, err := adapter.ResolveMasterID(context.Background(), "Kendrick Lamar", "")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if id != 0 {
			t.Errorf("expected 0, got %d", id)
		}
	})
}

func TestDiscogsAdapter_LookupAlbum(t *testing.T) {
	t.Parallel()

	t.Run("assembles release-level credits, styles, labels, community", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/masters/1164779":
				writeJSON(w, damnMaster())
			case "/releases/10133538":
				writeJSON(w, damnRelease())
			default:
				t.Fatalf("unexpected path %q", r.URL.Path)
			}
		}))
		defer srv.Close()

		adapter := newTestDiscogsAdapter(srv)
		overrideDiscogsBaseURL(adapter, srv.URL)

		e, err := adapter.LookupAlbum(context.Background(), 1164779)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if e.MasterID != 1164779 {
			t.Errorf("master id: got %d", e.MasterID)
		}
		if e.Year != 2017 {
			t.Errorf("year: got %d", e.Year)
		}
		if len(e.Styles) != 3 || e.Styles[0] != "Conscious" {
			t.Errorf("styles: got %v", e.Styles)
		}
		// Release-level credits preferred over per-track; the duplicate is removed.
		if len(e.Credits) != 2 {
			t.Fatalf("credits: expected 2 deduped, got %d (%v)", len(e.Credits), e.Credits)
		}
		if e.Credits[0].Role != "Executive-Producer" || e.Credits[0].Name != "Anthony Tiffith" {
			t.Errorf("first credit: got %+v", e.Credits[0])
		}
		if len(e.Labels) != 2 || e.Labels[0].Catno != "B0026716-02" {
			t.Errorf("labels: got %v", e.Labels)
		}
		if len(e.Formats) != 1 || e.Formats[0] != "CD · Album" {
			t.Errorf("formats: got %v", e.Formats)
		}
		if e.Country != "US" {
			t.Errorf("country: got %q", e.Country)
		}
		if len(e.Companies) != 2 || e.Companies[0].Role != "Mastered At" {
			t.Errorf("companies: got %v", e.Companies)
		}
		if e.Community.Have != 2980 || e.Community.Rating != 4.27 || e.Community.Votes != 313 {
			t.Errorf("community: got %+v", e.Community)
		}
	})

	t.Run("degrades to per-track credits when the release fetch fails", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/masters/1164779":
				writeJSON(w, damnMaster())
			case "/releases/10133538":
				w.WriteHeader(http.StatusInternalServerError)
			default:
				t.Fatalf("unexpected path %q", r.URL.Path)
			}
		}))
		defer srv.Close()

		adapter := newTestDiscogsAdapter(srv)
		overrideDiscogsBaseURL(adapter, srv.URL)

		e, err := adapter.LookupAlbum(context.Background(), 1164779)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		// Master-only fields survive; credits fall back to the tracklist set.
		if len(e.Credits) != 2 {
			t.Fatalf("fallback credits: expected 2, got %d (%v)", len(e.Credits), e.Credits)
		}
		if e.Credits[0].Role != "Producer" {
			t.Errorf("expected per-track Producer credit, got %+v", e.Credits[0])
		}
		if len(e.Labels) != 0 || e.Country != "" {
			t.Errorf("expected no release fields on degrade, got labels=%v country=%q", e.Labels, e.Country)
		}
	})

	t.Run("returns error on master fetch failure", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNotFound)
		}))
		defer srv.Close()

		adapter := newTestDiscogsAdapter(srv)
		overrideDiscogsBaseURL(adapter, srv.URL)

		_, err := adapter.LookupAlbum(context.Background(), 1164779)
		if err == nil {
			t.Fatal("expected an error on a 404 master")
		}
	})

	t.Run("returns empty for non-positive master id", func(t *testing.T) {
		adapter := newTestDiscogsAdapter(httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			t.Fatal("should not call the API")
		})))
		e, err := adapter.LookupAlbum(context.Background(), 0)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !e.IsZero() {
			t.Errorf("expected zero enrichment, got %+v", e)
		}
	})
}

func TestCollectCredits_DedupAndCap(t *testing.T) {
	t.Parallel()

	raw := make([]discogsExtraArtist, 0, discogsCreditsCap+10)
	raw = append(raw,
		discogsExtraArtist{Name: "A", Role: "Producer"},
		discogsExtraArtist{Name: "A", Role: "Producer"}, // dup
		discogsExtraArtist{Name: "A", Role: "Written-By"},
		discogsExtraArtist{Name: "", Role: "Producer"}, // dropped (no name)
		discogsExtraArtist{Name: "B", Role: ""},        // dropped (no role)
	)
	for i := 0; i < discogsCreditsCap+10; i++ {
		raw = append(raw, discogsExtraArtist{Name: fmt.Sprintf("X%d", i), Role: "Featuring"})
	}

	got := collectCredits(raw)
	if len(got) != discogsCreditsCap {
		t.Fatalf("expected cap of %d, got %d", discogsCreditsCap, len(got))
	}
	if got[0] != (domain.DiscogsCredit{Name: "A", Role: "Producer"}) {
		t.Errorf("first credit: got %+v", got[0])
	}
}

func TestDiscogsAdapter_ResolveArtistID(t *testing.T) {
	t.Parallel()

	t.Run("prefers the exact normalized name match", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			writeJSON(w, discogsSearchResponse{Results: []discogsSearchResult{
				{ID: 200, Title: "Kendrick Lamar Tribute", Type: "artist"},
				{ID: 3062364, Title: "Kendrick Lamar", Type: "artist"},
			}})
		}))
		defer srv.Close()

		adapter := newTestDiscogsAdapter(srv)
		overrideDiscogsBaseURL(adapter, srv.URL)

		id, err := adapter.ResolveArtistID(context.Background(), "Kendrick Lamar")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if id != 3062364 {
			t.Errorf("expected exact match 3062364, got %d", id)
		}
	})

	t.Run("falls back to the top result when no exact match", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			writeJSON(w, discogsSearchResponse{Results: []discogsSearchResult{
				{ID: 7, Title: "Some Other Artist", Type: "artist"},
			}})
		}))
		defer srv.Close()

		adapter := newTestDiscogsAdapter(srv)
		overrideDiscogsBaseURL(adapter, srv.URL)

		id, err := adapter.ResolveArtistID(context.Background(), "Kendrick Lamar")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if id != 7 {
			t.Errorf("expected fallback to 7, got %d", id)
		}
	})

	t.Run("returns 0 for an empty name", func(t *testing.T) {
		adapter := newTestDiscogsAdapter(httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			t.Fatal("should not call the API for an empty name")
		})))
		id, err := adapter.ResolveArtistID(context.Background(), "  ")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if id != 0 {
			t.Errorf("expected 0, got %d", id)
		}
	})
}

func TestDiscogsAdapter_LookupArtist(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/artists/3062364" {
			t.Fatalf("unexpected path %q", r.URL.Path)
		}
		writeJSON(w, discogsArtistFull{
			Profile:        "American rapper, best known as [b]Kendrick Lamar[/b]. Signed to [l=Top Dawg Entertainment]. See [a=Dr. Dre] and [a123].",
			RealName:       "Kendrick Lamar Duckworth",
			NameVariations: []string{"K. Duckworth", "K. Duckworth"},
			Aliases:        []discogsArtistRef{{Name: "K Dot (2)"}, {Name: "OKLAMA"}},
			Groups:         []discogsArtistRef{{Name: "Black Hippy"}},
			Members:        nil,
			URLs: []string{
				"https://en.wikipedia.org/wiki/Kendrick_Lamar",
				"https://instagram.com/kendricklamar",
				"http://www.kendricklamar.com/",
			},
		})
	}))
	defer srv.Close()

	adapter := newTestDiscogsAdapter(srv)
	overrideDiscogsBaseURL(adapter, srv.URL)

	e, err := adapter.LookupArtist(context.Background(), 3062364)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if e.ArtistID != 3062364 {
		t.Errorf("artist id: got %d", e.ArtistID)
	}
	if e.RealName != "Kendrick Lamar Duckworth" {
		t.Errorf("realname: got %q", e.RealName)
	}
	wantProfile := "American rapper, best known as Kendrick Lamar. Signed to Top Dawg Entertainment. See Dr. Dre and ."
	if e.Profile != wantProfile {
		t.Errorf("profile:\n got %q\nwant %q", e.Profile, wantProfile)
	}
	if len(e.NameVariations) != 1 {
		t.Errorf("namevariations should dedupe to 1, got %v", e.NameVariations)
	}
	if len(e.Aliases) != 2 || e.Aliases[0] != "K Dot (2)" {
		t.Errorf("aliases: got %v", e.Aliases)
	}
	if len(e.Groups) != 1 || e.Groups[0] != "Black Hippy" {
		t.Errorf("groups: got %v", e.Groups)
	}
	if len(e.Links) != 3 {
		t.Fatalf("links: got %d (%v)", len(e.Links), e.Links)
	}
	if e.Links[0].Label != "Wikipedia" || e.Links[1].Label != "Instagram" {
		t.Errorf("link labels: got %q, %q", e.Links[0].Label, e.Links[1].Label)
	}
	if e.Links[2].Label != "kendricklamar.com" {
		t.Errorf("unknown-host label: got %q", e.Links[2].Label)
	}
}

func TestCleanDiscogsProfile(t *testing.T) {
	t.Parallel()
	cases := map[string]string{
		"":                              "",
		"[b]Bold[/b] text":              "Bold text",
		"See [a=Dr. Dre] now":           "See Dr. Dre now",
		"ref [a123] and [l45]":          "ref  and",
		"[url=https://x.com]link[/url]": "link",
	}
	for in, want := range cases {
		if got := cleanDiscogsProfile(in); got != want {
			t.Errorf("cleanDiscogsProfile(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestMapFormats(t *testing.T) {
	t.Parallel()
	got := mapFormats([]discogsFormat{
		{Name: "Vinyl", Descriptions: []string{"LP", "Album"}},
		{Name: "", Descriptions: []string{}},
	})
	if len(got) != 1 || got[0] != "Vinyl · LP · Album" {
		t.Errorf("got %v", got)
	}
}
