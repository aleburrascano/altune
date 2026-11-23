package handler_test

import (
	"altune/go-api/internal/discovery"
	"altune/go-api/internal/shared/httputil"
	shared "altune/go-api/internal/shared/kernel"
	"altune/go-api/internal/shared/logging"
	"altune/go-api/internal/shared/telemetry"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"go.opentelemetry.io/otel/log"
	"go.opentelemetry.io/otel/log/embedded"
)

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

			assertErrorCode(t, rec, tc.wantStatus, tc.wantCode)
		})
	}
}

const mobileOutboxEventID = "3f2b8c1e-9a4d-4e6f-b1c2-7d8e9f0a1b2c"

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
	preFill := discovery.ResultSignature(discovery.SearchResult{
		Kind:  discovery.ResultKindArtist,
		Title: "Nas",
	})
	sr := discovery.SearchResult{
		Kind:      discovery.ResultKindArtist,
		Title:     "Nas",
		Subtitle:  "American rapper",
		Signature: preFill,
	}

	dto := servedItem(t, sr)

	if dto.ResultSignature != preFill {
		t.Errorf("ResultSignature = %q, want the stamped pre-fill %q", dto.ResultSignature, preFill)
	}
	if recomputed := discovery.ResultSignature(sr); dto.ResultSignature == recomputed {
		t.Errorf("wire signature drifted to the post-fill recompute %q", recomputed)
	}
}

func TestSearchResultToDTO_ComputesSignatureFallback(t *testing.T) {
	sr := discovery.SearchResult{
		Kind:     discovery.ResultKindTrack,
		Title:    "Hello",
		Subtitle: "Adele",
	}
	dto := servedItem(t, sr)
	if want := discovery.ResultSignature(sr); dto.ResultSignature != want {
		t.Errorf("ResultSignature = %q, want computed fallback %q", dto.ResultSignature, want)
	}
}

func TestSearchResultToDTO_ProjectsMetadataIntoExtras(t *testing.T) {
	sr := discovery.SearchResult{
		Kind:             discovery.ResultKindAlbum,
		Title:            "Illmatic",
		Subtitle:         "Nas",
		Album:            "Illmatic",
		ISRC:             "USIR19400001",
		UPC:              "074643991124",
		ReleaseGroupMBID: "abc-123",
		Year:             1994,
		ReleaseDate:      "1994-04-19",
		TrackCount:       10,
		ProviderRank:     3,
		FanCount:         42,
		Extras:           map[string]any{"custom": "keep"},
		Sources: []discovery.SourceRef{
			{Provider: discovery.ProviderDeezer, ExternalID: "123", URL: "https://deezer.com/album/123"},
		},
	}

	dto := servedItem(t, sr)

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
		if !sameJSON(t, dto.Extras[k], v) {
			t.Errorf("Extras[%q] = %#v, want %#v", k, dto.Extras[k], v)
		}
	}
	if len(dto.Sources) != 1 || dto.Sources[0].Provider != "deezer" {
		t.Errorf("Sources = %#v, want one deezer source", dto.Sources)
	}
}

func TestSearchResultToDTO_ZeroValuedMetadataOmittedFromExtras(t *testing.T) {
	sr := discovery.SearchResult{
		Kind:  discovery.ResultKindArtist,
		Title: "Nas",
	}

	dto := servedItem(t, sr)

	for _, k := range []string{"album", "isrc", "upc", "mbid", "year", "release_date", "track_count", "rank", "nb_fan"} {
		if _, set := dto.Extras[k]; set {
			t.Errorf("Extras[%q] should be omitted for zero-valued field, got %#v", k, dto.Extras[k])
		}
	}
}

type fakeVocabStore struct {
	entries       []discovery.VocabularyEntry
	err           error
	capturedLimit int
}

func (s *fakeVocabStore) Add(context.Context, discovery.VocabularyEntry) error       { return nil }
func (s *fakeVocabStore) BulkAdd(context.Context, []discovery.VocabularyEntry) error { return nil }
func (s *fakeVocabStore) SuggestByPrefix(_ context.Context, _ string, limit int) ([]discovery.VocabularyEntry, error) {
	s.capturedLimit = limit
	if s.err != nil {
		return nil, s.err
	}
	return s.entries, nil
}

func (s *fakeVocabStore) FindClosest(context.Context, string, int) ([]discovery.VocabularyEntry, error) {
	return nil, nil
}

type recordingEventStore struct {
	events []discovery.InteractionEvent
	err    error
}

func (s *recordingEventStore) Append(_ context.Context, e discovery.InteractionEvent) error {
	if s.err != nil {
		return s.err
	}
	s.events = append(s.events, e)
	return nil
}

func buildSuggestRouter(vocab *fakeVocabStore) chi.Router {
	h := discovery.NewDiscoveryHandler(discovery.DiscoveryServices{Suggest: discovery.NewSuggestService(vocab)})
	r := chi.NewRouter()
	r.Use(discAsTestUser)
	r.Mount("/discovery", h.Routes())
	return r
}

func buildEventRouter(store *recordingEventStore) chi.Router {
	h := discovery.NewDiscoveryHandler(discovery.DiscoveryServices{Event: discovery.NewRecordEventService(store)})
	r := chi.NewRouter()
	r.Use(discAsTestUser)
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
		vocab := &fakeVocabStore{entries: []discovery.VocabularyEntry{
			{Term: "Kendrick Lamar", TermNorm: "kendrick lamar", Kind: discovery.VocabKindArtist, Popularity: 99},
		}}
		router := buildSuggestRouter(vocab)
		rec := discServe(t, router, http.MethodGet, "/discovery/suggest?q=kend", nil)
		discAssertStatus(t, rec, http.StatusOK)
		discAssertJSON(t, rec)

		var resp discovery.SuggestResponse
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
		if r.Message == "request.failed" {
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
			provider := &fakeSearchProvider{name: discovery.ProviderDeezer}
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
			provider := &fakeSearchProvider{name: discovery.ProviderDeezer}
			router := buildDiscoveryRouter(provider, &fakeSearchHistoryRepo{}, nil, nil)
			rec := discServe(t, router, http.MethodGet, c.query, nil)
			discAssertStatus(t, rec, c.wantStatus)
		})
	}
}

