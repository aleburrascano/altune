package handler

import (
	"altune/go-api/internal/discovery/service/enrich"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"testing"

	discdomain "altune/go-api/internal/discovery/domain"

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
