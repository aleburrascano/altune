package providers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"altune/go-api/internal/discovery/domain"
)

// AIDEV-NOTE: KNOWN silent-zero — doSearch walks the whole Template Interface
// tree collecting card-shaped elements; a valid 200 JSON response containing
// none (a layout drift, an interstitial, an empty shelf) yields zero results
// with no error anywhere. This test PINS that behaviour; if the adapter ever
// gains a tree-shape check, update this to expect an error.
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
		name              string
		deeplink          string
		album, track      string
		ok                bool
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
