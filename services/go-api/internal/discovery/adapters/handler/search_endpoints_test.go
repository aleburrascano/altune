package handler

import (
	"altune/go-api/internal/auth"
	discdomain "altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/discovery/ports"
	"altune/go-api/internal/discovery/service"
	"altune/go-api/internal/shared/httputil"
	"altune/go-api/internal/shared/logging"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

// classifiedTestError is a service failure that carries its own HTTP status
// and machine-readable code through the httputil StatusError/ErrorCoder
// contract.
type classifiedTestError struct{}

func (classifiedTestError) Error() string     { return "history store unavailable" }
func (classifiedTestError) HTTPStatus() int   { return http.StatusServiceUnavailable }
func (classifiedTestError) ErrorCode() string { return "history_unavailable" }

func TestSearchEndpoints_ServiceErrorsUseTypedContract(t *testing.T) {
	classified := classifiedTestError{}
	unclassified := errors.New("boom")

	cases := []struct {
		name       string
		router     func(err error) chi.Router
		method     string
		path       string
		err        error
		wantStatus int
		wantCode   string
	}{
		{
			name:       "suggest classified failure",
			router:     func(err error) chi.Router { return buildSuggestRouter(&fakeVocabStore{err: err}) },
			method:     http.MethodGet,
			path:       "/discovery/suggest?q=kend",
			err:        classified,
			wantStatus: http.StatusServiceUnavailable,
			wantCode:   "history_unavailable",
		},
		{
			name:       "suggest unclassified failure",
			router:     func(err error) chi.Router { return buildSuggestRouter(&fakeVocabStore{err: err}) },
			method:     http.MethodGet,
			path:       "/discovery/suggest?q=kend",
			err:        unclassified,
			wantStatus: http.StatusInternalServerError,
			wantCode:   "internal",
		},
		{
			name: "search history classified failure",
			router: func(err error) chi.Router {
				return buildDiscoveryRouter(nil, &fakeSearchHistoryRepo{err: err}, nil, nil)
			},
			method:     http.MethodGet,
			path:       "/discovery/search-history?limit=10",
			err:        classified,
			wantStatus: http.StatusServiceUnavailable,
			wantCode:   "history_unavailable",
		},
		{
			name: "clear search history classified failure",
			router: func(err error) chi.Router {
				return buildDiscoveryRouter(nil, &fakeSearchHistoryRepo{err: err}, nil, nil)
			},
			method:     http.MethodDelete,
			path:       "/discovery/search-history",
			err:        classified,
			wantStatus: http.StatusServiceUnavailable,
			wantCode:   "history_unavailable",
		},
		{
			name: "clear search history unclassified failure",
			router: func(err error) chi.Router {
				return buildDiscoveryRouter(nil, &fakeSearchHistoryRepo{err: err}, nil, nil)
			},
			method:     http.MethodDelete,
			path:       "/discovery/search-history",
			err:        unclassified,
			wantStatus: http.StatusInternalServerError,
			wantCode:   "internal",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			router := tc.router(tc.err)
			rec := discServe(t, router, tc.method, tc.path, nil)

			discAssertStatus(t, rec, tc.wantStatus)
			discAssertJSON(t, rec)
			var resp httputil.ErrorResponse
			discDecodeJSON(t, rec, &resp)
			if resp.Code != tc.wantCode {
				t.Errorf("code = %q, want %q (body detail: %q)", resp.Code, tc.wantCode, resp.Detail)
			}
		})
	}
}

// mobileOutboxEventID is the shape apps/mobile/src/shared/telemetry/outbox.ts
// makeEventId mints for every label-critical event: a lower-case RFC 4122 v4.
const mobileOutboxEventID = "3f2b8c1e-9a4d-4e6f-b1c2-7d8e9f0a1b2c"

// Regression for #1093: a label-critical event without a parseable event_id
// used to be stored with a NULL event_id, which the partial unique index never
// dedups, so a retry or double-fire inserted a second row. Every such request
// is now rejected before it reaches the store.
func TestHandleRecordEvent_LabelCriticalRequiresValidEventID(t *testing.T) {
	cases := []struct {
		name    string
		eventID any
	}{
		{"missing", nil},
		{"empty", ""},
		{"not a uuid", "retry-1"},
		{"truncated uuid", "3f2b8c1e-9a4d-4e6f-b1c2"},
		{"nil uuid", "00000000-0000-0000-0000-000000000000"},
	}
	for _, eventType := range []string{"library_add", "wrong_album"} {
		for _, tc := range cases {
			t.Run(eventType+"/"+tc.name, func(t *testing.T) {
				store := &recordingEventStore{}
				router := buildEventRouter(store)
				body := map[string]any{"type": eventType, "payload": map[string]any{"result_signature": "sig"}}
				if tc.eventID != nil {
					body["event_id"] = tc.eventID
				}

				for range 2 {
					rec := discServe(t, router, http.MethodPost, "/discovery/events", discJsonBody(t, body))
					discAssertStatus(t, rec, http.StatusBadRequest)
				}
				if got := len(store.events); got != 0 {
					t.Errorf("stored %d events, want 0 (an un-dedupable critical event must not be stored)", got)
				}
			})
		}
	}
}

func TestHandleRecordEvent_MalformedEventIDRejectedForEveryType(t *testing.T) {
	for _, eventType := range []string{"play", "skip", "completed", "result_clicked", "results_shown", "playback_health"} {
		t.Run(eventType, func(t *testing.T) {
			store := &recordingEventStore{}
			router := buildEventRouter(store)
			body := map[string]any{"type": eventType, "event_id": "not-a-uuid"}

			rec := discServe(t, router, http.MethodPost, "/discovery/events", discJsonBody(t, body))

			discAssertStatus(t, rec, http.StatusBadRequest)
			if got := len(store.events); got != 0 {
				t.Errorf("stored %d events, want 0", got)
			}
		})
	}
}

