package providers

import (
	"altune/go-api/internal/discovery/domain"
	"context"
	"fmt"
	"net/url"
	"strings"
	"testing"
)

func TestSearchAcrossKinds_DoesNotLogQueryText(t *testing.T) {
	buf := captureDefaultLog(t)
	all := allSearchKinds()

	// The provider error embeds the request URL, as *url.Error does.
	_, _ = searchAcrossKinds(context.Background(), "deezer", sensitiveQuery, all, all,
		func(_ context.Context, _ domain.ResultKind) ([]domain.SearchResult, error) {
			return nil, fmt.Errorf("get \"https://api.example/search?q=%s\": timeout", url.QueryEscape(sensitiveQuery))
		})

	logged := buf.String()
	if !strings.Contains(logged, "deezer.search_kind_failed") || !strings.Contains(logged, "timeout") {
		t.Fatalf("expected the kind failure (with its error cause) to still be logged, got:\n%s", logged)
	}
	assertNoQueryText(t, logged)
}