func TestHandleSearch_UnicodeQueryEchoedDecoded(t *testing.T) {
	provider := &fakeSearchProvider{name: discovery.ProviderDeezer}
	router := buildDiscoveryRouter(provider, &fakeSearchHistoryRepo{}, nil, nil)

	rec := discServe(t, router, http.MethodGet, "/discovery/search?q=Beyonc%C3%A9%20%26%20Jay", nil)
	discAssertStatus(t, rec, http.StatusOK)

	var resp discovery.DiscoverySearchResponse
	discDecodeJSON(t, rec, &resp)
	if resp.Query != "Beyoncé & Jay" {
		t.Errorf("Query = %q, want the URL-decoded unicode query", resp.Query)
	}
	if resp.QueryNorm == "" {
		t.Error("expected non-empty query_norm")
	}
}

func TestHandleSearch_SaveHistoryFalseSkipsHistory(t *testing.T) {
	provider := &fakeSearchProvider{name: discovery.ProviderDeezer}
	historyRepo := &fakeSearchHistoryRepo{}
	router := buildDiscoveryRouter(provider, historyRepo, nil, nil)

	rec := discServe(t, router, http.MethodGet, "/discovery/search?q=quiet&save_history=false", nil)
	discAssertStatus(t, rec, http.StatusOK)
	if len(historyRepo.entries) != 0 {
		t.Errorf("history entries = %d, want 0 with save_history=false", len(historyRepo.entries))
	}
}

func TestHandleSearch_AllProvidersFailedReturns503(t *testing.T) {
	ring := captureLogs(t)
	provider := &fakeSearchProvider{name: discovery.ProviderDeezer, err: context.DeadlineExceeded}
	router := buildDiscoveryRouter(provider, &fakeSearchHistoryRepo{}, nil, nil)

	rec := discServe(t, router, http.MethodGet, "/discovery/search?q="+url.QueryEscape(secretSearchText), nil)

	assertErrorCode(t, rec, http.StatusServiceUnavailable, "discovery.all_providers_failed")
	if _, logged := findRecord(ring, "request.failed"); !logged {
		t.Fatal("expected the all-failed search to be logged, so the redaction assertion is meaningful")
	}
	assertNoSearchText(t, ring)
}

