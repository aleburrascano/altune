package handler_test

import (
	"altune/go-api/internal/discovery"
	"altune/go-api/internal/shared/httputil"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"testing"

	"github.com/go-chi/chi/v5"
)

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
	pos map[string]discovery.MBEnrichment
	neg map[string]bool
}

func newMemMBCache() *memMBCache {
	return &memMBCache{pos: map[string]discovery.MBEnrichment{}, neg: map[string]bool{}}
}

func (c *memMBCache) Get(_ context.Context, kind discovery.ResultKind, mbid string) (discovery.MBEnrichment, bool, error) {
	v, ok := c.pos[kind.String()+mbid]
	return v, ok, nil
}

func (c *memMBCache) Set(_ context.Context, kind discovery.ResultKind, mbid string, e discovery.MBEnrichment) error {
	c.pos[kind.String()+mbid] = e
	return nil
}

func (c *memMBCache) GetNegative(_ context.Context, kind discovery.ResultKind, nameKey string) (bool, error) {
	return c.neg[kind.String()+nameKey], nil
}

func (c *memMBCache) SetNegative(_ context.Context, kind discovery.ResultKind, nameKey string) error {
	c.neg[kind.String()+nameKey] = true
	return nil
}

type scriptedMBEnricher struct {
	mbid       string
	resolveErr error
	lookupErr  error
	calls      int
}

func (f *scriptedMBEnricher) ResolveMBID(context.Context, discovery.ResultKind, string, string) (string, error) {
	f.calls++
	return f.mbid, f.resolveErr
}

func (f *scriptedMBEnricher) Lookup(context.Context, discovery.ResultKind, string) (discovery.MBEnrichment, error) {
	f.calls++
	if f.lookupErr != nil {
		return discovery.EmptyEnrichment(), f.lookupErr
	}
	return sampleAlbumEnrichment(), nil
}

type scriptedDeezerEnricher struct {
	id         string
	found      *discovery.DeezerEnrichment
	resolveErr error
	lookupErr  error
	calls      int
}

func (f *scriptedDeezerEnricher) ResolveID(context.Context, discovery.ResultKind, string, string) (string, error) {
	f.calls++
	return f.id, f.resolveErr
}

func (f *scriptedDeezerEnricher) Lookup(context.Context, discovery.ResultKind, string) (discovery.DeezerEnrichment, error) {
	f.calls++
	if f.found != nil {
		return *f.found, f.lookupErr
	}
	return discovery.EmptyDeezerEnrichment(), f.lookupErr
}

type scriptedLyricsProvider struct {
	id         string
	found      *discovery.DeezerLyrics
	resolveErr error
	lookupErr  error
	calls      int
}

func (f *scriptedLyricsProvider) ResolveTrackID(context.Context, string, string) (string, error) {
	f.calls++
	return f.id, f.resolveErr
}

func (f *scriptedLyricsProvider) Lookup(context.Context, string) (discovery.DeezerLyrics, error) {
	f.calls++
	if f.found != nil {
		return *f.found, f.lookupErr
	}
	return discovery.EmptyDeezerLyrics(), f.lookupErr
}

type degradedCase struct {
	name            string
	build           func() (chi.Router, *int)
	path            string
	wantDegraded    bool
	wantSecondCalls int
}

func mbRouter(f *scriptedMBEnricher) (chi.Router, *int) {
	svc := discovery.NewEnrichmentService(f, nil, newMemMBCache())
	return buildEnrichmentRouter(svc), &f.calls
}

func deezerRouter(f *scriptedDeezerEnricher) (chi.Router, *int) {
	svc := discovery.NewDeezerEnrichmentService(f, newMemNameCache[discovery.DeezerEnrichment]())
	return buildEnrichersRouter(discovery.DetailEnrichers{Deezer: svc}), &f.calls
}

func lyricsRouter(f *scriptedLyricsProvider) (chi.Router, *int) {
	svc := discovery.NewLyricsService(f, newMemNameCache[discovery.DeezerLyrics]())
	return buildEnrichersRouter(discovery.DetailEnrichers{Lyrics: svc}), &f.calls
}

