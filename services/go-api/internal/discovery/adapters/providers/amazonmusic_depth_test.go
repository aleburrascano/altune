package providers

import (
	"altune/go-api/internal/discovery/domain"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

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