func TestHandleSearchHistory_UTCTimestampFormat(t *testing.T) {
	cest := time.FixedZone("CEST", 2*3600)
	historyRepo := &fakeSearchHistoryRepo{entries: []*discovery.SearchHistoryEntry{{
		ID:         shared.NewEntityId(),
		UserId:     discTestUserId,
		Query:      "Radiohead",
		QueryNorm:  "radiohead",
		ExecutedAt: time.Date(2026, 7, 24, 15, 30, 45, 123_000_000, cest),
	}}}
	router := buildDiscoveryRouter(nil, historyRepo, nil, nil)

	rec := discServe(t, router, http.MethodGet, "/discovery/search-history?limit=10", nil)
	discAssertStatus(t, rec, http.StatusOK)

	var resp httputil.List[discovery.SearchHistoryItemDTO]
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

func TestHandleRecordEvent_OversizedBodyReturns413(t *testing.T) {
	router := buildEventRouter(&recordingEventStore{})

	oversized := `{"type":"play","query_norm":"` + strings.Repeat("a", 1<<20) + `"}`
	rec := discServe(t, router, http.MethodPost, "/discovery/events", strings.NewReader(oversized))
	discAssertStatus(t, rec, http.StatusRequestEntityTooLarge)
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
	if store.events[0].Type != discovery.EventTypeResultsShown {
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
	if store.events[0].Type != discovery.EventTypeSearchFailed {
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
	if store.events[0].Type != discovery.EventTypeSearchDegraded {
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
	if store.events[0].Type != discovery.EventTypePlaybackHealth {
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
	if store.events[0].Type != discovery.EventTypeDetailHealth {
		t.Errorf("stored type = %v, want detail_health", store.events[0].Type)
	}
}

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
	sr := discovery.SearchResult{
		Kind:             discovery.ResultKindAlbum,
		Title:            "DAMN.",
		Subtitle:         "Kendrick Lamar",
		ImageURL:         "https://img.example.com/damn.jpg",
		ArtworkSource:    "deezer",
		Confidence:       discovery.ConfidenceHigh,
		ISRC:             "USUM71703085",
		UPC:              "00602557618280",
		ReleaseGroupMBID: "mbid-1",
		Year:             2017,
		ReleaseDate:      "2017-04-14",
		TrackCount:       14,
		ProviderRank:     3,
		FanCount:         1_000_000,
		Extras:           map[string]any{"duration": 180},
		Sources: []discovery.SourceRef{
			{Provider: discovery.ProviderDeezer, ExternalID: "1", URL: "https://deezer.com/1"},
		},
	}

	dto := servedItem(t, sr)

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
		if got, ok := dto.Extras[k]; !ok || !sameJSON(t, got, want) {
			t.Errorf("extras[%q] = %v (present=%v), want %v", k, got, ok, want)
		}
	}
	if got, ok := dto.Extras["rank"]; !ok || !sameJSON(t, got, 3) {
		t.Errorf("extras[rank] = %v (present=%v), want 3", got, ok)
	}
	if got, ok := dto.Extras["nb_fan"]; !ok || !sameJSON(t, got, 1_000_000) {
		t.Errorf("extras[nb_fan] = %v (present=%v), want 1000000", got, ok)
	}
	if dto.ArtworkSource != "deezer" {
		t.Errorf("ArtworkSource = %q, want deezer", dto.ArtworkSource)
	}
}

func TestSearchResultToDTO_ZeroTypedFieldsOmitted(t *testing.T) {
	dto := servedItem(t, discovery.SearchResult{
		Kind:  discovery.ResultKindTrack,
		Title: "Bare",
	})
	for _, key := range []string{"isrc", "upc", "mbid", "year", "release_date", "track_count", "rank", "nb_fan"} {
		if _, ok := dto.Extras[key]; ok {
			t.Errorf("extras[%q] present for a zero-valued field", key)
		}
	}
}

func TestSearchResultDTO_JSONNeverNullCollections(t *testing.T) {
	s := string(servedItems(t, discovery.SearchResult{
		Kind:  discovery.ResultKindTrack,
		Title: "Bare",
	})[0])
	if !strings.Contains(s, `"sources":[]`) {
		t.Errorf("sources must serialize as [], got %s", s)
	}
	if !strings.Contains(s, `"extras":{}`) {
		t.Errorf("extras must serialize as {}, got %s", s)
	}
}

func searchOnlyRouter(svc *discovery.SearchService) chi.Router {
	h := discovery.NewDiscoveryHandler(discovery.DiscoveryServices{Search: svc})
	r := chi.NewRouter()
	r.Use(discAsTestUser)
	r.Mount("/discovery", h.Routes())
	return r
}

func TestSearch_RelatedGroupsReachTheWire(t *testing.T) {
	artist := discovery.SearchResult{
		Kind:    discovery.ResultKindArtist,
		Title:   "Nas",
		Sources: []discovery.SourceRef{{Provider: discovery.ProviderDeezer, ExternalID: "27"}},
	}
	provider := &fakeSearchProvider{name: discovery.ProviderDeezer, results: []discovery.SearchResult{artist}}

	t.Run("no related groups leaves the key absent", func(t *testing.T) {
		router := searchOnlyRouter(discovery.NewSearchService([]discovery.SearchProvider{provider}, discovery.NewCircuitBreaker()))

		rec := discServe(t, router, http.MethodGet, "/discovery/search?q=nas&save_history=false", nil)

		discAssertStatus(t, rec, http.StatusOK)
		var raw map[string]json.RawMessage
		discDecodeJSON(t, rec, &raw)
		if got, present := raw["related"]; present {
			t.Errorf("related = %s, want the key absent when nothing is related", got)
		}
	})

	t.Run("an artist's albums come back as a group with its items", func(t *testing.T) {
		findRelated := discovery.NewFindRelatedService(nil, nil, stubContentProvider{provider: discovery.ProviderDeezer})
		router := searchOnlyRouter(discovery.NewSearchService([]discovery.SearchProvider{provider}, discovery.NewCircuitBreaker(),
			discovery.WithFindRelatedService(findRelated)))

		rec := discServe(t, router, http.MethodGet, "/discovery/search?q=nas&save_history=false", nil)

		discAssertStatus(t, rec, http.StatusOK)
		var resp discovery.DiscoverySearchResponse
		discDecodeJSON(t, rec, &resp)
		if len(resp.Related) != 1 {
			t.Fatalf("related groups = %+v, want one", resp.Related)
		}
		group := resp.Related[0]
		if group.Relationship != "artist_albums" || group.RelatedTo != "Nas" {
			t.Errorf("group = %+v, want artist_albums related to Nas", group)
		}
		if len(group.Items) != 1 || group.Items[0].Title != "Real Song" || group.Items[0].Kind != "album" {
			t.Errorf("items = %+v, want the artist's one album", group.Items)
		}
	})
}

func TestSearch_OutcomeFollowsTheProviders(t *testing.T) {
	served := &fakeSearchProvider{name: discovery.ProviderDeezer, results: []discovery.SearchResult{{
		Kind:    discovery.ResultKindTrack,
		Title:   "Hello",
		Sources: []discovery.SourceRef{{Provider: discovery.ProviderDeezer, ExternalID: "1"}},
	}}}
	failing := &fakeSearchProvider{name: discovery.ProviderITunes, err: errors.New("upstream down")}
	cases := []struct {
		name       string
		providers  []discovery.SearchProvider
		wantStatus int
		wantCode   string
	}{
		{"empty scatter is 200", nil, http.StatusOK, ""},
		{"all ok is 200", []discovery.SearchProvider{served}, http.StatusOK, ""},
		{"mixed is 200", []discovery.SearchProvider{failing, served}, http.StatusOK, ""},
		{"all failed is 503 with a code", []discovery.SearchProvider{failing}, http.StatusServiceUnavailable, "discovery.all_providers_failed"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			router := searchOnlyRouter(discovery.NewSearchService(c.providers, discovery.NewCircuitBreaker()))

			rec := discServe(t, router, http.MethodGet, "/discovery/search?q=hello&save_history=false", nil)

			var resp struct {
				Code string `json:"code"`
			}
			discDecodeJSON(t, rec, &resp)
			if rec.Code != c.wantStatus || resp.Code != c.wantCode {
				t.Errorf("search = %d %q, want %d %q", rec.Code, resp.Code, c.wantStatus, c.wantCode)
			}
		})
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

type countingSearchProvider struct {
	fakeSearchProvider
	calls atomic.Int32
}

func (p *countingSearchProvider) Search(ctx context.Context, q string, k map[discovery.ResultKind]bool) ([]discovery.SearchResult, error) {
	p.calls.Add(1)
	return p.fakeSearchProvider.Search(ctx, q, k)
}

type recordingVocabStore struct {
	fakeVocabStore
	mu    sync.Mutex
	terms []string
}

func (s *recordingVocabStore) Add(_ context.Context, e discovery.VocabularyEntry) error {
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

func buildQueryCapRouter(p discovery.SearchProvider, vocab discovery.VocabularyStore) chi.Router {
	svc := discovery.NewSearchService([]discovery.SearchProvider{p}, discovery.NewCircuitBreaker(), discovery.WithVocabularyStore(vocab))
	h := discovery.NewDiscoveryHandler(discovery.DiscoveryServices{Search: svc})
	r := chi.NewRouter()
	r.Use(discAsTestUser)
	r.Mount("/discovery", h.Routes())
	return r
}

func newQueryCapFixture() (*countingSearchProvider, *recordingVocabStore, chi.Router) {
	p := &countingSearchProvider{fakeSearchProvider: fakeSearchProvider{
		name: discovery.ProviderDeezer,
		results: []discovery.SearchResult{{
			Kind: discovery.ResultKindTrack, Title: "Song", Subtitle: "Artist",
			Confidence: discovery.ConfidenceLow,
			Sources: []discovery.SourceRef{
				{Provider: discovery.ProviderDeezer, ExternalID: "1", URL: "https://deezer.com/1"},
			},
		}},
	}}
	vocab := &recordingVocabStore{}
	return p, vocab, buildQueryCapRouter(p, vocab)
}

func wordQuery(n int) string {
	return strings.TrimSpace(strings.Repeat("a ", n))
}

func TestHandleSearch_HighTokenQuery_RejectedBeforeFanOutAndVocab(t *testing.T) {
	p, vocab, router := newQueryCapFixture()
	raw := wordQuery(discovery.MaxSearchQueryTokens + 1)
	if len([]rune(raw)) > discovery.MaxSearchQueryRunes {
		t.Fatalf("fixture must stay under the rune cap to isolate the token cap")
	}

	rec := discServe(t, router, http.MethodGet, "/discovery/search?q="+url.QueryEscape(raw), nil)

	discAssertStatus(t, rec, http.StatusBadRequest)
	time.Sleep(100 * time.Millisecond)
	if n := p.calls.Load(); n != 0 {
		t.Errorf("provider fan-out calls = %d, want 0", n)
	}
	if terms := vocab.written(); len(terms) != 0 {
		t.Errorf("vocabulary writes = %q, want none", terms)
	}
}

func TestHandleSearch_QueryAtTokenCap_StillSearchesAndIngests(t *testing.T) {
	p, vocab, router := newQueryCapFixture()
	raw := wordQuery(discovery.MaxSearchQueryTokens)

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
		name: discovery.ProviderDeezer,
		results: []discovery.SearchResult{{
			Kind: discovery.ResultKindTrack, Title: "Song", Subtitle: "Artist",
			Confidence: discovery.ConfidenceLow,
			Sources: []discovery.SourceRef{
				{Provider: discovery.ProviderDeezer, ExternalID: "1", URL: "https://deezer.com/1"},
			},
		}},
	}
	svc := discovery.NewSearchService([]discovery.SearchProvider{p}, discovery.NewCircuitBreaker(),
		discovery.WithHistoryRepository(history))
	h := discovery.NewDiscoveryHandler(discovery.DiscoveryServices{Search: svc})
	r := chi.NewRouter()
	r.Use(discAsTestUser)
	r.Mount("/discovery", h.Routes())
	return r
}

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
			var resp discovery.DiscoverySearchResponse
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

func validationRouter(t *testing.T) chi.Router {
	t.Helper()
	albumProviders := map[discovery.ProviderName]discovery.AlbumContentProvider{
		discovery.ProviderDeezer: &discovery.FakeAlbumContentProvider{Results: seedTracks(3)},
	}
	return buildDiscoveryRouter(
		&fakeSearchProvider{name: discovery.ProviderDeezer}, &fakeSearchHistoryRepo{}, albumProviders, nil)
}

func TestSearchRejection_MissingQIsDistinctFromUnknownKind(t *testing.T) {
	missingQ := rejectionCode(t, discServe(t, validationRouter(t), http.MethodGet, "/discovery/search", nil))
	unknownKind := rejectionCode(t, discServe(t, validationRouter(t), http.MethodGet, "/discovery/search?q=x&kinds=bogus", nil))

	if missingQ == unknownKind {
		t.Errorf("both rejections answered code %q; the causes must be distinguishable", missingQ)
	}
}

func TestSearchRejections_CodeNamesTheCause(t *testing.T) {
	cases := []struct {
		cause    string
		path     string
		wantCode string
	}{
		{"missing q", "/discovery/search", "discovery.q_required"},
		{"whitespace-only q", "/discovery/search?q=%20%20", "discovery.q_required"},
		{"unknown kind", "/discovery/search?q=x&kinds=bogus", "discovery.invalid_kind"},
		{"non-numeric offset", "/discovery/search?q=x&offset=abc", "discovery.invalid_param"},
		{"non-numeric limit", "/discovery/search?q=x&limit=abc", "discovery.invalid_param"},
		{"offset past the cap", "/discovery/search?q=x&offset=100000", "discovery.invalid_param"},
		{"limit past the cap", "/discovery/search?q=x&limit=51", "discovery.invalid_param"},
		{"search_id that is not a uuid", "/discovery/search?q=x&search_id=not-a-uuid", "discovery.invalid_param"},
	}
	for _, c := range cases {
		t.Run(c.cause, func(t *testing.T) {
			rec := discServe(t, validationRouter(t), http.MethodGet, c.path, nil)

			discAssertStatus(t, rec, http.StatusBadRequest)
			if got := rejectionCode(t, rec); got != c.wantCode {
				t.Errorf("code = %q, want %q", got, c.wantCode)
			}
		})
	}
}

func TestSearchPaging_MalformedValueIsRejectedRatherThanDefaulted(t *testing.T) {
	cases := []struct {
		name       string
		path       string
		wantStatus int
	}{
		{"non-numeric offset", "/discovery/search?q=x&offset=abc", http.StatusBadRequest},
		{"non-numeric limit", "/discovery/search?q=x&limit=abc", http.StatusBadRequest},
		{"offset overflows int", "/discovery/search?q=x&offset=99999999999999999999", http.StatusBadRequest},
		{"well-formed paging still serves", "/discovery/search?q=x&offset=10&limit=5", http.StatusOK},
		{"search_id of an expired search still serves", "/discovery/search?q=x&offset=5&search_id=" + uuid.NewString(), http.StatusOK},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rec := discServe(t, validationRouter(t), http.MethodGet, c.path, nil)

			discAssertStatus(t, rec, c.wantStatus)
		})
	}
}

func TestSearch_AllProvidersFailedCarriesCode(t *testing.T) {
	provider := &fakeSearchProvider{name: discovery.ProviderDeezer, err: context.DeadlineExceeded}
	router := buildDiscoveryRouter(provider, &fakeSearchHistoryRepo{}, nil, nil)

	rec := discServe(t, router, http.MethodGet, "/discovery/search?q=x", nil)

	assertErrorCode(t, rec, http.StatusServiceUnavailable, "discovery.all_providers_failed")
}

func TestSearch_ServedScatterCarriesNoCode(t *testing.T) {
	rec := discServe(t, validationRouter(t), http.MethodGet, "/discovery/search?q=x", nil)

	discAssertStatus(t, rec, http.StatusOK)
	var resp map[string]any
	discDecodeJSON(t, rec, &resp)
	if code, has := resp["code"]; has {
		t.Errorf("code = %v, want none on a served search", code)
	}
}

func TestDiscoveryRejections_CodeNamesTheCauseAcrossEndpoints(t *testing.T) {
	cases := []struct {
		cause    string
		method   string
		path     string
		body     any
		wantCode string
	}{
		{
			cause: "unknown content provider", method: http.MethodGet,
			path: "/discovery/albums/bogus/id-1/tracks", wantCode: "discovery.invalid_provider",
		},
		{
			cause: "non-numeric content limit", method: http.MethodGet,
			path: "/discovery/albums/deezer/id-1/tracks?limit=abc", wantCode: "discovery.invalid_param",
		},
		{
			cause: "non-numeric suggest limit", method: http.MethodGet,
			path: "/discovery/suggest?q=x&limit=abc", wantCode: "discovery.invalid_param",
		},
		{
			cause: "non-numeric search-history limit", method: http.MethodGet,
			path: "/discovery/search-history?limit=abc", wantCode: "discovery.invalid_param",
		},
		{
			cause: "enrichment without a kind", method: http.MethodGet,
			path: "/discovery/enrichment?title=DAMN.", wantCode: "discovery.invalid_param",
		},
		{
			cause: "enrichment with an unknown kind", method: http.MethodGet,
			path: "/discovery/enrichment?kind=bogus&title=DAMN.", wantCode: "discovery.invalid_kind",
		},
		{
			cause: "favorite with an unknown kind", method: http.MethodPut,
			path: "/discovery/favorites", body: map[string]any{"kind": "bogus", "title": "DAMN."},
			wantCode: "discovery.invalid_kind",
		},
		{
			cause: "favorite without a title", method: http.MethodPut,
			path: "/discovery/favorites", body: map[string]any{"kind": "album"},
			wantCode: "discovery.invalid_param",
		},
		{
			cause: "event with an unknown type", method: http.MethodPost,
			path: "/discovery/events", body: map[string]any{"type": "bogus"},
			wantCode: "discovery.invalid_event_type",
		},
	}
	for _, c := range cases {
		t.Run(c.cause, func(t *testing.T) {
			var body io.Reader
			if c.body != nil {
				body = discJsonBody(t, c.body)
			}
			rec := discServe(t, validationRouter(t), c.method, c.path, body)

			discAssertStatus(t, rec, http.StatusBadRequest)
			if got := rejectionCode(t, rec); got != c.wantCode {
				t.Errorf("code = %q, want %q", got, c.wantCode)
			}
		})
	}
}

func TestRecordEvent_UnparseableBodyCarriesCode(t *testing.T) {
	rec := discServe(t, validationRouter(t), http.MethodPost, "/discovery/events", strings.NewReader("{not json"))

	discAssertStatus(t, rec, http.StatusBadRequest)
	if got := rejectionCode(t, rec); got != "discovery.invalid_body" {
		t.Errorf("code = %q, want %q", got, "discovery.invalid_body")
	}
}

func eventBodyWithJunk(t *testing.T, junkBytes int) string {
	t.Helper()
	return discJsonBody(t, map[string]any{
		"type":    "play",
		"payload": map[string]any{"junk": strings.Repeat("x", junkBytes)},
	}).String()
}

func TestRecordEvent_OversizedPayloadsAreNeverStored(t *testing.T) {
	store := &recordingEventStore{}
	router := buildEventRouter(store)
	body := eventBodyWithJunk(t, 900<<10)

	for i := range 100 {
		rec := discServe(t, router, http.MethodPost, "/discovery/events", strings.NewReader(body))
		if rec.Code != http.StatusRequestEntityTooLarge && rec.Code != http.StatusTooManyRequests {
			t.Fatalf("request %d: status = %d, want 413 or 429 (body: %s)", i+1, rec.Code, rec.Body.String())
		}
	}

	if len(store.events) != 0 {
		t.Fatalf("store holds %d oversized events, want none", len(store.events))
	}
}

func TestRecordEvent_PayloadOverTheCapAnswersWithTheInvalidEventCode(t *testing.T) {
	router := buildEventRouter(&recordingEventStore{})

	rec := discServe(t, router, http.MethodPost, "/discovery/events", strings.NewReader(eventBodyWithJunk(t, 16<<10)))

	assertErrorCode(t, rec, http.StatusBadRequest, "discovery.invalid_event")
}

func TestRecordEvent_BodyPastTheRouteCapIs413TooLarge(t *testing.T) {
	router := buildEventRouter(&recordingEventStore{})

	rec := discServe(t, router, http.MethodPost, "/discovery/events", strings.NewReader(eventBodyWithJunk(t, 900<<10)))

	assertErrorCode(t, rec, http.StatusRequestEntityTooLarge, "request.too_large")
}

func TestHandleSuggest_OverlongQ(t *testing.T) {
	router := buildSuggestRouter(&fakeVocabStore{})
	rec := discServe(t, router, http.MethodGet, "/discovery/suggest?q="+strings.Repeat("a", 201), nil)
	discAssertStatus(t, rec, http.StatusBadRequest)
}

const secretSearchText = "very private query 7f3a"

func seededHistoryRepo() *fakeSearchHistoryRepo {
	return &fakeSearchHistoryRepo{entries: []*discovery.SearchHistoryEntry{{
		ID:         shared.NewEntityId(),
		UserId:     discTestUserId,
		Query:      secretSearchText,
		QueryNorm:  secretSearchText,
		ExecutedAt: time.Now().UTC(),
	}}}
}

func captureLogs(t *testing.T) *logging.RingBuffer {
	t.Helper()
	prev := slog.Default()
	t.Cleanup(func() { slog.SetDefault(prev) })
	return logging.Setup("info", false)
}

func findRecord(ring *logging.RingBuffer, msg string) (logging.CapturedRecord, bool) {
	for _, r := range ring.Snapshot() {
		if r.Message == msg {
			return r, true
		}
	}
	return logging.CapturedRecord{}, false
}

func assertNoSearchText(t *testing.T, ring *logging.RingBuffer) {
	t.Helper()
	for _, r := range ring.Snapshot() {
		if strings.Contains(r.Message, secretSearchText) {
			t.Fatalf("log message leaks search text: %q", r.Message)
		}
		for k, v := range r.Attrs {
			if strings.Contains(v, secretSearchText) {
				t.Fatalf("log attr %s leaks search text: %q", k, v)
			}
		}
	}
}

func TestHandleClearSearchHistory_AuditsSuccess(t *testing.T) {
	ring := captureLogs(t)
	router := buildDiscoveryRouter(nil, seededHistoryRepo(), nil, nil)

	before := time.Now().UTC()
	rec := discServe(t, router, http.MethodDelete, "/discovery/search-history", nil)
	discAssertStatus(t, rec, http.StatusNoContent)

	r, ok := findRecord(ring, "discovery.search_history_cleared")
	if !ok {
		t.Fatalf("no discovery.search_history_cleared audit record; logs: %v", ring.Snapshot())
	}
	if r.Level != "INFO" {
		t.Errorf("level = %q, want INFO", r.Level)
	}
	if r.Attrs["user_id"] != discTestUserId.String() {
		t.Errorf("user_id = %q, want %q", r.Attrs["user_id"], discTestUserId.String())
	}
	if r.Attrs["action"] != "clear_search_history" {
		t.Errorf("action = %q, want clear_search_history", r.Attrs["action"])
	}
	at, err := time.Parse("2006-01-02 15:04:05.999999999 -0700 MST", r.Attrs["at"])
	if err != nil {
		t.Fatalf("at = %q is not a timestamp: %v", r.Attrs["at"], err)
	}
	if at.Before(before.Add(-time.Second)) || at.After(time.Now().UTC().Add(time.Second)) {
		t.Errorf("at = %v, want around request time %v", at, before)
	}
	assertNoSearchText(t, ring)
}

func TestHandleClearSearchHistory_AuditsFailure(t *testing.T) {
	ring := captureLogs(t)
	repo := seededHistoryRepo()
	repo.err = errors.New("db unavailable")
	router := buildDiscoveryRouter(nil, repo, nil, nil)

	rec := discServe(t, router, http.MethodDelete, "/discovery/search-history", nil)
	discAssertStatus(t, rec, http.StatusInternalServerError)

	r, ok := findRecord(ring, "clear search history failed")
	if !ok {
		t.Fatalf("no clear search history failed record; logs: %v", ring.Snapshot())
	}
	if r.Attrs["user_id"] != discTestUserId.String() {
		t.Errorf("user_id = %q, want %q", r.Attrs["user_id"], discTestUserId.String())
	}
	if r.Attrs["action"] != "clear_search_history" {
		t.Errorf("action = %q, want clear_search_history", r.Attrs["action"])
	}
	if _, ok := findRecord(ring, "discovery.search_history_cleared"); ok {
		t.Error("success audit record emitted for a failed clear")
	}
	assertNoSearchText(t, ring)
}

type driftingSearchProvider struct {
	slates [][]discovery.SearchResult
	calls  int
}

func (p *driftingSearchProvider) Name() discovery.ProviderName { return discovery.ProviderDeezer }

func (p *driftingSearchProvider) Search(_ context.Context, _ string, _ map[discovery.ResultKind]bool) ([]discovery.SearchResult, error) {
	slate := p.slates[min(p.calls, len(p.slates)-1)]
	p.calls++
	return slate, nil
}

func (p *driftingSearchProvider) SupportedKinds() map[discovery.ResultKind]bool {
	return map[discovery.ResultKind]bool{discovery.ResultKindTrack: true}
}

type mapResultCache struct {
	entries map[string][]discovery.SearchResult
}

func (c *mapResultCache) Get(_ context.Context, key string) ([]discovery.SearchResult, bool) {
	results, hit := c.entries[key]
	return results, hit
}

func (c *mapResultCache) Set(_ context.Context, key string, results []discovery.SearchResult) {
	c.entries[key] = results
}

func searchSlate(prefix string, n int) []discovery.SearchResult {
	out := make([]discovery.SearchResult, n)
	for i := range out {
		out[i] = discovery.NewProviderResult(
			discovery.ResultKindTrack, "Humble", prefix+string(rune('a'+i)), "",
			discovery.SourceRef{Provider: discovery.ProviderDeezer, ExternalID: prefix + string(rune('a'+i))}, nil)
	}
	return out
}

func pagingRouter(provider discovery.SearchProvider) chi.Router {
	down := &fakeSearchProvider{name: discovery.ProviderITunes, err: context.DeadlineExceeded}
	searchSvc := discovery.NewSearchService(
		[]discovery.SearchProvider{provider, down},
		discovery.NewCircuitBreaker(),
		discovery.WithResultCache(&mapResultCache{entries: map[string][]discovery.SearchResult{}}),
	)
	h := discovery.NewDiscoveryHandler(discovery.DiscoveryServices{Search: searchSvc})
	r := chi.NewRouter()
	r.Use(discAsTestUser)
	r.Mount("/discovery", h.Routes())
	return r
}

func searchPageOverHTTP(t *testing.T, router chi.Router, path string) discovery.DiscoverySearchResponse {
	t.Helper()
	rec := discServe(t, router, http.MethodGet, path, nil)
	discAssertStatus(t, rec, http.StatusOK)
	var resp discovery.DiscoverySearchResponse
	discDecodeJSON(t, rec, &resp)
	return resp
}

func TestHandleSearch_SearchIdKeepsTheNextPageInTheSameRanking(t *testing.T) {
	router := pagingRouter(&driftingSearchProvider{
		slates: [][]discovery.SearchResult{searchSlate("first-", 12), searchSlate("second-", 12)},
	})

	first := searchPageOverHTTP(t, router, "/discovery/search?q=humble&kinds=track&limit=5")
	second := searchPageOverHTTP(t, router,
		"/discovery/search?q=humble&kinds=track&limit=5&offset=5&search_id="+first.SearchID)

	seen := map[string]bool{}
	for _, r := range append(first.Results, second.Results...) {
		if seen[r.ResultSignature] {
			t.Fatalf("result %q served on both pages", r.Subtitle)
		}
		seen[r.ResultSignature] = true
	}
	if len(seen) != 10 {
		t.Fatalf("two pages of 5 served %d distinct results", len(seen))
	}
	for _, r := range second.Results {
		if r.Sources[0].ExternalID[:6] != "first-" {
			t.Fatalf("page two served %q, which the first page's ranking never held", r.Sources[0].ExternalID)
		}
	}
	if second.SearchID != first.SearchID {
		t.Errorf("search_id = %q on page two, want the continued %q", second.SearchID, first.SearchID)
	}
	if second.Total != first.Total {
		t.Errorf("total = %d on page two, %d on page one", second.Total, first.Total)
	}
}

func TestHandleRecordEvent_UserTelemetryEnvelopesAreAccepted(t *testing.T) {
	cases := []struct {
		name    string
		body    map[string]any
		want    discovery.EventType
		payload map[string]any
	}{
		{
			name: "user_action tapped, minimal",
			body: map[string]any{"type": "user_action", "payload": map[string]any{
				"action": "library.retry", "outcome": "tapped", "session_id": "s-1",
			}},
			want:    discovery.EventTypeUserAction,
			payload: map[string]any{"action": "library.retry", "outcome": "tapped"},
		},
		{
			name: "user_action failed, every optional field",
			body: map[string]any{"type": "user_action", "payload": map[string]any{
				"action": "detail.save", "outcome": "failed", "track_id": "t-1",
				"status": 503, "correlation_id": "corr-1",
				"error": strings.Repeat("x", 300), "session_id": "s-1",
			}},
			want: discovery.EventTypeUserAction,
			payload: map[string]any{
				"action": "detail.save", "outcome": "failed", "track_id": "t-1",
				"correlation_id": "corr-1", "error": strings.Repeat("x", 300),
			},
		},
		{
			name: "failure_shown with track",
			body: map[string]any{"type": "failure_shown", "payload": map[string]any{
				"surface": "alert.delete_track", "message": "could not delete",
				"track_id": "t-2", "session_id": "s-1",
			}},
			want:    discovery.EventTypeFailureShown,
			payload: map[string]any{"surface": "alert.delete_track", "message": "could not delete", "track_id": "t-2"},
		},
		{
			name: "failure_shown via the outbox envelope",
			body: map[string]any{
				"type": "failure_shown", "event_id": mobileOutboxEventID,
				"client_occurred_at": "2026-09-15T10:00:00.000Z",
				"payload": map[string]any{
					"surface": "banner.track_status", "message": "ünicode 山 🎵", "session_id": "s-1",
				},
			},
			want:    discovery.EventTypeFailureShown,
			payload: map[string]any{"surface": "banner.track_status", "message": "ünicode 山 🎵"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			store := &recordingEventStore{}
			router := buildEventRouter(store)

			rec := discServe(t, router, http.MethodPost, "/discovery/events", discJsonBody(t, tc.body))

			discAssertStatus(t, rec, http.StatusNoContent)
			if len(store.events) != 1 {
				t.Fatalf("stored %d events, want 1", len(store.events))
			}
			got := store.events[0]
			if got.Type != tc.want {
				t.Errorf("stored type = %v, want %v", got.Type, tc.want)
			}
			for k, v := range tc.payload {
				if got.Payload[k] != v {
					t.Errorf("payload[%q] = %#v, want %#v", k, got.Payload[k], v)
				}
			}
		})
	}
}

func TestHandleRecordEvent_NearMissUserTelemetryTypesAreRejected(t *testing.T) {
	for _, typ := range []string{
		"User_Action", "USER_ACTION", "user-action", "userAction", "useraction",
		" user_action", "user_action ", "user_action\n", "user_actions",
		"failure-shown", "Failure_Shown", "failureShown", "failure_show", "failure_shown_",
		"user_action\x00", "failure_shown,user_action",
	} {
		t.Run(typ, func(t *testing.T) {
			store := &recordingEventStore{}
			router := buildEventRouter(store)

			rec := discServe(t, router, http.MethodPost, "/discovery/events",
				discJsonBody(t, map[string]any{"type": typ, "payload": map[string]any{"session_id": "s-1"}}))

			if rec.Code != http.StatusBadRequest {
				t.Errorf("type %q: status = %d, want 400", typ, rec.Code)
			}
			if len(store.events) != 0 {
				t.Errorf("type %q: stored %d events, want 0", typ, len(store.events))
			}
		})
	}
}

func TestHandleSearch_CacheHitReflectsTheResultCache(t *testing.T) {
	provider := &fakeSearchProvider{name: discovery.ProviderDeezer, results: searchSlate("c-", 3)}
	searchSvc := discovery.NewSearchService(
		[]discovery.SearchProvider{provider},
		discovery.NewCircuitBreaker(),
		discovery.WithResultCache(&mapResultCache{entries: map[string][]discovery.SearchResult{}}),
	)
	h := discovery.NewDiscoveryHandler(discovery.DiscoveryServices{Search: searchSvc})
	router := chi.NewRouter()
	router.Use(discAsTestUser)
	router.Mount("/discovery", h.Routes())

	first := searchPageOverHTTP(t, router, "/discovery/search?q=humble")
	if first.Cache.Hit {
		t.Error("first query reported cache.hit = true")
	}
	second := searchPageOverHTTP(t, router, "/discovery/search?q=humble")
	if !second.Cache.Hit {
		t.Error("repeated query reported cache.hit = false")
	}
}

type recordingLogger struct {
	embedded.Logger
	records []log.Record
}

func (l *recordingLogger) Emit(_ context.Context, r log.Record) { l.records = append(l.records, r) }
func (l *recordingLogger) Enabled(context.Context, log.EnabledParameters) bool {
	return true
}

func buildClientEventRouter(logger *recordingLogger) chi.Router {
	h := discovery.NewDiscoveryHandler(discovery.DiscoveryServices{Event: discovery.NewRecordEventService(&recordingEventStore{})}).
		WithClientEvents(telemetry.NewClientEvents("k", logger))
	r := chi.NewRouter()
	r.Use(discAsTestUser)
	r.Mount("/discovery", h.Routes())
	return r
}

func serveClientEvent(t *testing.T, router chi.Router, analytics string) *httptest.ResponseRecorder {
	t.Helper()
	body := discJsonBody(t, map[string]any{"type": "play", "payload": map[string]any{"surface": "search"}})
	req := httptest.NewRequest(http.MethodPost, "/discovery/events", body)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer fake-token")
	if analytics != "" {
		req.Header.Set("X-Altune-Analytics", analytics)
	}
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

func TestHandleRecordEvent_ClientEventEmittedOnce(t *testing.T) {
	logger := &recordingLogger{}
	rec := serveClientEvent(t, buildClientEventRouter(logger), "")
	discAssertStatus(t, rec, http.StatusNoContent)
	if len(logger.records) != 1 {
		t.Fatalf("records = %d, want 1", len(logger.records))
	}
}

func TestHandleRecordEvent_AnOldBuildThatSendsAnalyticsOffIsStillRecorded(t *testing.T) {
	logger := &recordingLogger{}
	rec := serveClientEvent(t, buildClientEventRouter(logger), "off")
	discAssertStatus(t, rec, http.StatusNoContent)
	if len(logger.records) != 1 {
		t.Fatalf("records = %d, want 1: the server no longer honours an analytics opt-out", len(logger.records))
	}
}

func TestHandleRecordEvent_ClientEventSkippedWhenStoreFails(t *testing.T) {
	logger := &recordingLogger{}
	h := discovery.NewDiscoveryHandler(discovery.DiscoveryServices{Event: discovery.NewRecordEventService(&recordingEventStore{err: context.DeadlineExceeded})}).
		WithClientEvents(telemetry.NewClientEvents("k", logger))
	r := chi.NewRouter()
	r.Use(discAsTestUser)
	r.Mount("/discovery", h.Routes())
	rec := serveClientEvent(t, r, "")
	discAssertStatus(t, rec, http.StatusInternalServerError)
	if len(logger.records) != 0 {
		t.Fatalf("records = %d, want 0", len(logger.records))
	}
}

type recordedArtist struct {
	key   discovery.UnreleasedArtistKey
	name  string
	known map[discovery.ProviderName]string
}

type recordingUnreleasedStore struct {
	*discovery.FakeUnreleasedStore
	mu      sync.Mutex
	artists []recordedArtist
}

func (s *recordingUnreleasedStore) Record(ctx context.Context, key discovery.UnreleasedArtistKey, name string, known map[discovery.ProviderName]string) error {
	s.mu.Lock()
	s.artists = append(s.artists, recordedArtist{key: key, name: name, known: known})
	s.mu.Unlock()
	return s.FakeUnreleasedStore.Record(ctx, key, name, known)
}

func searchArtist(name, id string) discovery.SearchResult {
	return discovery.SearchResult{
		Kind: discovery.ResultKindArtist, Title: name, Confidence: discovery.ConfidenceLow,
		Sources: []discovery.SourceRef{{Provider: discovery.ProviderDeezer, ExternalID: id, URL: "https://deezer.com/artist/" + id}},
	}
}

func searchAsWarmUp(t *testing.T, store discovery.UnreleasedStore, results []discovery.SearchResult) *httptest.ResponseRecorder {
	t.Helper()
	var jobs sync.WaitGroup
	svc := discovery.NewUnreleasedService(nil, &discovery.ArtistCatalogTitles{}, nil, discovery.UnreleasedConfig{},
		discovery.WithUnreleasedStore(store), discovery.WithUnreleasedLifecycle(context.Background(), &jobs))
	p := &fakeSearchProvider{name: discovery.ProviderDeezer, results: results}
	searchSvc := discovery.NewSearchService([]discovery.SearchProvider{p}, discovery.NewCircuitBreaker())
	h := discovery.NewDiscoveryHandler(discovery.DiscoveryServices{Search: searchSvc}).WithUnreleased(svc)
	r := chi.NewRouter()
	r.Use(discAsTestUser)
	r.Mount("/discovery", h.Routes())
	rec := discServe(t, r, http.MethodGet, "/discovery/search?q=a", nil)
	jobs.Wait()
	return rec
}

func TestSearchWarmsUnreleased_TopThreeArtistsAreRecorded(t *testing.T) {
	store := &recordingUnreleasedStore{FakeUnreleasedStore: &discovery.FakeUnreleasedStore{}}
	results := []discovery.SearchResult{
		searchArtist("One", "1"), searchArtist("Two", "2"), searchArtist("Three", "3"), searchArtist("Four", "4"),
	}

	rec := searchAsWarmUp(t, store, results)

	discAssertStatus(t, rec, http.StatusOK)
	if len(store.artists) != 3 {
		t.Fatalf("recorded %d artists, want 3: %+v", len(store.artists), store.artists)
	}
	got := store.artists[0]
	want := discovery.UnreleasedArtistKey{Provider: discovery.ProviderDeezer, ExternalID: "1"}
	if got.key != want || got.name != "One" || got.known[discovery.ProviderDeezer] != "1" {
		t.Errorf("first record = %+v, want deezer:1 One with its known id", got)
	}
}

func TestSearchWarmsUnreleased_FailingStoreLeavesTheResponseUnchanged(t *testing.T) {
	results := []discovery.SearchResult{searchArtist("One", "1")}
	healthy := searchAsWarmUp(t, &discovery.FakeUnreleasedStore{}, results)
	failing := searchAsWarmUp(t, &discovery.FakeUnreleasedStore{RecordErr: errors.New("db down")}, results)

	discAssertStatus(t, failing, http.StatusOK)
	var a, b discovery.DiscoverySearchResponse
	if err := json.Unmarshal(healthy.Body.Bytes(), &a); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(failing.Body.Bytes(), &b); err != nil {
		t.Fatal(err)
	}
	if len(b.Results) != 1 || len(a.Results) != len(b.Results) || a.Results[0].Title != b.Results[0].Title {
		t.Errorf("failing-store results = %+v, want the healthy results %+v", b.Results, a.Results)
	}
}

type stubContentProvider struct {
	provider discovery.ProviderName
	down     bool
}

func (p stubContentProvider) answer(kind discovery.ResultKind, id string) ([]discovery.SearchResult, error) {
	if p.down {
		return nil, errors.New("upstream unavailable")
	}
	return []discovery.SearchResult{{
		Kind:     kind,
		Title:    "Real Song",
		Subtitle: "Artist",
		Sources:  []discovery.SourceRef{{Provider: p.provider, ExternalID: id}},
		Extras:   map[string]any{},
	}}, nil
}

func (p stubContentProvider) GetAlbumTracks(_ context.Context, _ discovery.ProviderName, id string) ([]discovery.SearchResult, error) {
	return p.answer(discovery.ResultKindTrack, id)
}

func (p stubContentProvider) GetArtistTopTracks(_ context.Context, _ discovery.ProviderName, id string) ([]discovery.SearchResult, error) {
	return p.answer(discovery.ResultKindTrack, id)
}

func (p stubContentProvider) GetArtistAlbums(_ context.Context, _ discovery.ProviderName, id string) ([]discovery.SearchResult, error) {
	return p.answer(discovery.ResultKindAlbum, id)
}

func (p stubContentProvider) GetRelatedTracks(_ context.Context, _ discovery.ProviderName, id string) ([]discovery.SearchResult, error) {
	return p.answer(discovery.ResultKindTrack, id)
}
