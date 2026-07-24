package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"altune/go-api/internal/auth"
	discdomain "altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/discovery/ports"
	"altune/go-api/internal/discovery/service"
	"altune/go-api/internal/shared/httputil"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

// --- fake vocabulary store (suggest) ---

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

// --- recording event store ---

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

// --- fake operator-console recorders ---

type fakeProviderHealth struct {
	records []string // "provider/status"
}

func (f *fakeProviderHealth) Record(provider, status string, _ int64) {
	f.records = append(f.records, provider+"/"+status)
}

type fakeSearchTrace struct {
	searchQueries  []string
	contentFetches []string // fetch kind ("top_tracks", "albums")
}

func (f *fakeSearchTrace) RecordSearch(_ context.Context, query string, _ []string, _ string, _ []discdomain.ProviderSearchResponse, _ []discdomain.SearchResult) {
	f.searchQueries = append(f.searchQueries, query)
}

func (f *fakeSearchTrace) RecordContentFetch(_ context.Context, kind, _, _, _ string, _ []discdomain.SearchResult) {
	f.contentFetches = append(f.contentFetches, kind)
}

// --- router helpers ---

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
	// Mirror the app's body-size cap (app.go wires MaxBodySize(1<<20) globally)
	// so the oversized-payload path is exercised as deployed.
	r.Use(httputil.MaxBodySize(1 << 20))
	r.Mount("/discovery", h.Routes())
	return r
}

// ==================== Suggest ====================

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
		// limit is valid on (0, 10]; everything else falls back to 5.
		cases := []struct {
			query string
			want  int
		}{
			{"/discovery/suggest?q=x", 5},           // absent
			{"/discovery/suggest?q=x&limit=0", 5},   // zero
			{"/discovery/suggest?q=x&limit=-3", 5},  // negative
			{"/discovery/suggest?q=x&limit=11", 5},  // above cap
			{"/discovery/suggest?q=x&limit=abc", 5}, // non-numeric
			{"/discovery/suggest?q=x&limit=1", 1},   // lower valid bound
			{"/discovery/suggest?q=x&limit=10", 10}, // upper valid bound
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

// ==================== Search param validation ====================

func TestHandleSearch_LimitBoundaries(t *testing.T) {
	// The domain caps limit at 50 (NewSearchQuery); non-positive / unparsable
	// values fall back to the handler default of 20 rather than erroring.
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
		{"non-numeric defaults", "/discovery/search?q=x&limit=abc", http.StatusOK},
		// strconv.Atoi saturates an overflowing value to MaxInt (alongside
		// ErrRange), so a huge limit lands above the domain cap and 400s.
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
		{"mixed valid and invalid rejects", "/discovery/search?q=x&kinds=track,bogus", http.StatusUnprocessableEntity},
		{"only empty entries default", "/discovery/search?q=x&kinds=,,", http.StatusOK},
		{"whitespace-padded entries trim", "/discovery/search?q=x&kinds=%20track%20,album", http.StatusOK},
		{"all invalid rejects", "/discovery/search?q=x&kinds=foo,bar", http.StatusUnprocessableEntity},
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

func TestHandleSearch_RecordsProviderHealthAndTrace(t *testing.T) {
	provider := &fakeSearchProvider{name: discdomain.ProviderDeezer}
	ph := &fakeProviderHealth{}
	tr := &fakeSearchTrace{}
	h := NewDiscoveryHandler(DiscoveryServices{
		Search: service.NewService([]ports.SearchProvider{provider}, service.NewCircuitBreaker()),
	}).WithProviderHealth(ph).WithRequestTrace(tr)

	router := chi.NewRouter()
	router.Use(auth.Middleware(discVerifyAsTestUser))
	router.Mount("/discovery", h.Routes())

	rec := discServe(t, router, http.MethodGet, "/discovery/search?q=trace+me", nil)
	discAssertStatus(t, rec, http.StatusOK)

	if len(ph.records) == 0 {
		t.Error("expected provider-health records after a search")
	}
	if len(tr.searchQueries) != 1 || tr.searchQueries[0] != "trace me" {
		t.Errorf("trace queries = %v, want [trace me]", tr.searchQueries)
	}
}

// ==================== Search history ====================

func TestHandleSearchHistory_UTCTimestampFormat(t *testing.T) {
	// A non-UTC ExecutedAt must be converted before the Z-suffixed layout is
	// applied — the wire timestamp always tells UTC truth.
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

	var resp DiscoverySearchHistoryResponse
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

// ==================== Events ====================

func TestHandleRecordEvent_StoreErrorReturns500(t *testing.T) {
	router := buildEventRouter(&recordingEventStore{err: context.DeadlineExceeded})

	body := map[string]any{"type": "play"}
	rec := discServe(t, router, http.MethodPost, "/discovery/events", discJsonBody(t, body))
	discAssertStatus(t, rec, http.StatusInternalServerError)
}

func TestHandleRecordEvent_OversizedBodyReturns400(t *testing.T) {
	router := buildEventRouter(&recordingEventStore{})

	// Just over the 1MB cap: MaxBytesReader trips mid-decode -> "invalid
	// request body" 400, never a 500 or an appended event.
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

	for _, reserved := range []string{"search_performed", "results_shown"} {
		body := map[string]any{"type": reserved}
		rec := discServe(t, router, http.MethodPost, "/discovery/events", discJsonBody(t, body))
		if rec.Code != http.StatusBadRequest {
			t.Errorf("type %q: status = %d, want 400 (server-reserved)", reserved, rec.Code)
		}
	}
}

// ==================== Wire mapping units ====================

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
		"duration":     180, // pre-existing extras survive the merge
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

func TestKindNames_SortedStable(t *testing.T) {
	got := kindNames(map[discdomain.ResultKind]bool{
		discdomain.ResultKindTrack:  true,
		discdomain.ResultKindAlbum:  true,
		discdomain.ResultKindArtist: true,
	})
	want := []string{"album", "artist", "track"}
	if len(got) != len(want) {
		t.Fatalf("len = %d, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("kindNames[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestSearchStatusCode(t *testing.T) {
	okStatus := discdomain.ProviderSearchResponse{Provider: discdomain.ProviderDeezer, Status: discdomain.ProviderStatusOK}
	errStatus := discdomain.ProviderSearchResponse{Provider: discdomain.ProviderITunes, Status: discdomain.ProviderStatusError}

	cases := []struct {
		name     string
		statuses []discdomain.ProviderSearchResponse
		want     int
	}{
		{"empty scatter is 200", nil, http.StatusOK},
		{"all ok is 200", []discdomain.ProviderSearchResponse{okStatus}, http.StatusOK},
		{"mixed is 200", []discdomain.ProviderSearchResponse{errStatus, okStatus}, http.StatusOK},
		{"all failed is 503", []discdomain.ProviderSearchResponse{errStatus, errStatus}, http.StatusServiceUnavailable},
	}
	for _, c := range cases {
		if got := searchStatusCode(c.statuses); got != c.want {
			t.Errorf("%s: searchStatusCode = %d, want %d", c.name, got, c.want)
		}
	}
}
