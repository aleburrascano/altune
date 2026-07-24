package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"altune/go-api/internal/auth"
	discdomain "altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/discovery/service/enrich"

	"github.com/go-chi/chi/v5"
)

// Fakes for the per-source detail-open enrichment families (Discogs album +
// artist, Last.fm, Deezer, lyrics). Each stands in for the provider adapter
// behind its enrich service; caches are left nil (uncached).

// --- fake Discogs enricher (album + artist sides) ---

type fakeDiscogsEnricher struct {
	album  discdomain.DiscogsEnrichment
	artist discdomain.DiscogsArtistEnrichment
}

func (f *fakeDiscogsEnricher) ResolveMasterID(context.Context, string, string) (int, error) {
	return 42, nil
}

func (f *fakeDiscogsEnricher) LookupAlbum(context.Context, int) (discdomain.DiscogsEnrichment, error) {
	return f.album, nil
}

func (f *fakeDiscogsEnricher) ResolveArtistID(context.Context, string) (int, error) {
	return 7, nil
}

func (f *fakeDiscogsEnricher) LookupArtist(context.Context, int) (discdomain.DiscogsArtistEnrichment, error) {
	return f.artist, nil
}

// --- fake Last.fm enricher ---

type fakeLastFmEnricher struct {
	enrichment discdomain.LastFmEnrichment
}

func (f *fakeLastFmEnricher) Lookup(context.Context, discdomain.ResultKind, string, string) (discdomain.LastFmEnrichment, error) {
	return f.enrichment, nil
}

// --- fake Deezer enricher ---

type fakeDeezerEnricher struct {
	enrichment discdomain.DeezerEnrichment
}

func (f *fakeDeezerEnricher) ResolveID(context.Context, discdomain.ResultKind, string, string) (string, error) {
	return "dz-1", nil
}

func (f *fakeDeezerEnricher) Lookup(context.Context, discdomain.ResultKind, string) (discdomain.DeezerEnrichment, error) {
	return f.enrichment, nil
}

// --- fake lyrics provider ---

type fakeLyricsProvider struct {
	lyrics discdomain.DeezerLyrics
}

func (f *fakeLyricsProvider) ResolveTrackID(context.Context, string, string) (string, error) {
	return "t-1", nil
}

func (f *fakeLyricsProvider) Lookup(context.Context, string) (discdomain.DeezerLyrics, error) {
	return f.lyrics, nil
}

// buildEnrichersRouter mounts a discovery handler with only the detail-open
// enrichers wired (any nil member degrades its endpoint to an empty DTO).
func buildEnrichersRouter(e DetailEnrichers) chi.Router {
	h := NewDiscoveryHandler(DiscoveryServices{}).WithDetailEnrichers(e)
	r := chi.NewRouter()
	r.Use(auth.Middleware(discVerifyAsTestUser))
	r.Mount("/discovery", h.Routes())
	return r
}

// ==================== Discogs album ====================

func TestHandleDiscogsEnrichment(t *testing.T) {
	t.Run("missing album returns 400", func(t *testing.T) {
		router := buildEnrichersRouter(DetailEnrichers{})
		rec := discServe(t, router, http.MethodGet, "/discovery/enrichment/discogs?artist=Nas", nil)
		discAssertStatus(t, rec, http.StatusBadRequest)
	})

	t.Run("wired service maps the full DTO", func(t *testing.T) {
		album := discdomain.EmptyDiscogsEnrichment()
		album.MasterID = 42
		album.Genres = []string{"Hip Hop"}
		album.Styles = []string{"Conscious"}
		album.Year = 1994
		album.Credits = []discdomain.DiscogsCredit{{Name: "DJ Premier", Role: "Producer"}}
		album.Labels = []discdomain.DiscogsLabelRef{{Name: "Columbia", Catno: "CK 57684"}}
		album.Formats = []string{"CD · Album"}
		album.Country = "US"
		album.Companies = []discdomain.DiscogsCompany{{Name: "D&D Studios", Role: "Recorded At"}}
		album.Community = discdomain.DiscogsCommunity{Have: 100, Want: 50, Rating: 4.8, Votes: 900}

		svc := enrich.NewDiscogsEnrichmentService(&fakeDiscogsEnricher{album: album}, nil)
		router := buildEnrichersRouter(DetailEnrichers{Discogs: svc})

		rec := discServe(t, router, http.MethodGet,
			"/discovery/enrichment/discogs?album=Illmatic&artist=Nas", nil)
		discAssertStatus(t, rec, http.StatusOK)
		discAssertJSON(t, rec)

		var resp DiscogsEnrichmentResponseDTO
		discDecodeJSON(t, rec, &resp)
		if resp.MasterID != 42 || resp.Year != 1994 || resp.Country != "US" {
			t.Errorf("scalar fields: %+v", resp)
		}
		if len(resp.Credits) != 1 || resp.Credits[0].Role != "Producer" {
			t.Errorf("credits = %+v", resp.Credits)
		}
		if len(resp.Labels) != 1 || resp.Labels[0].Catno != "CK 57684" {
			t.Errorf("labels = %+v", resp.Labels)
		}
		if len(resp.Companies) != 1 || resp.Companies[0].Role != "Recorded At" {
			t.Errorf("companies = %+v", resp.Companies)
		}
		if resp.Community.Have != 100 || resp.Community.Rating != 4.8 {
			t.Errorf("community = %+v", resp.Community)
		}
	})

	t.Run("nil enricher returns 200 empty DTO with non-null collections", func(t *testing.T) {
		router := buildEnrichersRouter(DetailEnrichers{})
		rec := discServe(t, router, http.MethodGet, "/discovery/enrichment/discogs?album=X", nil)
		discAssertStatus(t, rec, http.StatusOK)

		var raw map[string]json.RawMessage
		discDecodeJSON(t, rec, &raw)
		for _, key := range []string{"genres", "styles", "credits", "labels", "formats", "companies"} {
			if string(raw[key]) == "null" {
				t.Errorf("%s must be [] when empty, got null", key)
			}
		}
	})
}

