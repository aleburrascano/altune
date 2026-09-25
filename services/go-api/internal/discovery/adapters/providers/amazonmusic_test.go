package providers

import (
	"altune/go-api/internal/discovery/domain"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func newTestAmazonMusicAdapter(srv *httptest.Server) *AmazonMusicAdapter {
	a := NewAmazonMusicAdapter(srv.Client())
	a.searchURL = srv.URL
	a.resolver.cached = &amazonMusicSession{
		DeviceID:  "test-device",
		SessionID: "test-session",
		Version:   "1.0.0",
	}
	a.resolver.cached.CSRF.Token = "test-csrf-token"
	return a
}

func allKinds() map[domain.ResultKind]bool {
	return map[domain.ResultKind]bool{
		domain.ResultKindTrack:  true,
		domain.ResultKindAlbum:  true,
		domain.ResultKindArtist: true,
	}
}

const amzFixtureResponse = `{
  "methods": [{
    "template": {
      "widgets": [{
        "items": [
          {
            "interface": "Web.TemplatesInterface.v1_0.Touch.WidgetsInterface.SquareHorizontalItemElement",
            "primaryText": {"text": "Blinding Lights"},
            "secondaryText": "The Weeknd",
            "image": "https://m.media-amazon.com/images/I/track.jpg",
            "primaryLink": {"deeplink": "/albums/B086Q2QNLH?trackAsin=B086Q41M9C"},
            "secondaryLink": {"deeplink": "/artists/B00G9Y64K6/the-weeknd"}
          },
          {
            "interface": "Web.TemplatesInterface.v1_0.Touch.WidgetsInterface.SquareHorizontalItemElement",
            "primaryText": {"text": "Blinding Lights"},
            "secondaryText": "The Weeknd",
            "image": "https://m.media-amazon.com/images/I/track.jpg",
            "primaryLink": {"deeplink": "/albums/B086Q2QNLH?trackAsin=B086Q41M9C"},
            "secondaryLink": {"deeplink": "/artists/B00G9Y64K6/the-weeknd"}
          },
          {
            "interface": "Web.TemplatesInterface.v1_0.Touch.WidgetsInterface.SquareVerticalItemElement",
            "primaryText": {"text": "After Hours (Deluxe)"},
            "secondaryText": "The Weeknd",
            "image": "https://m.media-amazon.com/images/I/album.jpg",
            "primaryLink": {"deeplink": "/albums/B086Q2QNLH"},
            "secondaryLink": {"deeplink": "/artists/B00G9Y64K6/the-weeknd"}
          },
          {
            "interface": "Web.TemplatesInterface.v1_0.Touch.WidgetsInterface.CircleVerticalItemElement",
            "primaryText": {"text": "The Weeknd"},
            "image": "https://m.media-amazon.com/images/I/artist.jpg",
            "primaryLink": {"deeplink": "/artists/B00G9Y64K6/the-weeknd"}
          },
          {
            "interface": "Web.TemplatesInterface.v1_0.Touch.WidgetsInterface.SquareVerticalItemElement",
            "primaryText": {"text": "Some Podcast Episode"},
            "image": "https://m.media-amazon.com/images/I/podcast.jpg",
            "primaryLink": {"deeplink": "/podcasts/B0PODCAST1"}
          }
        ]
      }]
    }
  }]
}`

func TestAmazonMusicAdapter_Search_classifiesAndDedupes(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(amzFixtureResponse))
	}))
	defer srv.Close()

	a := newTestAmazonMusicAdapter(srv)
	results, err := a.Search(t.Context(), "Blinding Lights", allKinds())
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}

	byKind := map[domain.ResultKind][]domain.SearchResult{}
	for _, r := range results {
		byKind[r.Kind] = append(byKind[r.Kind], r)
	}

	if got := len(byKind[domain.ResultKindTrack]); got != 1 {
		t.Errorf("track count = %d, want 1 (duplicate should dedupe)", got)
	}
	if got := len(byKind[domain.ResultKindAlbum]); got != 1 {
		t.Errorf("album count = %d, want 1", got)
	}
	if got := len(byKind[domain.ResultKindArtist]); got != 1 {
		t.Errorf("artist count = %d, want 1", got)
	}
	for _, r := range results {
		if r.Title == "Some Podcast Episode" {
			t.Errorf("podcast card leaked into results: %+v", r)
		}
	}

	track := byKind[domain.ResultKindTrack][0]
	if track.Title != "Blinding Lights" || track.Subtitle != "The Weeknd" {
		t.Errorf("track = %+v, want title/subtitle Blinding Lights/The Weeknd", track)
	}
	if len(track.Sources) != 1 || track.Sources[0].ExternalID != "B086Q41M9C" {
		t.Errorf("track source = %+v, want ExternalID B086Q41M9C (the trackAsin, not the album)", track.Sources)
	}
	if track.Extras["album_asin"] != "B086Q2QNLH" {
		t.Errorf("track album_asin extra = %v, want B086Q2QNLH", track.Extras["album_asin"])
	}
	if track.Extras["artist_asin"] != "B00G9Y64K6" {
		t.Errorf("track artist_asin extra = %v, want B00G9Y64K6", track.Extras["artist_asin"])
	}

	album := byKind[domain.ResultKindAlbum][0]
	if len(album.Sources) != 1 || album.Sources[0].ExternalID != "B086Q2QNLH" {
		t.Errorf("album source = %+v, want ExternalID B086Q2QNLH", album.Sources)
	}

	artist := byKind[domain.ResultKindArtist][0]
	if len(artist.Sources) != 1 || artist.Sources[0].ExternalID != "B00G9Y64K6" {
		t.Errorf("artist source = %+v, want ExternalID B00G9Y64K6", artist.Sources)
	}
}

