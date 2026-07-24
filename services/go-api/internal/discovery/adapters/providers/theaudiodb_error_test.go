package providers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"altune/go-api/internal/discovery/domain"
)

// A dead TheAudioDB must be distinguishable from no-matches: transport/HTTP
// failures surface as errors so breaker/health signals can fire.
func TestTheAudioDBAdapter_Search_transportErrorSurfaces(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	adapter := NewTheAudioDBAdapter(newTestClient(server.URL))
	results, err := adapter.Search(context.Background(), "anything", map[domain.ResultKind]bool{
		domain.ResultKindArtist: true,
	})
	if err == nil {
		t.Fatal("expected an error on HTTP 500, got nil (dead provider indistinguishable from no-matches)")
	}
	if len(results) != 0 {
		t.Errorf("expected 0 results, got %d", len(results))
	}
}
