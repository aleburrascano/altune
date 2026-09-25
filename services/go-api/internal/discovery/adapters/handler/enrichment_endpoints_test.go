package handler

import (
	"altune/go-api/internal/auth"
	discdomain "altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/discovery/service/enrich"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"testing"

	"github.com/go-chi/chi/v5"
)

// These tests pin the enrichment contract that a transient provider failure is
// distinguishable from a genuine "no data" result: both answer 200 with an
// empty payload, but only the failure sets degraded=true, and only the genuine
// miss is negative-cached (so the failure is retried on the next request).

var errProviderDown = errors.New("provider returned 503")

type memNameCache[T any] struct {
	pos map[string]T
	neg map[string]bool
}

func newMemNameCache[T any]() *memNameCache[T] {
	return &memNameCache[T]{pos: map[string]T{}, neg: map[string]bool{}}
}

func (c *memNameCache[T]) Get(_ context.Context, k string) (T, bool, error) {
	v, ok := c.pos[k]
	return v, ok, nil
}

func (c *memNameCache[T]) Set(_ context.Context, k string, v T) error {
	c.pos[k] = v
	return nil
}

func (c *memNameCache[T]) GetNegative(_ context.Context, k string) (bool, error) {
	return c.neg[k], nil
}

func (c *memNameCache[T]) SetNegative(_ context.Context, k string) error {
	c.neg[k] = true
	return nil
}

type memMBCache struct {
	pos map[string]discdomain.MBEnrichment
	neg map[string]bool
}

func newMemMBCache() *memMBCache {
	return &memMBCache{pos: map[string]discdomain.MBEnrichment{}, neg: map[string]bool{}}
}

func (c *memMBCache) Get(_ context.Context, kind discdomain.ResultKind, mbid string) (discdomain.MBEnrichment, bool, error) {
	v, ok := c.pos[kind.String()+mbid]
	return v, ok, nil
}

func (c *memMBCache) Set(_ context.Context, kind discdomain.ResultKind, mbid string, e discdomain.MBEnrichment) error {
	c.pos[kind.String()+mbid] = e
	return nil
}

func (c *memMBCache) GetNegative(_ context.Context, kind discdomain.ResultKind, nameKey string) (bool, error) {
	return c.neg[kind.String()+nameKey], nil
}

func (c *memMBCache) SetNegative(_ context.Context, kind discdomain.ResultKind, nameKey string) error {
	c.neg[kind.String()+nameKey] = true
	return nil
}

type scriptedMBEnricher struct {
	mbid       string
	resolveErr error
	lookupErr  error
	calls      int
}

func (f *scriptedMBEnricher) ResolveMBID(context.Context, discdomain.ResultKind, string, string) (string, error) {
	f.calls++
	return f.mbid, f.resolveErr
}

func (f *scriptedMBEnricher) Lookup(context.Context, discdomain.ResultKind, string) (discdomain.MBEnrichment, error) {
	f.calls++
	if f.lookupErr != nil {
		return discdomain.EmptyEnrichment(), f.lookupErr
	}
	return sampleAlbumEnrichment(), nil
}

type scriptedLastFmEnricher struct {
	err   error
	calls int
}

func (f *scriptedLastFmEnricher) Lookup(context.Context, discdomain.ResultKind, string, string) (discdomain.LastFmEnrichment, error) {
	f.calls++
	return discdomain.EmptyLastFmEnrichment(), f.err
}

type scriptedDeezerEnricher struct {
	id         string
	resolveErr error
	lookupErr  error
	calls      int
}

func (f *scriptedDeezerEnricher) ResolveID(context.Context, discdomain.ResultKind, string, string) (string, error) {
	f.calls++
	return f.id, f.resolveErr
}

func (f *scriptedDeezerEnricher) Lookup(context.Context, discdomain.ResultKind, string) (discdomain.DeezerEnrichment, error) {
	f.calls++
	return discdomain.EmptyDeezerEnrichment(), f.lookupErr
}

type scriptedLyricsProvider struct {
	id         string
	resolveErr error
	lookupErr  error
	calls      int
}

func (f *scriptedLyricsProvider) ResolveTrackID(context.Context, string, string) (string, error) {
	f.calls++
	return f.id, f.resolveErr
}

func (f *scriptedLyricsProvider) Lookup(context.Context, string) (discdomain.DeezerLyrics, error) {
	f.calls++
	return discdomain.EmptyDeezerLyrics(), f.lookupErr
}