func TestAmazonMusicAdapter_Search_filtersByRequestedKinds(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(amzFixtureResponse))
	}))
	defer srv.Close()

	a := newTestAmazonMusicAdapter(srv)
	results, err := a.Search(t.Context(), "Blinding Lights", map[domain.ResultKind]bool{domain.ResultKindArtist: true})
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	for _, r := range results {
		if r.Kind != domain.ResultKindArtist {
			t.Errorf("got kind %v, want only artist results", r.Kind)
		}
	}
	if len(results) != 1 {
		t.Errorf("results = %d, want 1 artist", len(results))
	}
}

func TestAmazonMusicAdapter_Search_reResolvesSessionOnAuthFailure(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls == 1 {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(amzFixtureResponse))
	}))
	defer srv.Close()

	configSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"deviceId":  "fresh-device",
			"sessionId": "fresh-session",
			"version":   "1.0.0",
			"csrf":      map[string]string{"token": "fresh-token", "rnd": "1", "ts": "1"},
		})
	}))
	defer configSrv.Close()

	a := newTestAmazonMusicAdapter(srv)
	a.resolver.configURL = configSrv.URL

	results, err := a.Search(t.Context(), "Blinding Lights", allKinds())
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	if calls != 2 {
		t.Errorf("calls = %d, want 2 (initial 403 then retry after re-resolve)", calls)
	}
	if len(results) == 0 {
		t.Errorf("expected results after re-resolve, got none")
	}
}

func TestAmazonMusicSessionResolver_resolve(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"deviceId":  "d1",
			"sessionId": "s1",
			"version":   "1.2.3",
			"csrf":      map[string]string{"token": "t1", "rnd": "r1", "ts": "ts1"},
		})
	}))
	defer srv.Close()

	r := newAmazonMusicSessionResolver(srv.Client())
	r.configURL = srv.URL

	sess, err := r.get(t.Context())
	if err != nil {
		t.Fatalf("get() error = %v", err)
	}
	if sess.DeviceID != "d1" || sess.SessionID != "s1" || sess.CSRF.Token != "t1" {
		t.Errorf("session = %+v, want d1/s1/t1", sess)
	}
}