// The mobile client sends play/skip/completed and the other fire-and-forget
// events through useRecordEvent with no event_id at all, and only
// library_add/wrong_album through the outbox with a minted one. Every shape a
// real client sends must still be accepted, or its telemetry is silently lost.
func TestHandleRecordEvent_AcceptsEveryRealClientShape(t *testing.T) {
	cases := []struct {
		name string
		body map[string]any
	}{
		{"outbox library_add", map[string]any{
			"type": "library_add", "event_id": mobileOutboxEventID,
			"client_occurred_at": "2026-09-15T10:00:00.000Z",
			"payload":            map[string]any{"result_signature": "sig", "session_id": "s"},
		}},
		{"outbox wrong_album", map[string]any{
			"type": "wrong_album", "event_id": mobileOutboxEventID,
			"client_occurred_at": "2026-09-15T10:00:00.000Z",
			"payload":            map[string]any{"result_signature": "sig", "session_id": "s"},
		}},
		{"upper-case uuid", map[string]any{"type": "library_add", "event_id": "3F2B8C1E-9A4D-4E6F-B1C2-7D8E9F0A1B2C"}},
		{"fire-and-forget play without event_id", map[string]any{"type": "play", "payload": map[string]any{"session_id": "s"}}},
		{"fire-and-forget skip without event_id", map[string]any{"type": "skip", "payload": map[string]any{"dwell_ms": 1200}}},
		{"fire-and-forget completed without event_id", map[string]any{"type": "completed"}},
		{"play with a valid event_id", map[string]any{"type": "play", "event_id": mobileOutboxEventID}},
		{"results_shown without event_id", map[string]any{"type": "results_shown"}},
		{"search_failed without event_id", map[string]any{"type": "search_failed", "payload": map[string]any{"source": "search"}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			store := &recordingEventStore{}
			router := buildEventRouter(store)

			rec := discServe(t, router, http.MethodPost, "/discovery/events", discJsonBody(t, tc.body))

			discAssertStatus(t, rec, http.StatusNoContent)
			if got := len(store.events); got != 1 {
				t.Errorf("stored %d events, want 1", got)
			}
		})
	}
}

func TestSearchResultToDTO_PrefersStampedSignature(t *testing.T) {
	preFill := discdomain.ResultSignature(discdomain.SearchResult{
		Kind:  discdomain.ResultKindArtist,
		Title: "Nas",
	})
	sr := discdomain.SearchResult{
		Kind:      discdomain.ResultKindArtist,
		Title:     "Nas",
		Subtitle:  "American rapper",
		Signature: preFill,
	}

	dto := searchResultToDTO(sr)

	if dto.ResultSignature != preFill {
		t.Errorf("ResultSignature = %q, want the stamped pre-fill %q", dto.ResultSignature, preFill)
	}
	if recomputed := discdomain.ResultSignature(sr); dto.ResultSignature == recomputed {
		t.Errorf("wire signature drifted to the post-fill recompute %q", recomputed)
	}
}

func TestSearchResultToDTO_ComputesSignatureFallback(t *testing.T) {
	sr := discdomain.SearchResult{
		Kind:     discdomain.ResultKindTrack,
		Title:    "Hello",
		Subtitle: "Adele",
	}
	dto := searchResultToDTO(sr)
	if want := discdomain.ResultSignature(sr); dto.ResultSignature != want {
		t.Errorf("ResultSignature = %q, want computed fallback %q", dto.ResultSignature, want)
	}
}

func TestSearchResultToDTO_ProjectsMetadataIntoExtras(t *testing.T) {
	sr := discdomain.SearchResult{
		Kind:         discdomain.ResultKindAlbum,
		Title:        "Illmatic",
		Subtitle:     "Nas",
		Album:        "Illmatic",
		ISRC:         "USIR19400001",
		UPC:          "074643991124",
		MBID:         "abc-123",
		Year:         1994,
		ReleaseDate:  "1994-04-19",
		TrackCount:   10,
		ProviderRank: 3,
		FanCount:     42,
		Extras:       map[string]any{"custom": "keep"},
		Sources: []discdomain.SourceRef{
			{Provider: discdomain.ProviderDeezer, ExternalID: "123", URL: "https://deezer.com/album/123"},
		},
	}

	dto := searchResultToDTO(sr)

	want := map[string]any{
		"custom":       "keep",
		"album":        "Illmatic",
		"isrc":         "USIR19400001",
		"upc":          "074643991124",
		"mbid":         "abc-123",
		"year":         1994,
		"release_date": "1994-04-19",
		"track_count":  10,
		"rank":         int64(3),
		"nb_fan":       int64(42),
	}
	if len(dto.Extras) != len(want) {
		t.Fatalf("Extras has %d keys, want %d: %#v", len(dto.Extras), len(want), dto.Extras)
	}
	for k, v := range want {
		if dto.Extras[k] != v {
			t.Errorf("Extras[%q] = %#v, want %#v", k, dto.Extras[k], v)
		}
	}
	if len(dto.Sources) != 1 || dto.Sources[0].Provider != "deezer" {
		t.Errorf("Sources = %#v, want one deezer source", dto.Sources)
	}
}

func TestSearchResultToDTO_ZeroValuedMetadataOmittedFromExtras(t *testing.T) {
	sr := discdomain.SearchResult{
		Kind:  discdomain.ResultKindArtist,
		Title: "Nas",
	}

	dto := searchResultToDTO(sr)

	for _, k := range []string{"album", "isrc", "upc", "mbid", "year", "release_date", "track_count", "rank", "nb_fan"} {
		if _, set := dto.Extras[k]; set {
			t.Errorf("Extras[%q] should be omitted for zero-valued field, got %#v", k, dto.Extras[k])
		}
	}
}

type fakeVocabStore struct {
	entries       []discdomain.VocabularyEntry
	err           error
	capturedLimit int
}