type degradedCase struct {
	name string
	// build wires a fresh router and returns the provider call counter.
	build        func() (chi.Router, *int)
	path         string
	wantDegraded bool
	// wantSecondCalls is the cumulative provider call count after the same
	// request is served twice: a genuine miss is negative-cached (no new
	// calls), a transient failure is not (the provider is asked again).
	wantSecondCalls int
}

func mbRouter(f *scriptedMBEnricher) (chi.Router, *int) {
	svc := enrich.NewEnrichmentService(f, nil, newMemMBCache())
	return buildEnrichmentRouter(svc), &f.calls
}

func lastfmRouter(f *scriptedLastFmEnricher) (chi.Router, *int) {
	svc := enrich.NewLastFmEnrichmentService(f, newMemNameCache[discdomain.LastFmEnrichment]())
	return buildEnrichersRouter(DetailEnrichers{LastFm: svc}), &f.calls
}

func deezerRouter(f *scriptedDeezerEnricher) (chi.Router, *int) {
	svc := enrich.NewDeezerEnrichmentService(f, newMemNameCache[discdomain.DeezerEnrichment]())
	return buildEnrichersRouter(DetailEnrichers{Deezer: svc}), &f.calls
}

func lyricsRouter(f *scriptedLyricsProvider) (chi.Router, *int) {
	svc := enrich.NewLyricsService(f, newMemNameCache[discdomain.DeezerLyrics]())
	return buildEnrichersRouter(DetailEnrichers{Lyrics: svc}), &f.calls
}

func TestEnrichmentEndpoints_TransientFailureIsDistinguishableFromNoData(t *testing.T) {
	const (
		mbPath     = "/discovery/enrichment?kind=album&title=DAMN.&subtitle=Kendrick+Lamar"
		mbidPath   = "/discovery/enrichment?kind=album&title=DAMN.&mbid=1b022e01-4da6-387b-8658-8678046e4cef"
		lastfmPath = "/discovery/enrichment/lastfm?kind=artist&title=Nas"
		deezerPath = "/discovery/enrichment/deezer?kind=album&title=Illmatic&subtitle=Nas"
		lyricsPath = "/discovery/lyrics?title=N.Y.+State+of+Mind&subtitle=Nas"
	)
	cases := []degradedCase{
		{
			name:  "musicbrainz resolve error is degraded and retried",
			build: func() (chi.Router, *int) { return mbRouter(&scriptedMBEnricher{resolveErr: errProviderDown}) },
			path:  mbPath, wantDegraded: true, wantSecondCalls: 2,
		},
		{
			name: "musicbrainz lookup error after resolve is degraded and retried",
			build: func() (chi.Router, *int) {
				return mbRouter(&scriptedMBEnricher{mbid: "abc", lookupErr: errProviderDown})
			},
			path: mbPath, wantDegraded: true, wantSecondCalls: 4,
		},
		{
			name:  "musicbrainz lookup error by mbid is degraded and retried",
			build: func() (chi.Router, *int) { return mbRouter(&scriptedMBEnricher{lookupErr: errProviderDown}) },
			path:  mbidPath, wantDegraded: true, wantSecondCalls: 2,
		},
		{
			name:  "musicbrainz unresolved name is genuine empty and negative-cached",
			build: func() (chi.Router, *int) { return mbRouter(&scriptedMBEnricher{}) },
			path:  mbPath, wantDegraded: false, wantSecondCalls: 1,
		},
		{
			name:  "lastfm lookup error is degraded and retried",
			build: func() (chi.Router, *int) { return lastfmRouter(&scriptedLastFmEnricher{err: errProviderDown}) },
			path:  lastfmPath, wantDegraded: true, wantSecondCalls: 2,
		},
		{
			name:  "lastfm zero result is genuine empty and negative-cached",
			build: func() (chi.Router, *int) { return lastfmRouter(&scriptedLastFmEnricher{}) },
			path:  lastfmPath, wantDegraded: false, wantSecondCalls: 1,
		},
		{
			name:  "deezer resolve error is degraded and retried",
			build: func() (chi.Router, *int) { return deezerRouter(&scriptedDeezerEnricher{resolveErr: errProviderDown}) },
			path:  deezerPath, wantDegraded: true, wantSecondCalls: 2,
		},
		{
			name: "deezer lookup error is degraded and retried",
			build: func() (chi.Router, *int) {
				return deezerRouter(&scriptedDeezerEnricher{id: "dz-1", lookupErr: errProviderDown})
			},
			path: deezerPath, wantDegraded: true, wantSecondCalls: 4,
		},
		{
			name:  "deezer unresolved id is genuine empty and negative-cached",
			build: func() (chi.Router, *int) { return deezerRouter(&scriptedDeezerEnricher{}) },
			path:  deezerPath, wantDegraded: false, wantSecondCalls: 1,
		},
		{
			name: "lyrics lookup error is degraded and retried",
			build: func() (chi.Router, *int) {
				return lyricsRouter(&scriptedLyricsProvider{id: "t-1", lookupErr: errProviderDown})
			},
			path: lyricsPath, wantDegraded: true, wantSecondCalls: 4,
		},
		{
			name:  "lyrics unresolved track is genuine empty and negative-cached",
			build: func() (chi.Router, *int) { return lyricsRouter(&scriptedLyricsProvider{}) },
			path:  lyricsPath, wantDegraded: false, wantSecondCalls: 1,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			router, calls := tc.build()

			rec := discServe(t, router, http.MethodGet, tc.path, nil)
			discAssertStatus(t, rec, http.StatusOK)
			body := rec.Body.String()
			var raw map[string]json.RawMessage
			discDecodeJSON(t, rec, &raw)
			degraded, present := raw["degraded"]
			if !present {
				t.Fatalf("response has no degraded field, so a failure is indistinguishable from no data: %s", body)
			}
			if want := strconv.FormatBool(tc.wantDegraded); string(degraded) != want {
				t.Errorf("degraded = %s, want %s", degraded, want)
			}
			if hc, ok := raw["has_content"]; ok && string(hc) != "false" {
				t.Errorf("has_content = %s, want false for an empty result", hc)
			}

			discAssertStatus(t, discServe(t, router, http.MethodGet, tc.path, nil), http.StatusOK)
			if *calls != tc.wantSecondCalls {
				t.Errorf("provider calls after two requests = %d, want %d", *calls, tc.wantSecondCalls)
			}
		})
	}
}