func TestAmazonMusicAdapter_Name(t *testing.T) {
	a := NewAmazonMusicAdapter(http.DefaultClient)
	if got := a.Name(); got != domain.ProviderAmazonMusic {
		t.Errorf("Name() = %v, want %v", got, domain.ProviderAmazonMusic)
	}
}

const amzDeepCard = `{"interface":"Web.TemplatesInterface.v1_0.Touch.WidgetsInterface.CircleVerticalItemElement",` +
	`"primaryText":{"text":"Deep Artist"},"primaryLink":{"deeplink":"/artists/B0DEEP0001"}}`

// nestedAmazonMusicJSON wraps a card in levels alternating object/array
// nesting, so the card sits at depth `levels` and its own nested objects
// (primaryText, primaryLink) at depth levels+1.
func nestedAmazonMusicJSON(levels int) string {
	var b strings.Builder
	for i := range levels {
		if i%2 == 0 {
			b.WriteString(`{"n":`)
		} else {
			b.WriteString(`[`)
		}
	}
	b.WriteString(amzDeepCard)
	for i := levels - 1; i >= 0; i-- {
		if i%2 == 0 {
			b.WriteString(`}`)
		} else {
			b.WriteString(`]`)
		}
	}
	return b.String()
}

func serveAmazonMusicBody(t *testing.T, body string) *AmazonMusicAdapter {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return newTestAmazonMusicAdapter(srv)
}

func TestAmazonMusicAdapter_Search_rejectsResponseNestedBeyondMaxDepth(t *testing.T) {
	a := serveAmazonMusicBody(t, nestedAmazonMusicJSON(5000))

	results, err := a.Search(t.Context(), "deep", allKinds())
	if !errors.Is(err, ErrAmazonMusicResponseTooDeep) {
		t.Fatalf("Search() error = %v, results = %d; want ErrAmazonMusicResponseTooDeep for a 5000-level response", err, len(results))
	}
	if len(results) != 0 {
		t.Errorf("results = %d, want 0 on a rejected response", len(results))
	}
}

func decodeNestedAmazonMusic(t *testing.T, levels int) any {
	t.Helper()
	var root any
	if err := json.Unmarshal([]byte(nestedAmazonMusicJSON(levels)), &root); err != nil {
		t.Fatalf("decode fixture: %v", err)
	}
	return root
}

func TestWalkAmazonMusicNode_depthBoundary(t *testing.T) {
	tests := []struct {
		name      string
		levels    int
		wantErr   bool
		wantCards int
	}{
		{name: "deepest object at max depth is walked", levels: amzMaxWalkDepth - 1, wantCards: 1},
		{name: "object one level past max depth fails", levels: amzMaxWalkDepth, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var out []domain.SearchResult
			err := walkAmazonMusicNode(decodeNestedAmazonMusic(t, tt.levels), 0, map[string]bool{}, &out)
			if got := errors.Is(err, ErrAmazonMusicResponseTooDeep); got != tt.wantErr {
				t.Fatalf("walk error = %v, want too-deep failure %v", err, tt.wantErr)
			}
			if !tt.wantErr && len(out) != tt.wantCards {
				t.Errorf("cards = %d, want %d", len(out), tt.wantCards)
			}
		})
	}
}