// ==================== Discogs artist ====================

func TestHandleDiscogsArtistEnrichment(t *testing.T) {
	t.Run("missing name returns 400", func(t *testing.T) {
		router := buildEnrichersRouter(DetailEnrichers{})
		rec := discServe(t, router, http.MethodGet, "/discovery/enrichment/discogs/artist", nil)
		discAssertStatus(t, rec, http.StatusBadRequest)
	})

	t.Run("wired service maps the full DTO", func(t *testing.T) {
		artist := discdomain.EmptyDiscogsArtistEnrichment()
		artist.ArtistID = 7
		artist.Profile = "American rapper from Queensbridge."
		artist.RealName = "Nasir Jones"
		artist.Aliases = []string{"Nas Escobar"}
		artist.Groups = []string{"The Firm"}
		artist.Links = []discdomain.DiscogsLink{{Label: "Wikipedia", URL: "https://en.wikipedia.org/wiki/Nas"}}

		svc := enrich.NewDiscogsArtistEnrichmentService(&fakeDiscogsEnricher{artist: artist}, nil)
		router := buildEnrichersRouter(DetailEnrichers{DiscogsArtist: svc})

		rec := discServe(t, router, http.MethodGet, "/discovery/enrichment/discogs/artist?name=Nas", nil)
		discAssertStatus(t, rec, http.StatusOK)

		var resp DiscogsArtistEnrichmentResponseDTO
		discDecodeJSON(t, rec, &resp)
		if resp.ArtistID != 7 || resp.RealName != "Nasir Jones" {
			t.Errorf("scalar fields: %+v", resp)
		}
		if len(resp.Aliases) != 1 || resp.Aliases[0] != "Nas Escobar" {
			t.Errorf("aliases = %v", resp.Aliases)
		}
		if len(resp.Links) != 1 || resp.Links[0].Label != "Wikipedia" {
			t.Errorf("links = %+v", resp.Links)
		}
	})

	t.Run("nil enricher returns 200 empty DTO with non-null collections", func(t *testing.T) {
		router := buildEnrichersRouter(DetailEnrichers{})
		rec := discServe(t, router, http.MethodGet, "/discovery/enrichment/discogs/artist?name=X", nil)
		discAssertStatus(t, rec, http.StatusOK)

		var raw map[string]json.RawMessage
		discDecodeJSON(t, rec, &raw)
		for _, key := range []string{"aliases", "name_variations", "members", "groups", "links"} {
			if string(raw[key]) == "null" {
				t.Errorf("%s must be [] when empty, got null", key)
			}
		}
	})
}

// ==================== Last.fm ====================