func (s *fakeVocabStore) Add(context.Context, discdomain.VocabularyEntry) error       { return nil }
func (s *fakeVocabStore) BulkAdd(context.Context, []discdomain.VocabularyEntry) error { return nil }
func (s *fakeVocabStore) SuggestByPrefix(_ context.Context, _ string, limit int) ([]discdomain.VocabularyEntry, error) {
	s.capturedLimit = limit
	if s.err != nil {
		return nil, s.err
	}
	return s.entries, nil
}

func (s *fakeVocabStore) FindClosest(context.Context, string, int) ([]discdomain.VocabularyEntry, error) {
	return nil, nil
}

type recordingEventStore struct {
	events []discdomain.InteractionEvent
	err    error
}

func (s *recordingEventStore) Append(_ context.Context, e discdomain.InteractionEvent) error {
	if s.err != nil {
		return s.err
	}
	s.events = append(s.events, e)
	return nil
}

func buildSuggestRouter(vocab *fakeVocabStore) chi.Router {
	h := NewDiscoveryHandler(DiscoveryServices{Suggest: service.NewSuggestService(vocab)})
	r := chi.NewRouter()
	r.Use(auth.Middleware(discVerifyAsTestUser))
	r.Mount("/discovery", h.Routes())
	return r
}

func buildEventRouter(store *recordingEventStore) chi.Router {
	h := NewDiscoveryHandler(DiscoveryServices{Event: service.NewRecordEventService(store)})
	r := chi.NewRouter()
	r.Use(auth.Middleware(discVerifyAsTestUser))
	r.Use(httputil.MaxBodySize(1 << 20))
	r.Mount("/discovery", h.Routes())
	return r
}

func TestHandleSuggest(t *testing.T) {
	t.Run("missing q returns 400", func(t *testing.T) {
		router := buildSuggestRouter(&fakeVocabStore{})
		rec := discServe(t, router, http.MethodGet, "/discovery/suggest", nil)
		discAssertStatus(t, rec, http.StatusBadRequest)
	})

	t.Run("whitespace-only q returns 400", func(t *testing.T) {
		router := buildSuggestRouter(&fakeVocabStore{})
		rec := discServe(t, router, http.MethodGet, "/discovery/suggest?q=%20%20", nil)
		discAssertStatus(t, rec, http.StatusBadRequest)
	})

	t.Run("valid query maps vocabulary entries", func(t *testing.T) {
		vocab := &fakeVocabStore{entries: []discdomain.VocabularyEntry{
			{Term: "Kendrick Lamar", TermNorm: "kendrick lamar", Kind: discdomain.VocabKindArtist, Popularity: 99},
		}}
		router := buildSuggestRouter(vocab)
		rec := discServe(t, router, http.MethodGet, "/discovery/suggest?q=kend", nil)
		discAssertStatus(t, rec, http.StatusOK)
		discAssertJSON(t, rec)

		var resp SuggestResponse
		discDecodeJSON(t, rec, &resp)
		if len(resp.Suggestions) != 1 {
			t.Fatalf("len(Suggestions) = %d, want 1", len(resp.Suggestions))
		}
		got := resp.Suggestions[0]
		if got.Text != "Kendrick Lamar" || got.Popularity != 99 {
			t.Errorf("suggestion = %+v", got)
		}
	})

	t.Run("limit boundaries clamp to default 5", func(t *testing.T) {
		cases := []struct {
			query string
			want  int
		}{
			{"/discovery/suggest?q=x", 5},
			{"/discovery/suggest?q=x&limit=0", 5},
			{"/discovery/suggest?q=x&limit=-3", 5},
			{"/discovery/suggest?q=x&limit=11", 5},
			{"/discovery/suggest?q=x&limit=1", 1},
			{"/discovery/suggest?q=x&limit=10", 10},
		}
		for _, c := range cases {
			vocab := &fakeVocabStore{}
			router := buildSuggestRouter(vocab)
			rec := discServe(t, router, http.MethodGet, c.query, nil)
			discAssertStatus(t, rec, http.StatusOK)
			if vocab.capturedLimit != c.want {
				t.Errorf("%s: store limit = %d, want %d", c.query, vocab.capturedLimit, c.want)
			}
		}
	})

	t.Run("empty result emits non-null suggestions array", func(t *testing.T) {
		router := buildSuggestRouter(&fakeVocabStore{})
		rec := discServe(t, router, http.MethodGet, "/discovery/suggest?q=zzz", nil)
		discAssertStatus(t, rec, http.StatusOK)

		var raw map[string]json.RawMessage
		discDecodeJSON(t, rec, &raw)
		if string(raw["suggestions"]) == "null" {
			t.Error("suggestions must be [] when empty, got null")
		}
	})

	t.Run("store error returns 500", func(t *testing.T) {
		router := buildSuggestRouter(&fakeVocabStore{err: context.DeadlineExceeded})
		rec := discServe(t, router, http.MethodGet, "/discovery/suggest?q=x", nil)
		discAssertStatus(t, rec, http.StatusInternalServerError)
	})

	t.Run("no auth returns 401", func(t *testing.T) {
		router := buildSuggestRouter(&fakeVocabStore{})
		rec := discServeNoAuth(t, router, http.MethodGet, "/discovery/suggest?q=x")
		discAssertStatus(t, rec, http.StatusUnauthorized)
	})
}

func TestHandleSuggest_StoreErrorDoesNotLogQueryText(t *testing.T) {
	prev := slog.Default()
	defer slog.SetDefault(prev)
	ring := logging.Setup("error", false)

	const secret = "kendricksecretunreleasedcut"
	router := buildSuggestRouter(&fakeVocabStore{err: context.DeadlineExceeded})
	rec := discServe(t, router, http.MethodGet, "/discovery/suggest?q="+secret, nil)
	discAssertStatus(t, rec, http.StatusInternalServerError)

	sawFailureLog := false
	for _, r := range ring.Snapshot() {
		if r.Message == "suggest failed" {
			sawFailureLog = true
		}
		if strings.Contains(r.Message, secret) {
			t.Errorf("query text leaked into ring message: %q", r.Message)
		}
		for k, v := range r.Attrs {
			if strings.Contains(v, secret) {
				t.Errorf("query text leaked into ring attr %q = %q", k, v)
			}
		}
	}
	if !sawFailureLog {
		t.Fatal("expected the suggest failure to be logged, so the redaction assertion is meaningful")
	}
}