func TestAmazonMusicAdapter_Search_noCardsIsSilentZero(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"methods": [{"template": {"widgets": [{"items": []}]}}]}`))
	}))
	defer srv.Close()

	a := newTestAmazonMusicAdapter(srv)
	results, err := a.Search(context.Background(), "anything", allKinds())
	if err != nil {
		t.Fatalf("pinned behaviour: a card-less 200 tree is a silent empty success, got error %v", err)
	}
	if len(results) != 0 {
		t.Errorf("results = %d, want 0", len(results))
	}
}

func TestAmazonMusicAdapter_Search_http500IsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	a := newTestAmazonMusicAdapter(srv)
	if _, err := a.Search(context.Background(), "q", allKinds()); err == nil {
		t.Fatal("expected an error on a non-auth HTTP 500 (no session re-resolve applies)")
	}
}

func TestAmazonMusicAdapter_Search_malformedJSONIsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`<html>maintenance</html>`))
	}))
	defer srv.Close()

	a := newTestAmazonMusicAdapter(srv)
	_, err := a.Search(context.Background(), "q", allKinds())
	if err == nil || !strings.Contains(err.Error(), "decode showSearch") {
		t.Fatalf("err = %v, want a decode error on an HTML-instead-of-JSON body", err)
	}
}

func TestAmazonMusicAdapter_doSearch_non200ErrorTextAndStatusPinned(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(`{"methods":[]}`))
	}))
	defer srv.Close()

	a := newTestAmazonMusicAdapter(srv)
	results, status, err := a.doSearch(context.Background(), a.resolver.cached, "q")
	if err == nil || err.Error() != "http status 503" {
		t.Fatalf("err = %v, want exactly %q", err, "http status 503")
	}
	if status != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503 surfaced for auth-retry branching", status)
	}
	if results != nil {
		t.Errorf("results = %v, want nil on a non-200", results)
	}
}

func TestBuildAmazonMusicSearchBody(t *testing.T) {
	sess := &amazonMusicSession{
		DeviceID:  "dev-1",
		SessionID: "sess-1",
		Version:   "9.9.9",
	}
	sess.CSRF.Token = "csrf-tok"
	sess.CSRF.Rnd = "rnd-1"
	sess.CSRF.Ts = "1700000000"

	body, err := buildAmazonMusicSearchBody(sess, "blinding lights")
	if err != nil {
		t.Fatalf("buildAmazonMusicSearchBody: %v", err)
	}

	var req amzSearchRequest
	if err := json.Unmarshal([]byte(body), &req); err != nil {
		t.Fatalf("body is not valid JSON: %v", err)
	}
	if req.SuggestedKeyword != "blinding lights" {
		t.Errorf("SuggestedKeyword = %q", req.SuggestedKeyword)
	}
	var headers amzHeadersBundle
	if err := json.Unmarshal([]byte(req.Headers), &headers); err != nil {
		t.Fatalf("headers bundle is not nested JSON: %v", err)
	}
	if headers.SessionID != "sess-1" || headers.DeviceID != "dev-1" || headers.AppVersion != "9.9.9" {
		t.Errorf("headers = %+v, want session fields carried", headers)
	}
	var csrf amzCSRFElement
	if err := json.Unmarshal([]byte(headers.CSRF), &csrf); err != nil {
		t.Fatalf("csrf element is not nested JSON: %v", err)
	}
	if csrf.Token != "csrf-tok" || csrf.RndNonce != "rnd-1" || csrf.Timestamp != "1700000000" {
		t.Errorf("csrf = %+v, want the session's CSRF triple", csrf)
	}
}

func TestAmazonMusicDeeplinkID(t *testing.T) {
	tests := []struct {
		name     string
		deeplink string
		prefix   string
		want     string
		ok       bool
	}{
		{"artist with slug", "/artists/B00G9Y64K6/the-weeknd", "/artists/", "B00G9Y64K6", true},
		{"artist bare", "/artists/B00G9Y64K6", "/artists/", "B00G9Y64K6", true},
		{"stops at query", "/artists/B00G9Y64K6?ref=x", "/artists/", "B00G9Y64K6", true},
		{"wrong prefix", "/albums/B086Q2QNLH", "/artists/", "", false},
		{"empty rest", "/artists/", "/artists/", "", false},
		{"query only", "/artists/?ref=x", "/artists/", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := amazonMusicDeeplinkID(tt.deeplink, tt.prefix)
			if got != tt.want || ok != tt.ok {
				t.Errorf("amazonMusicDeeplinkID(%q) = (%q, %v), want (%q, %v)", tt.deeplink, got, ok, tt.want, tt.ok)
			}
		})
	}
}

func TestAmazonMusicAlbumDeeplink(t *testing.T) {
	tests := []struct {
		name         string
		deeplink     string
		album, track string
		ok           bool
	}{
		{"album only", "/albums/B086Q2QNLH", "B086Q2QNLH", "", true},
		{"album with slug", "/albums/B086Q2QNLH/after-hours", "B086Q2QNLH", "", true},
		{"track within album", "/albums/B086Q2QNLH?trackAsin=B086Q41M9C", "B086Q2QNLH", "B086Q41M9C", true},
		{"malformed query keeps album", "/albums/B086Q2QNLH?track;Asin=%zz", "B086Q2QNLH", "", true},
		{"not an album link", "/artists/B00G9Y64K6", "", "", false},
		{"bare prefix", "/albums/", "", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			album, track, ok := amazonMusicAlbumDeeplink(tt.deeplink)
			if album != tt.album || track != tt.track || ok != tt.ok {
				t.Errorf("amazonMusicAlbumDeeplink(%q) = (%q, %q, %v), want (%q, %q, %v)",
					tt.deeplink, album, track, ok, tt.album, tt.track, tt.ok)
			}
		})
	}
}

func TestAmazonMusicText(t *testing.T) {
	if got := amazonMusicText("bare"); got != "bare" {
		t.Errorf("bare string = %q", got)
	}
	if got := amazonMusicText(map[string]any{"text": "wrapped"}); got != "wrapped" {
		t.Errorf("element = %q", got)
	}
	if got := amazonMusicText(42); got != "" {
		t.Errorf("non-text = %q, want empty", got)
	}
	if got := amazonMusicText(map[string]any{"other": "x"}); got != "" {
		t.Errorf("element without text = %q, want empty", got)
	}
}

func TestMapAmazonMusicItem_rejections(t *testing.T) {
	if _, ok := mapAmazonMusicItem(map[string]any{"interface": "NavigationElement"}); ok {
		t.Error("non-card interface must be rejected")
	}
	if _, ok := mapAmazonMusicItem(map[string]any{
		"interface":   "X.SquareHorizontalItemElement",
		"primaryText": map[string]any{"text": "   "},
	}); ok {
		t.Error("blank title must be rejected")
	}
	if _, ok := mapAmazonMusicItem(map[string]any{
		"interface":   "X.SquareHorizontalItemElement",
		"primaryText": map[string]any{"text": "Title"},
	}); ok {
		t.Error("missing primaryLink must be rejected")
	}
	if _, ok := mapAmazonMusicItem(map[string]any{
		"interface":   "X.SquareHorizontalItemElement",
		"primaryText": map[string]any{"text": "Title"},
		"primaryLink": map[string]any{"deeplink": "/podcasts/B0X"},
	}); ok {
		t.Error("unclassifiable deeplink must be rejected")
	}
}

func TestAmazonMusicAdapter_meta(t *testing.T) {
	a := NewAmazonMusicAdapter(http.DefaultClient)
	kinds := a.SupportedKinds()
	if !kinds[domain.ResultKindTrack] || !kinds[domain.ResultKindAlbum] || !kinds[domain.ResultKindArtist] {
		t.Errorf("SupportedKinds = %v, want all three", kinds)
	}
	if a.SearchTimeout() != amzSearchTimeout {
		t.Errorf("SearchTimeout = %v, want %v", a.SearchTimeout(), amzSearchTimeout)
	}
	if a.ArtworkSource() != "amazonmusic" {
		t.Errorf("ArtworkSource = %q", a.ArtworkSource())
	}
}