func TestHandleLastFmEnrichment(t *testing.T) {
	t.Run("missing kind returns 400", func(t *testing.T) {
		router := buildEnrichersRouter(DetailEnrichers{})
		rec := discServe(t, router, http.MethodGet, "/discovery/enrichment/lastfm?title=Nas", nil)
		discAssertStatus(t, rec, http.StatusBadRequest)
	})

	t.Run("whitespace-only kind returns 400", func(t *testing.T) {
		router := buildEnrichersRouter(DetailEnrichers{})
		rec := discServe(t, router, http.MethodGet, "/discovery/enrichment/lastfm?kind=%20&title=Nas", nil)
		discAssertStatus(t, rec, http.StatusBadRequest)
	})

	t.Run("invalid kind returns 400", func(t *testing.T) {
		router := buildEnrichersRouter(DetailEnrichers{})
		rec := discServe(t, router, http.MethodGet, "/discovery/enrichment/lastfm?kind=mixtape&title=Nas", nil)
		discAssertStatus(t, rec, http.StatusBadRequest)
	})

	t.Run("missing title returns 400", func(t *testing.T) {
		router := buildEnrichersRouter(DetailEnrichers{})
		rec := discServe(t, router, http.MethodGet, "/discovery/enrichment/lastfm?kind=artist", nil)
		discAssertStatus(t, rec, http.StatusBadRequest)
	})

	t.Run("wired service maps the DTO", func(t *testing.T) {
		e := discdomain.EmptyLastFmEnrichment()
		e.MBID = "lfm-mbid"
		e.Listeners = 5_000_000
		e.Playcount = 90_000_000
		e.Tags = []string{"hip hop", "rap"}
		e.Bio = "Queensbridge legend."
		e.Similar = []string{"AZ", "Mobb Deep"}

		svc := enrich.NewLastFmEnrichmentService(&fakeLastFmEnricher{enrichment: e}, nil)
		router := buildEnrichersRouter(DetailEnrichers{LastFm: svc})

		rec := discServe(t, router, http.MethodGet, "/discovery/enrichment/lastfm?kind=artist&title=Nas", nil)
		discAssertStatus(t, rec, http.StatusOK)

		var resp LastFmEnrichmentResponseDTO
		discDecodeJSON(t, rec, &resp)
		if resp.MBID != "lfm-mbid" || resp.Listeners != 5_000_000 || resp.Playcount != 90_000_000 {
			t.Errorf("scalar fields: %+v", resp)
		}
		if len(resp.Tags) != 2 || len(resp.Similar) != 2 {
			t.Errorf("tags = %v, similar = %v", resp.Tags, resp.Similar)
		}
	})

	t.Run("nil enricher returns 200 empty DTO with non-null collections", func(t *testing.T) {
		router := buildEnrichersRouter(DetailEnrichers{})
		rec := discServe(t, router, http.MethodGet, "/discovery/enrichment/lastfm?kind=artist&title=X", nil)
		discAssertStatus(t, rec, http.StatusOK)

		var raw map[string]json.RawMessage
		discDecodeJSON(t, rec, &raw)
		for _, key := range []string{"tags", "similar"} {
			if string(raw[key]) == "null" {
				t.Errorf("%s must be [] when empty, got null", key)
			}
		}
	})
}

// ==================== Deezer ====================

func TestHandleDeezerEnrichment(t *testing.T) {
	t.Run("missing kind returns 400", func(t *testing.T) {
		router := buildEnrichersRouter(DetailEnrichers{})
		rec := discServe(t, router, http.MethodGet, "/discovery/enrichment/deezer?title=X", nil)
		discAssertStatus(t, rec, http.StatusBadRequest)
	})

	t.Run("missing title returns 400", func(t *testing.T) {
		router := buildEnrichersRouter(DetailEnrichers{})
		rec := discServe(t, router, http.MethodGet, "/discovery/enrichment/deezer?kind=track", nil)
		discAssertStatus(t, rec, http.StatusBadRequest)
	})

	t.Run("artist kind returns 200 empty (only track and album enrich)", func(t *testing.T) {
		e := discdomain.EmptyDeezerEnrichment()
		e.BPM = 90
		svc := enrich.NewDeezerEnrichmentService(&fakeDeezerEnricher{enrichment: e}, nil)
		router := buildEnrichersRouter(DetailEnrichers{Deezer: svc})

		rec := discServe(t, router, http.MethodGet, "/discovery/enrichment/deezer?kind=artist&title=Nas", nil)
		discAssertStatus(t, rec, http.StatusOK)

		var resp DeezerEnrichmentResponseDTO
		discDecodeJSON(t, rec, &resp)
		if resp.BPM != 0 {
			t.Errorf("BPM = %d, want 0 (artist kind never reaches the enricher)", resp.BPM)
		}
	})

	t.Run("wired service maps the DTO", func(t *testing.T) {
		e := discdomain.EmptyDeezerEnrichment()
		e.BPM = 92
		e.Gain = -7.3
		e.Explicit = true
		e.Label = "Columbia"
		e.Genres = []string{"Rap/Hip Hop"}
		e.UPC = "074645368429"
		e.RecordType = "album"

		svc := enrich.NewDeezerEnrichmentService(&fakeDeezerEnricher{enrichment: e}, nil)
		router := buildEnrichersRouter(DetailEnrichers{Deezer: svc})

		rec := discServe(t, router, http.MethodGet,
			"/discovery/enrichment/deezer?kind=album&title=Illmatic&subtitle=Nas", nil)
		discAssertStatus(t, rec, http.StatusOK)

		var resp DeezerEnrichmentResponseDTO
		discDecodeJSON(t, rec, &resp)
		if resp.BPM != 92 || resp.Gain != -7.3 || !resp.Explicit {
			t.Errorf("audio fields: %+v", resp)
		}
		if resp.Label != "Columbia" || resp.UPC != "074645368429" || resp.RecordType != "album" {
			t.Errorf("liner fields: %+v", resp)
		}
	})

	t.Run("nil enricher returns 200 empty DTO with non-null genres", func(t *testing.T) {
		router := buildEnrichersRouter(DetailEnrichers{})
		rec := discServe(t, router, http.MethodGet, "/discovery/enrichment/deezer?kind=track&title=X", nil)
		discAssertStatus(t, rec, http.StatusOK)

		var raw map[string]json.RawMessage
		discDecodeJSON(t, rec, &raw)
		if string(raw["genres"]) == "null" {
			t.Error("genres must be [] when empty, got null")
		}
	})
}