func TestHandleSearch_LimitBoundaries(t *testing.T) {
	cases := []struct {
		name       string
		query      string
		wantStatus int
	}{
		{"absent defaults", "/discovery/search?q=x", http.StatusOK},
		{"lower bound 1", "/discovery/search?q=x&limit=1", http.StatusOK},
		{"upper bound 50", "/discovery/search?q=x&limit=50", http.StatusOK},
		{"51 exceeds domain cap", "/discovery/search?q=x&limit=51", http.StatusBadRequest},
		{"zero defaults", "/discovery/search?q=x&limit=0", http.StatusOK},
		{"negative defaults", "/discovery/search?q=x&limit=-1", http.StatusOK},
		{"non-numeric rejects", "/discovery/search?q=x&limit=abc", http.StatusBadRequest},
		{"overflow-huge exceeds cap", "/discovery/search?q=x&limit=99999999999999999999", http.StatusBadRequest},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			provider := &fakeSearchProvider{name: discdomain.ProviderDeezer}
			router := buildDiscoveryRouter(provider, &fakeSearchHistoryRepo{}, nil, nil)
			rec := discServe(t, router, http.MethodGet, c.query, nil)
			discAssertStatus(t, rec, c.wantStatus)
		})
	}
}

func TestHandleSearch_KindsParsing(t *testing.T) {
	cases := []struct {
		name       string
		query      string
		wantStatus int
	}{
		{"single valid kind", "/discovery/search?q=x&kinds=artist", http.StatusOK},
		{"playlist is a valid kind", "/discovery/search?q=x&kinds=playlist", http.StatusOK},
		{"mixed valid and invalid rejects", "/discovery/search?q=x&kinds=track,bogus", http.StatusBadRequest},
		{"only empty entries default", "/discovery/search?q=x&kinds=,,", http.StatusOK},
		{"whitespace-padded entries trim", "/discovery/search?q=x&kinds=%20track%20,album", http.StatusOK},
		{"all invalid rejects", "/discovery/search?q=x&kinds=foo,bar", http.StatusBadRequest},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			provider := &fakeSearchProvider{name: discdomain.ProviderDeezer}
			router := buildDiscoveryRouter(provider, &fakeSearchHistoryRepo{}, nil, nil)
			rec := discServe(t, router, http.MethodGet, c.query, nil)
			discAssertStatus(t, rec, c.wantStatus)
		})
	}
}

func TestHandleSearch_UnicodeQueryEchoedDecoded(t *testing.T) {
	provider := &fakeSearchProvider{name: discdomain.ProviderDeezer}
	router := buildDiscoveryRouter(provider, &fakeSearchHistoryRepo{}, nil, nil)

	rec := discServe(t, router, http.MethodGet, "/discovery/search?q=Beyonc%C3%A9%20%26%20Jay", nil)
	discAssertStatus(t, rec, http.StatusOK)

	var resp DiscoverySearchResponse
	discDecodeJSON(t, rec, &resp)
	if resp.Query != "Beyoncé & Jay" {
		t.Errorf("Query = %q, want the URL-decoded unicode query", resp.Query)
	}
	if resp.QueryNorm == "" {
		t.Error("expected non-empty query_norm")
	}
}

func TestHandleSearch_SaveHistoryFalseSkipsHistory(t *testing.T) {
	provider := &fakeSearchProvider{name: discdomain.ProviderDeezer}
	historyRepo := &fakeSearchHistoryRepo{}
	router := buildDiscoveryRouter(provider, historyRepo, nil, nil)

	rec := discServe(t, router, http.MethodGet, "/discovery/search?q=quiet&save_history=false", nil)
	discAssertStatus(t, rec, http.StatusOK)
	if len(historyRepo.entries) != 0 {
		t.Errorf("history entries = %d, want 0 with save_history=false", len(historyRepo.entries))
	}
}

func TestHandleSearch_AllProvidersFailedReturns503(t *testing.T) {
	provider := &fakeSearchProvider{name: discdomain.ProviderDeezer, err: context.DeadlineExceeded}
	router := buildDiscoveryRouter(provider, &fakeSearchHistoryRepo{}, nil, nil)

	rec := discServe(t, router, http.MethodGet, "/discovery/search?q=x", nil)
	discAssertStatus(t, rec, http.StatusServiceUnavailable)

	var resp DiscoverySearchResponse
	discDecodeJSON(t, rec, &resp)
	if len(resp.Providers) == 0 {
		t.Fatal("expected provider statuses in the 503 envelope")
	}
	for _, p := range resp.Providers {
		if p.Status == "ok" {
			t.Errorf("provider %s reports ok in an all-failed scatter", p.Provider)
		}
	}
}

func TestHandleSearchHistory_UTCTimestampFormat(t *testing.T) {
	cest := time.FixedZone("CEST", 2*3600)
	historyRepo := &fakeSearchHistoryRepo{entries: []*discdomain.SearchHistoryEntry{{
		ID:         uuid.New(),
		UserId:     discTestUserId,
		Query:      "Radiohead",
		QueryNorm:  "radiohead",
		ExecutedAt: time.Date(2026, 7, 24, 15, 30, 45, 123_000_000, cest),
	}}}
	router := buildDiscoveryRouter(nil, historyRepo, nil, nil)

	rec := discServe(t, router, http.MethodGet, "/discovery/search-history?limit=10", nil)
	discAssertStatus(t, rec, http.StatusOK)

	var resp httputil.List[SearchHistoryItemDTO]
	discDecodeJSON(t, rec, &resp)
	if len(resp.Items) != 1 {
		t.Fatalf("len(Items) = %d, want 1", len(resp.Items))
	}
	if got, want := resp.Items[0].ExecutedAt, "2026-07-24T13:30:45.123Z"; got != want {
		t.Errorf("ExecutedAt = %q, want %q (UTC-converted, millisecond Z layout)", got, want)
	}
}

