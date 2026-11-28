package providers_test

import (
	"altune/go-api/internal/search"
	"altune/go-api/internal/search/internal/adapters/memory"
	"altune/go-api/internal/search/internal/adapters/providers"
	"altune/go-api/internal/search/internal/contracttest"
	"altune/go-api/internal/search/internal/domain"
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"testing"
)

func TestSearchAcrossKinds_DoesNotLogQueryText(t *testing.T) {
	buf := contracttest.CaptureDefaultLog(t)
	all := providers.AllSearchKinds()

	_, _ = providers.SearchAcrossKinds(context.Background(), "deezer", contracttest.SensitiveQuery, all, all,
		func(_ context.Context, _ search.ResultKind) ([]search.SearchResult, error) {
			return nil, fmt.Errorf("get \"https://api.example/search?q=%s\": timeout", url.QueryEscape(contracttest.SensitiveQuery))
		})

	logged := buf.String()
	if !strings.Contains(logged, "deezer.search_kind_failed") || !strings.Contains(logged, "timeout") {
		t.Fatalf("expected the kind failure (with its error cause) to still be logged, got:\n%s", logged)
	}
	contracttest.AssertNoQueryText(t, logged)
}

func TestSearchAcrossKinds_OneFailedKindKeepsTheRestAndSaysWhichFailed(t *testing.T) {
	all := providers.AllSearchKinds()

	got, err := providers.SearchAcrossKinds(context.Background(), "deezer", "humble", all, all,
		func(_ context.Context, kind search.ResultKind) ([]search.SearchResult, error) {
			if kind == search.ResultKindAlbum {
				return nil, context.DeadlineExceeded
			}
			return []search.SearchResult{memory.NewResult(kind, "Humble")}, nil
		})

	if len(got) != 2 {
		t.Fatalf("results = %d, want the 2 kinds that answered", len(got))
	}
	var partial *domain.PartialKindsError
	if !errors.As(err, &partial) {
		t.Fatalf("err = %v, want a PartialKindsError", err)
	}
	if len(partial.Failed) != 1 || partial.Failed[0] != search.ResultKindAlbum {
		t.Errorf("failed kinds = %v, want [album]", partial.Failed)
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("err = %v, want the album timeout kept as its cause", err)
	}
}

func TestSearchAcrossKinds_AllKindsFailedIsAPlainError(t *testing.T) {
	all := providers.AllSearchKinds()

	_, err := providers.SearchAcrossKinds(context.Background(), "deezer", "humble", all, all,
		func(context.Context, search.ResultKind) ([]search.SearchResult, error) {
			return nil, errors.New("down")
		})

	var partial *domain.PartialKindsError
	if err == nil || errors.As(err, &partial) {
		t.Fatalf("err = %v, want a plain failure when nothing answered", err)
	}
}
