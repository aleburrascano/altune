package providers

import (
	"altune/go-api/internal/discovery/domain"
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"testing"
)

func TestSearchAcrossKinds_DoesNotLogQueryText(t *testing.T) {
	buf := captureDefaultLog(t)
	all := allSearchKinds()

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

func TestSearchAcrossKinds_OneFailedKindKeepsTheRestAndSaysWhichFailed(t *testing.T) {
	all := allSearchKinds()

	got, err := searchAcrossKinds(context.Background(), "deezer", "humble", all, all,
		func(_ context.Context, kind domain.ResultKind) ([]domain.SearchResult, error) {
			if kind == domain.ResultKindAlbum {
				return nil, context.DeadlineExceeded
			}
			return []domain.SearchResult{{Kind: kind, Title: "Humble"}}, nil
		})

	if len(got) != 2 {
		t.Fatalf("results = %d, want the 2 kinds that answered", len(got))
	}
	var partial *domain.PartialKindsError
	if !errors.As(err, &partial) {
		t.Fatalf("err = %v, want a PartialKindsError", err)
	}
	if len(partial.Failed) != 1 || partial.Failed[0] != domain.ResultKindAlbum {
		t.Errorf("failed kinds = %v, want [album]", partial.Failed)
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("err = %v, want the album timeout kept as its cause", err)
	}
}

func TestSearchAcrossKinds_AllKindsFailedIsAPlainError(t *testing.T) {
	all := allSearchKinds()

	_, err := searchAcrossKinds(context.Background(), "deezer", "humble", all, all,
		func(context.Context, domain.ResultKind) ([]domain.SearchResult, error) {
			return nil, errors.New("down")
		})

	var partial *domain.PartialKindsError
	if err == nil || errors.As(err, &partial) {
		t.Fatalf("err = %v, want a plain failure when nothing answered", err)
	}
}