func TestHandleSearchHistory_RepoErrorReturns500(t *testing.T) {
	historyRepo := &fakeSearchHistoryRepo{err: context.DeadlineExceeded}
	router := buildDiscoveryRouter(nil, historyRepo, nil, nil)

	rec := discServe(t, router, http.MethodGet, "/discovery/search-history", nil)
	discAssertStatus(t, rec, http.StatusInternalServerError)
}

func TestHandleClearSearchHistory_RepoErrorReturns500(t *testing.T) {
	historyRepo := &fakeSearchHistoryRepo{err: context.DeadlineExceeded}
	router := buildDiscoveryRouter(nil, historyRepo, nil, nil)

	rec := discServe(t, router, http.MethodDelete, "/discovery/search-history", nil)
	discAssertStatus(t, rec, http.StatusInternalServerError)
}

func TestHandleClearSearchHistory_NoAuthReturns401(t *testing.T) {
	router := buildDiscoveryRouter(nil, &fakeSearchHistoryRepo{}, nil, nil)
	rec := discServeNoAuth(t, router, http.MethodDelete, "/discovery/search-history")
	discAssertStatus(t, rec, http.StatusUnauthorized)
}

func TestHandleRecordEvent_StoreErrorReturns500(t *testing.T) {
	router := buildEventRouter(&recordingEventStore{err: context.DeadlineExceeded})

	body := map[string]any{"type": "play"}
	rec := discServe(t, router, http.MethodPost, "/discovery/events", discJsonBody(t, body))
	discAssertStatus(t, rec, http.StatusInternalServerError)
}

func TestHandleRecordEvent_OversizedBodyReturns400(t *testing.T) {
	router := buildEventRouter(&recordingEventStore{})

	oversized := `{"type":"play","query_norm":"` + strings.Repeat("a", 1<<20) + `"}`
	rec := discServe(t, router, http.MethodPost, "/discovery/events", strings.NewReader(oversized))
	discAssertStatus(t, rec, http.StatusBadRequest)
}

func TestHandleRecordEvent_ClientOccurredAt(t *testing.T) {
	t.Run("valid RFC3339 is carried into the event", func(t *testing.T) {
		store := &recordingEventStore{}
		router := buildEventRouter(store)

		body := map[string]any{"type": "play", "client_occurred_at": "2026-07-24T10:00:00Z"}
		rec := discServe(t, router, http.MethodPost, "/discovery/events", discJsonBody(t, body))
		discAssertStatus(t, rec, http.StatusNoContent)

		if len(store.events) != 1 {
			t.Fatalf("stored events = %d, want 1", len(store.events))
		}
		want := time.Date(2026, 7, 24, 10, 0, 0, 0, time.UTC)
		if !store.events[0].ClientOccurredAt.Equal(want) {
			t.Errorf("ClientOccurredAt = %v, want %v", store.events[0].ClientOccurredAt, want)
		}
	})

	t.Run("malformed value is dropped, event still recorded", func(t *testing.T) {
		store := &recordingEventStore{}
		router := buildEventRouter(store)

		body := map[string]any{"type": "play", "client_occurred_at": "yesterday"}
		rec := discServe(t, router, http.MethodPost, "/discovery/events", discJsonBody(t, body))
		discAssertStatus(t, rec, http.StatusNoContent)

		if len(store.events) != 1 {
			t.Fatalf("stored events = %d, want 1", len(store.events))
		}
		if !store.events[0].ClientOccurredAt.IsZero() {
			t.Errorf("ClientOccurredAt = %v, want zero (malformed dropped)", store.events[0].ClientOccurredAt)
		}
	})
}

func TestHandleRecordEvent_ServerReservedTypes(t *testing.T) {
	router := buildEventRouter(&recordingEventStore{})

	for _, reserved := range []string{"search_performed"} {
		body := map[string]any{"type": reserved}
		rec := discServe(t, router, http.MethodPost, "/discovery/events", discJsonBody(t, body))
		if rec.Code != http.StatusBadRequest {
			t.Errorf("type %q: status = %d, want 400 (server-reserved)", reserved, rec.Code)
		}
	}
}

func TestHandleRecordEvent_ResultsShownIsClientSubmittable(t *testing.T) {
	store := &recordingEventStore{}
	router := buildEventRouter(store)

	body := map[string]any{
		"type":      "results_shown",
		"search_id": "9f1c2a3e-0000-4000-8000-000000000001",
		"payload":   map[string]any{"results": []any{}},
	}
	rec := discServe(t, router, http.MethodPost, "/discovery/events", discJsonBody(t, body))
	discAssertStatus(t, rec, http.StatusNoContent)

	if len(store.events) != 1 {
		t.Fatalf("stored events = %d, want 1", len(store.events))
	}
	if store.events[0].Type != discdomain.EventTypeResultsShown {
		t.Errorf("stored type = %v, want results_shown", store.events[0].Type)
	}
}

func TestHandleRecordEvent_SearchFailedIsClientSubmittable(t *testing.T) {
	store := &recordingEventStore{}
	router := buildEventRouter(store)

	body := map[string]any{
		"type":    "search_failed",
		"payload": map[string]any{"source": "suggest", "status": 503, "session_id": "s-1"},
	}
	rec := discServe(t, router, http.MethodPost, "/discovery/events", discJsonBody(t, body))
	discAssertStatus(t, rec, http.StatusNoContent)

	if len(store.events) != 1 {
		t.Fatalf("stored events = %d, want 1", len(store.events))
	}
	if store.events[0].Type != discdomain.EventTypeSearchFailed {
		t.Errorf("stored type = %v, want search_failed", store.events[0].Type)
	}
	if store.events[0].Payload["source"] != "suggest" {
		t.Errorf("stored payload source = %v, want suggest", store.events[0].Payload["source"])
	}
}