func TestEnrichmentEndpoints_TransientFailureIsDistinguishableFromNoData(t *testing.T) {
	const (
		mbPath     = "/discovery/enrichment?kind=album&title=DAMN.&subtitle=Kendrick+Lamar"
		mbidPath   = "/discovery/enrichment?kind=album&title=DAMN.&mbid=1b022e01-4da6-387b-8658-8678046e4cef"
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

func buildEnrichersRouter(e discovery.DetailEnrichers) chi.Router {
	h := discovery.NewDiscoveryHandler(discovery.DiscoveryServices{}).WithDetailEnrichers(e)
	r := chi.NewRouter()
	r.Use(discAsTestUser)
	r.Mount("/discovery", h.Routes())
	return r
}

func TestHandleDeezerEnrichment(t *testing.T) {
	t.Run("missing kind returns 400", func(t *testing.T) {
		router := buildEnrichersRouter(discovery.DetailEnrichers{})
		rec := discServe(t, router, http.MethodGet, "/discovery/enrichment/deezer?title=X", nil)
		discAssertStatus(t, rec, http.StatusBadRequest)
	})

	t.Run("missing title returns 400", func(t *testing.T) {
		router := buildEnrichersRouter(discovery.DetailEnrichers{})
		rec := discServe(t, router, http.MethodGet, "/discovery/enrichment/deezer?kind=track", nil)
		discAssertStatus(t, rec, http.StatusBadRequest)
	})

	t.Run("artist kind returns 200 empty (only track and album enrich)", func(t *testing.T) {
		e := discovery.EmptyDeezerEnrichment()
		e.BPM = 90
		svc := discovery.NewDeezerEnrichmentService(&scriptedDeezerEnricher{id: "dz-1", found: &e}, nil)
		router := buildEnrichersRouter(discovery.DetailEnrichers{Deezer: svc})

		rec := discServe(t, router, http.MethodGet, "/discovery/enrichment/deezer?kind=artist&title=Nas", nil)
		discAssertStatus(t, rec, http.StatusOK)

		var resp discovery.DeezerEnrichmentResponseDTO
		discDecodeJSON(t, rec, &resp)
		if resp.BPM != 0 {
			t.Errorf("BPM = %d, want 0 (artist kind never reaches the enricher)", resp.BPM)
		}
	})

	t.Run("nil enricher returns 200 empty DTO with non-null genres", func(t *testing.T) {
		router := buildEnrichersRouter(discovery.DetailEnrichers{})
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
		router := buildEnrichersRouter(discovery.DetailEnrichers{})
		rec := discServe(t, router, http.MethodGet, "/discovery/lyrics?subtitle=Nas", nil)
		discAssertStatus(t, rec, http.StatusBadRequest)
	})

	t.Run("wired service maps plain, synced lines, writers", func(t *testing.T) {
		l := discovery.EmptyDeezerLyrics()
		l.Plain = "I never sleep, cause sleep is the cousin of death"
		l.SyncedLines = []discovery.SyncedLyricLine{
			{Timecode: "[00:12.30]", Line: "I never sleep", Milliseconds: 12300, Duration: 2100},
		}
		l.Writers = []string{"Nasir Jones"}
		l.Copyright = "© 1994 Columbia"

		svc := discovery.NewLyricsService(&scriptedLyricsProvider{id: "t-1", found: &l}, nil)
		router := buildEnrichersRouter(discovery.DetailEnrichers{Lyrics: svc})

		rec := discServe(t, router, http.MethodGet,
			"/discovery/lyrics?title=N.Y.+State+of+Mind&subtitle=Nas", nil)
		discAssertStatus(t, rec, http.StatusOK)

		var resp discovery.LyricsResponseDTO
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
		router := buildEnrichersRouter(discovery.DetailEnrichers{})
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

type bareMBEnricher struct{}

func (bareMBEnricher) ResolveMBID(context.Context, discovery.ResultKind, string, string) (string, error) {
	return "mbid-1", nil
}

func (bareMBEnricher) Lookup(context.Context, discovery.ResultKind, string) (discovery.MBEnrichment, error) {
	return discovery.MBEnrichment{MBID: "mbid-1"}, nil
}

func TestEnrichmentEndpoints_NilCollectionsServeAsEmpty(t *testing.T) {
	cases := []struct {
		name   string
		router chi.Router
		path   string
		want   map[string]string
	}{
		{
			name:   "musicbrainz",
			router: buildEnrichmentRouter(discovery.NewEnrichmentService(bareMBEnricher{}, nil, nil)),
			path:   "/discovery/enrichment?kind=album&title=Illmatic",
			want:   map[string]string{"genres": "[]", "secondary_types": "[]", "external_ids": "{}"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := discServe(t, tc.router, http.MethodGet, tc.path, nil)
			discAssertStatus(t, rec, http.StatusOK)
			var raw map[string]json.RawMessage
			discDecodeJSON(t, rec, &raw)
			for key, want := range tc.want {
				if string(raw[key]) != want {
					t.Errorf("%s = %s, want %s (body %s)", key, raw[key], want, rec.Body.String())
				}
			}
		})
	}
}

func TestEnrichmentEndpoints_TypedProviderErrorStaysDegraded(t *testing.T) {
	upstream := discovery.StatusError{Status: http.StatusNotFound}
	router, _ := deezerRouter(&scriptedDeezerEnricher{resolveErr: upstream})

	rec := discServe(t, router, http.MethodGet, "/discovery/enrichment/deezer?kind=album&title=Illmatic&subtitle=Nas", nil)

	discAssertStatus(t, rec, http.StatusOK)
	var resp discovery.DeezerEnrichmentResponseDTO
	discDecodeJSON(t, rec, &resp)
	if !resp.Degraded {
		t.Errorf("degraded = false, want true")
	}
}

func TestEnrichmentRoutes_NoIdentityIs401(t *testing.T) {
	paths := []string{
		"/discovery/enrichment?kind=track&title=X",
		"/discovery/enrichment/deezer?kind=track&title=X",
		"/discovery/lyrics?title=X&artist=Y",
	}
	router := unauthenticatedDiscoveryRouter(discovery.NewDiscoveryHandler(discovery.DiscoveryServices{}))
	for _, path := range paths {
		t.Run(path, func(t *testing.T) {
			discAssertStatus(t, discServeNoAuth(t, router, http.MethodGet, path), http.StatusUnauthorized)
		})
	}
}

func TestHandleEnrichment_UnconfiguredIsNotEmptySuccess(t *testing.T) {
	router := buildEnrichersRouter(discovery.DetailEnrichers{})

	rec := discServe(t, router, http.MethodGet, "/discovery/enrichment?kind=album&title=X", nil)

	assertErrorCode(t, rec, http.StatusServiceUnavailable, "enrichment_unavailable")
}

type namedEnrichmentCase struct {
	name        string
	path        string
	router      func(err error) chi.Router
	emptyFields []string
}

func namedEnrichmentCases() []namedEnrichmentCase {
	return []namedEnrichmentCase{
		{
			name: "deezer",
			path: "/discovery/enrichment/deezer",
			router: func(err error) chi.Router {
				r, _ := deezerRouter(&scriptedDeezerEnricher{resolveErr: err})
				return r
			},
			emptyFields: []string{"genres"},
		},
	}
}

func TestNamedEnrichmentEndpoints_SharedContract(t *testing.T) {
	for _, tc := range namedEnrichmentCases() {
		t.Run(tc.name+" missing title is 400", func(t *testing.T) {
			rec := discServe(t, tc.router(nil), http.MethodGet, tc.path+"?kind=artist", nil)
			discAssertStatus(t, rec, http.StatusBadRequest)
			var resp httputil.ErrorResponse
			discDecodeJSON(t, rec, &resp)
			if resp.Code != "discovery.invalid_param" {
				t.Errorf("code = %q, want %q", resp.Code, "discovery.invalid_param")
			}
			if resp.Detail != "title is required" {
				t.Errorf("detail = %q, want %q", resp.Detail, "title is required")
			}
		})

		t.Run(tc.name+" invalid kind is 400", func(t *testing.T) {
			rec := discServe(t, tc.router(nil), http.MethodGet, tc.path+"?kind=albumx&title=Nas", nil)
			assertErrorCode(t, rec, http.StatusBadRequest, "discovery.invalid_kind")
		})

		t.Run(tc.name+" whitespace-only kind is a missing kind", func(t *testing.T) {
			rec := discServe(t, tc.router(nil), http.MethodGet, tc.path+"?kind=%20&title=Nas", nil)
			assertErrorCode(t, rec, http.StatusBadRequest, "discovery.invalid_param")
		})

		t.Run(tc.name+" nil enricher is 200 with the empty DTO", func(t *testing.T) {
			router := buildEnrichersRouter(discovery.DetailEnrichers{})
			rec := discServe(t, router, http.MethodGet, tc.path+"?kind=artist&title=Nas", nil)
			discAssertStatus(t, rec, http.StatusOK)
			var raw map[string]json.RawMessage
			discDecodeJSON(t, rec, &raw)
			for _, f := range tc.emptyFields {
				if string(raw[f]) != "[]" {
					t.Errorf("%s = %s, want []", f, raw[f])
				}
			}
			if string(raw["degraded"]) != "false" {
				t.Errorf("degraded = %s, want false", raw["degraded"])
			}
		})

		t.Run(tc.name+" ErrDegraded is 200 with degraded true", func(t *testing.T) {
			rec := discServe(t, tc.router(discovery.ErrEnrichDegraded), http.MethodGet, tc.path+"?kind=album&title=Illmatic&subtitle=Nas", nil)
			discAssertStatus(t, rec, http.StatusOK)
			var raw map[string]json.RawMessage
			discDecodeJSON(t, rec, &raw)
			if string(raw["degraded"]) != "true" {
				t.Errorf("degraded = %s, want true", raw["degraded"])
			}
		})
	}
}