func TestEnrichmentEndpoints_NonDegradedErrorStillFails(t *testing.T) {
	if degraded, err := splitDegraded(nil); degraded || err != nil {
		t.Errorf("splitDegraded(nil) = %v, %v", degraded, err)
	}
	plain := errors.New("boom")
	if degraded, err := splitDegraded(plain); degraded || !errors.Is(err, plain) {
		t.Errorf("a non-degraded error must pass through, got %v, %v", degraded, err)
	}
}

type fakeLastFmEnricher struct {
	enrichment discdomain.LastFmEnrichment
}

func (f *fakeLastFmEnricher) Lookup(context.Context, discdomain.ResultKind, string, string) (discdomain.LastFmEnrichment, error) {
	return f.enrichment, nil
}

type fakeDeezerEnricher struct {
	enrichment discdomain.DeezerEnrichment
}

func (f *fakeDeezerEnricher) ResolveID(context.Context, discdomain.ResultKind, string, string) (string, error) {
	return "dz-1", nil
}

func (f *fakeDeezerEnricher) Lookup(context.Context, discdomain.ResultKind, string) (discdomain.DeezerEnrichment, error) {
	return f.enrichment, nil
}

type fakeLyricsProvider struct {
	lyrics discdomain.DeezerLyrics
}

func (f *fakeLyricsProvider) ResolveTrackID(context.Context, string, string) (string, error) {
	return "t-1", nil
}

func (f *fakeLyricsProvider) Lookup(context.Context, string) (discdomain.DeezerLyrics, error) {
	return f.lyrics, nil
}

func buildEnrichersRouter(e DetailEnrichers) chi.Router {
	h := NewDiscoveryHandler(DiscoveryServices{}).WithDetailEnrichers(e)
	r := chi.NewRouter()
	r.Use(auth.Middleware(discVerifyAsTestUser))
	r.Mount("/discovery", h.Routes())
	return r
}

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

func TestEnrichmentToDTO_NilCollectionsBecomeEmpty(t *testing.T) {
	dto := enrichmentToDTO(discdomain.MBEnrichment{})
	if dto.Genres == nil || dto.SecondaryTypes == nil || dto.ExternalIDs == nil {
		t.Errorf("nil domain collections must map to empty, got %+v", dto)
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