func TestHandleRecordEvent_SearchDegradedIsClientSubmittable(t *testing.T) {
	store := &recordingEventStore{}
	router := buildEventRouter(store)

	body := map[string]any{
		"type":       "search_degraded",
		"query_norm": "radiohead",
		"payload": map[string]any{
			"result_count":       3,
			"degraded_providers": []any{map[string]any{"provider": "deezer", "status": "timeout"}},
			"session_id":         "s-1",
		},
	}
	rec := discServe(t, router, http.MethodPost, "/discovery/events", discJsonBody(t, body))
	discAssertStatus(t, rec, http.StatusNoContent)

	if len(store.events) != 1 {
		t.Fatalf("stored events = %d, want 1", len(store.events))
	}
	if store.events[0].Type != discdomain.EventTypeSearchDegraded {
		t.Errorf("stored type = %v, want search_degraded", store.events[0].Type)
	}
}

func TestHandleRecordEvent_PlaybackHealthIsClientSubmittable(t *testing.T) {
	store := &recordingEventStore{}
	router := buildEventRouter(store)

	body := map[string]any{
		"type": "playback_health",
		"payload": map[string]any{
			"prefetch_ok":              20,
			"prefetch_failed_download": 3,
			"presign_ok":               2,
			"presign_failed":           0,
			"session_id":               "s-1",
		},
	}
	rec := discServe(t, router, http.MethodPost, "/discovery/events", discJsonBody(t, body))
	discAssertStatus(t, rec, http.StatusNoContent)

	if len(store.events) != 1 {
		t.Fatalf("stored events = %d, want 1", len(store.events))
	}
	if store.events[0].Type != discdomain.EventTypePlaybackHealth {
		t.Errorf("stored type = %v, want playback_health", store.events[0].Type)
	}
}

func TestHandleRecordEvent_DetailHealthIsClientSubmittable(t *testing.T) {
	store := &recordingEventStore{}
	router := buildEventRouter(store)

	body := map[string]any{
		"type": "detail_health",
		"payload": map[string]any{
			"enrichment_musicbrainz_ok":     12,
			"enrichment_lastfm_failed":      3,
			"content_album_tracks_ok":       5,
			"content_artist_content_failed": 1,
			"session_id":                    "s-1",
		},
	}
	rec := discServe(t, router, http.MethodPost, "/discovery/events", discJsonBody(t, body))
	discAssertStatus(t, rec, http.StatusNoContent)

	if len(store.events) != 1 {
		t.Fatalf("stored events = %d, want 1", len(store.events))
	}
	if store.events[0].Type != discdomain.EventTypeDetailHealth {
		t.Errorf("stored type = %v, want detail_health", store.events[0].Type)
	}
}

// Regression for #1086: query_norm is server-owned (resolved from the
// search_id's search_performed row), so a client-sent value never reaches the
// store for any client-submittable event.
func TestHandleRecordEvent_IgnoresClientQueryNorm(t *testing.T) {
	for _, typ := range []string{"result_clicked", "play", "skip", "completed", "library_add", "wrong_album"} {
		t.Run(typ, func(t *testing.T) {
			store := &recordingEventStore{}
			router := buildEventRouter(store)
			searchID := "6f1c1c1e-0000-4000-8000-000000000001"

			body := map[string]any{
				"type": typ, "query_norm": "forged target", "search_id": searchID,
				"event_id": "6f1c1c1e-0000-4000-8000-000000000002",
			}
			rec := discServe(t, router, http.MethodPost, "/discovery/events", discJsonBody(t, body))
			discAssertStatus(t, rec, http.StatusNoContent)

			if len(store.events) != 1 {
				t.Fatalf("stored events = %d, want 1", len(store.events))
			}
			if got := store.events[0].QueryNorm; got != "" {
				t.Errorf("stored query_norm = %q, want empty (client value must be ignored)", got)
			}
			if got := store.events[0].SearchId; got != searchID {
				t.Errorf("stored search_id = %q, want %q", got, searchID)
			}
		})
	}
}

func TestSearchResultToDTO_TypedFieldsMirroredIntoExtras(t *testing.T) {
	sr := discdomain.SearchResult{
		Kind:          discdomain.ResultKindAlbum,
		Title:         "DAMN.",
		Subtitle:      "Kendrick Lamar",
		ImageURL:      "https://img.example.com/damn.jpg",
		ArtworkSource: "deezer",
		Confidence:    discdomain.ConfidenceHigh,
		ISRC:          "USUM71703085",
		UPC:           "00602557618280",
		MBID:          "mbid-1",
		Year:          2017,
		ReleaseDate:   "2017-04-14",
		TrackCount:    14,
		ProviderRank:  3,
		FanCount:      1_000_000,
		Extras:        map[string]any{"duration": 180},
		Sources: []discdomain.SourceRef{
			{Provider: discdomain.ProviderDeezer, ExternalID: "1", URL: "https://deezer.com/1"},
		},
	}

	dto := searchResultToDTO(sr)

	wantExtras := map[string]any{
		"isrc":         "USUM71703085",
		"upc":          "00602557618280",
		"mbid":         "mbid-1",
		"year":         2017,
		"release_date": "2017-04-14",
		"track_count":  14,
		"duration":     180,
	}
	for k, want := range wantExtras {
		if got, ok := dto.Extras[k]; !ok || got != want {
			t.Errorf("extras[%q] = %v (present=%v), want %v", k, got, ok, want)
		}
	}
	if got, ok := dto.Extras["rank"]; !ok || got != int64(3) {
		t.Errorf("extras[rank] = %v (present=%v), want 3", got, ok)
	}
	if got, ok := dto.Extras["nb_fan"]; !ok || got != int64(1_000_000) {
		t.Errorf("extras[nb_fan] = %v (present=%v), want 1000000", got, ok)
	}
	if dto.ArtworkSource != "deezer" {
		t.Errorf("ArtworkSource = %q, want deezer", dto.ArtworkSource)
	}
}