// ==================== Lyrics ====================

func TestHandleLyrics(t *testing.T) {
	t.Run("missing title returns 400", func(t *testing.T) {
		router := buildEnrichersRouter(DetailEnrichers{})
		rec := discServe(t, router, http.MethodGet, "/discovery/lyrics?subtitle=Nas", nil)
		discAssertStatus(t, rec, http.StatusBadRequest)
	})

	t.Run("wired service maps plain, synced lines, writers", func(t *testing.T) {
		l := discdomain.EmptyDeezerLyrics()
		l.Plain = "I never sleep, cause sleep is the cousin of death"
		l.SyncedLines = []discdomain.SyncedLyricLine{
			{Timecode: "[00:12.30]", Line: "I never sleep", Milliseconds: 12300, Duration: 2100},
		}
		l.Writers = []string{"Nasir Jones"}
		l.Copyright = "© 1994 Columbia"

		svc := enrich.NewLyricsService(&fakeLyricsProvider{lyrics: l}, nil)
		router := buildEnrichersRouter(DetailEnrichers{Lyrics: svc})

		rec := discServe(t, router, http.MethodGet,
			"/discovery/lyrics?title=N.Y.+State+of+Mind&subtitle=Nas", nil)
		discAssertStatus(t, rec, http.StatusOK)

		var resp LyricsResponseDTO
		discDecodeJSON(t, rec, &resp)
		if resp.Plain == "" || resp.Copyright != "© 1994 Columbia" {
			t.Errorf("scalar fields: %+v", resp)
		}
		if len(resp.SyncedLines) != 1 || resp.SyncedLines[0].Milliseconds != 12300 || resp.SyncedLines[0].Duration != 2100 {
			t.Errorf("synced_lines = %+v", resp.SyncedLines)
		}
		if len(resp.Writers) != 1 || resp.Writers[0] != "Nasir Jones" {
			t.Errorf("writers = %v", resp.Writers)
		}
	})

	t.Run("nil provider returns 200 empty DTO with non-null collections", func(t *testing.T) {
		router := buildEnrichersRouter(DetailEnrichers{})
		rec := discServe(t, router, http.MethodGet, "/discovery/lyrics?title=X", nil)
		discAssertStatus(t, rec, http.StatusOK)

		var raw map[string]json.RawMessage
		discDecodeJSON(t, rec, &raw)
		for _, key := range []string{"synced_lines", "writers"} {
			if string(raw[key]) == "null" {
				t.Errorf("%s must be [] when empty, got null", key)
			}
		}
	})
}

// ==================== mapper nil-collection guards ====================

// The domain Empty* constructors already return non-nil slices, so the mappers'
// nil-guards only fire on hand-built values — pin them so a null never leaks
// onto the wire regardless of how the domain value was constructed.

func TestEnrichmentToDTO_NilCollectionsBecomeEmpty(t *testing.T) {
	dto := enrichmentToDTO(discdomain.MBEnrichment{})
	if dto.Genres == nil || dto.SecondaryTypes == nil || dto.ExternalIDs == nil {
		t.Errorf("nil domain collections must map to empty, got %+v", dto)
	}
}

func TestDiscogsEnrichmentToDTO_NilCollectionsBecomeEmpty(t *testing.T) {
	dto := discogsEnrichmentToDTO(discdomain.DiscogsEnrichment{})
	if dto.Genres == nil || dto.Styles == nil || dto.Formats == nil {
		t.Errorf("nil domain collections must map to empty, got %+v", dto)
	}
	if dto.Credits == nil || dto.Labels == nil || dto.Companies == nil {
		t.Errorf("nil domain struct slices must map to empty, got %+v", dto)
	}
}

func TestNonNilStrings(t *testing.T) {
	if got := nonNilStrings(nil); got == nil || len(got) != 0 {
		t.Errorf("nonNilStrings(nil) = %v, want []", got)
	}
	in := []string{"a"}
	if got := nonNilStrings(in); len(got) != 1 || got[0] != "a" {
		t.Errorf("nonNilStrings(%v) = %v", in, got)
	}
}