func TestSearchResultToDTO_ZeroTypedFieldsOmitted(t *testing.T) {
	dto := searchResultToDTO(discdomain.SearchResult{
		Kind:  discdomain.ResultKindTrack,
		Title: "Bare",
	})
	for _, key := range []string{"isrc", "upc", "mbid", "year", "release_date", "track_count", "rank", "nb_fan"} {
		if _, ok := dto.Extras[key]; ok {
			t.Errorf("extras[%q] present for a zero-valued field", key)
		}
	}
}

func TestSearchResultDTO_JSONNeverNullCollections(t *testing.T) {
	dto := searchResultToDTO(discdomain.SearchResult{
		Kind:  discdomain.ResultKindTrack,
		Title: "Bare",
	})
	b, err := json.Marshal(dto)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	s := string(b)
	if !strings.Contains(s, `"sources":[]`) {
		t.Errorf("sources must serialize as [], got %s", s)
	}
	if !strings.Contains(s, `"extras":{}`) {
		t.Errorf("extras must serialize as {}, got %s", s)
	}
}

func TestRelatedGroupsToDTOs(t *testing.T) {
	t.Run("empty is nil to preserve omitempty", func(t *testing.T) {
		if got := relatedGroupsToDTOs(nil); got != nil {
			t.Errorf("relatedGroupsToDTOs(nil) = %v, want nil", got)
		}
		if got := relatedGroupsToDTOs([]discdomain.RelatedGroup{}); got != nil {
			t.Errorf("relatedGroupsToDTOs(empty) = %v, want nil", got)
		}
	})

	t.Run("groups map with items", func(t *testing.T) {
		groups := []discdomain.RelatedGroup{{
			Relationship: "same_artist",
			RelatedTo:    "Nas",
			Items: []discdomain.SearchResult{
				{Kind: discdomain.ResultKindTrack, Title: "N.Y. State of Mind"},
			},
		}}
		dtos := relatedGroupsToDTOs(groups)
		if len(dtos) != 1 {
			t.Fatalf("len = %d, want 1", len(dtos))
		}
		if dtos[0].Relationship != "same_artist" || dtos[0].RelatedTo != "Nas" {
			t.Errorf("group = %+v", dtos[0])
		}
		if len(dtos[0].Items) != 1 || dtos[0].Items[0].Title != "N.Y. State of Mind" {
			t.Errorf("items = %+v", dtos[0].Items)
		}
	})
}

func TestSearchOutcome(t *testing.T) {
	okStatus := discdomain.ProviderSearchResponse{Provider: discdomain.ProviderDeezer, Status: discdomain.ProviderStatusOK}
	errStatus := discdomain.ProviderSearchResponse{Provider: discdomain.ProviderITunes, Status: discdomain.ProviderStatusError}

	cases := []struct {
		name       string
		statuses   []discdomain.ProviderSearchResponse
		wantStatus int
		wantCode   string
	}{
		{"empty scatter is 200", nil, http.StatusOK, ""},
		{"all ok is 200", []discdomain.ProviderSearchResponse{okStatus}, http.StatusOK, ""},
		{"mixed is 200", []discdomain.ProviderSearchResponse{errStatus, okStatus}, http.StatusOK, ""},
		{
			"all failed is 503 with a code",
			[]discdomain.ProviderSearchResponse{errStatus, errStatus},
			http.StatusServiceUnavailable, searchCodeAllProvidersFailed,
		},
	}
	for _, c := range cases {
		gotStatus, gotCode := searchOutcome(c.statuses)
		if gotStatus != c.wantStatus || gotCode != c.wantCode {
			t.Errorf("%s: searchOutcome = %d %q, want %d %q", c.name, gotStatus, gotCode, c.wantStatus, c.wantCode)
		}
	}
}

func TestHandleSearchHistory_LimitClamping(t *testing.T) {
	cases := []struct {
		name      string
		query     string
		wantLimit int
	}{
		{"absent limit uses default 10", "", 10},
		{"explicit limit passes through", "?limit=7", 7},
		{"limit at cap passes through", "?limit=100", 100},
		{"huge limit clamps to cap 100", "?limit=1000000000", 100},
		{"non-positive limit falls back to default", "?limit=-5", 10},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			historyRepo := &fakeSearchHistoryRepo{}
			router := buildDiscoveryRouter(nil, historyRepo, nil, nil)

			rec := discServe(t, router, http.MethodGet, "/discovery/search-history"+c.query, nil)
			discAssertStatus(t, rec, http.StatusOK)
			if historyRepo.lastLimit != c.wantLimit {
				t.Errorf("repo limit = %d, want %d", historyRepo.lastLimit, c.wantLimit)
			}
		})
	}
}

// countingSearchProvider records every fan-out call it receives.
type countingSearchProvider struct {
	fakeSearchProvider
	calls atomic.Int32
}

func (p *countingSearchProvider) Search(ctx context.Context, q string, k map[discdomain.ResultKind]bool) ([]discdomain.SearchResult, error) {
	p.calls.Add(1)
	return p.fakeSearchProvider.Search(ctx, q, k)
}

// recordingVocabStore records every term written to the shared vocabulary.
type recordingVocabStore struct {
	fakeVocabStore
	mu    sync.Mutex
	terms []string
}

func (s *recordingVocabStore) Add(_ context.Context, e discdomain.VocabularyEntry) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.terms = append(s.terms, e.Term)
	return nil
}

func (s *recordingVocabStore) Trim(context.Context, int) error { return nil }

func (s *recordingVocabStore) written() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.terms...)
}

func buildQueryCapRouter(p ports.SearchProvider, vocab ports.VocabularyStore) chi.Router {
	svc := service.NewService([]ports.SearchProvider{p}, service.NewCircuitBreaker(), service.WithVocabularyStore(vocab))
	h := NewDiscoveryHandler(DiscoveryServices{Search: svc})
	r := chi.NewRouter()
	r.Use(auth.Middleware(discVerifyAsTestUser))
	r.Mount("/discovery", h.Routes())
	return r
}

func newQueryCapFixture() (*countingSearchProvider, *recordingVocabStore, chi.Router) {
	p := &countingSearchProvider{fakeSearchProvider: fakeSearchProvider{
		name: discdomain.ProviderDeezer,
		results: []discdomain.SearchResult{{
			Kind: discdomain.ResultKindTrack, Title: "Song", Subtitle: "Artist",
			Confidence: discdomain.ConfidenceLow,
			Sources: []discdomain.SourceRef{
				{Provider: discdomain.ProviderDeezer, ExternalID: "1", URL: "https://deezer.com/1"},
			},
		}},
	}}
	vocab := &recordingVocabStore{}
	return p, vocab, buildQueryCapRouter(p, vocab)
}

func wordQuery(n int) string {
	return strings.TrimSpace(strings.Repeat("a ", n))
}

// TestHandleSearch_HighTokenQuery_RejectedBeforeFanOutAndVocab guards #1087: a
// query under the rune cap but over the token cap must never reach provider
// fan-out, correction, or the shared vocabulary index.
func TestHandleSearch_HighTokenQuery_RejectedBeforeFanOutAndVocab(t *testing.T) {
	p, vocab, router := newQueryCapFixture()
	raw := wordQuery(discdomain.MaxSearchQueryTokens + 1)
	if len([]rune(raw)) > discdomain.MaxSearchQueryRunes {
		t.Fatalf("fixture must stay under the rune cap to isolate the token cap")
	}

	rec := discServe(t, router, http.MethodGet, "/discovery/search?q="+url.QueryEscape(raw), nil)

	discAssertStatus(t, rec, http.StatusBadRequest)
	time.Sleep(100 * time.Millisecond) // let any stray background ingest land
	if n := p.calls.Load(); n != 0 {
		t.Errorf("provider fan-out calls = %d, want 0", n)
	}
	if terms := vocab.written(); len(terms) != 0 {
		t.Errorf("vocabulary writes = %q, want none", terms)
	}
}

func TestHandleSearch_QueryAtTokenCap_StillSearchesAndIngests(t *testing.T) {
	p, vocab, router := newQueryCapFixture()
	raw := wordQuery(discdomain.MaxSearchQueryTokens)

	rec := discServe(t, router, http.MethodGet, "/discovery/search?q="+url.QueryEscape(raw), nil)

	discAssertStatus(t, rec, http.StatusOK)
	if p.calls.Load() == 0 {
		t.Fatalf("provider was not called for a query at the token cap")
	}
	deadline := time.Now().Add(2 * time.Second)
	for len(vocab.written()) == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	terms := vocab.written()
	want := []string{"Song - Artist", "Artist"}
	if len(terms) != len(want) {
		t.Fatalf("vocabulary writes = %q, want only provider-derived %q", terms, want)
	}
	for i := range want {
		if terms[i] != want[i] {
			t.Errorf("vocabulary writes = %q, want only provider-derived %q", terms, want)
			break
		}
	}
	for _, term := range terms {
		if term == raw {
			t.Errorf("raw query %q must not be ingested into the vocabulary", raw)
		}
	}
}

func buildQueryNormRouter(history *fakeSearchHistoryRepo) chi.Router {
	p := &fakeSearchProvider{
		name: discdomain.ProviderDeezer,
		results: []discdomain.SearchResult{{
			Kind: discdomain.ResultKindTrack, Title: "Song", Subtitle: "Artist",
			Confidence: discdomain.ConfidenceLow,
			Sources: []discdomain.SourceRef{
				{Provider: discdomain.ProviderDeezer, ExternalID: "1", URL: "https://deezer.com/1"},
			},
		}},
	}
	svc := service.NewService([]ports.SearchProvider{p}, service.NewCircuitBreaker(),
		service.WithHistoryRepository(history))
	h := NewDiscoveryHandler(DiscoveryServices{Search: svc})
	r := chi.NewRouter()
	r.Use(auth.Middleware(discVerifyAsTestUser))
	r.Mount("/discovery", h.Routes())
	return r
}

// TestHandleSearch_QueryNorm_MatchesServiceCanonicalValue guards #1085: the
// response's query_norm must be the value the service computed (from the
// cleaned query) and persisted to history, not a re-normalization of the raw
// query string.
func TestHandleSearch_QueryNorm_MatchesServiceCanonicalValue(t *testing.T) {
	cases := []struct{ name, raw string }{
		{"noise stripped", "Humble Official Video"},
		{"trailing feat stripped", "Humble feat."},
		{"lyrics and hd stripped", "Kendrick Lamar - HUMBLE (Lyrics) HD"},
		{"no noise", "  Humble  "},
		{"all noise falls back to raw", "Official Video"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			history := &fakeSearchHistoryRepo{}
			router := buildQueryNormRouter(history)

			rec := discServe(t, router, http.MethodGet, "/discovery/search?q="+url.QueryEscape(tc.raw), nil)

			discAssertStatus(t, rec, http.StatusOK)
			var resp DiscoverySearchResponse
			if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
				t.Fatalf("decode response: %v", err)
			}
			if len(history.entries) != 1 {
				t.Fatalf("history entries = %d, want 1", len(history.entries))
			}
			if got, want := resp.QueryNorm, history.entries[0].QueryNorm; got != want {
				t.Errorf("response query_norm = %q, service canonical queryNorm = %q", got, want)
			}
			if resp.Query != tc.raw {
				t.Errorf("response query = %q, want raw %q", resp.Query, tc.raw)
			}
		})
	}
}
