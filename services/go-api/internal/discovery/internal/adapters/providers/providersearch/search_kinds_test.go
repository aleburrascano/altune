package providersearch_test

import (
	"altune/go-api/internal/discovery"
	"altune/go-api/internal/discovery/internal/adapters/providers/providertest"
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"testing"
)

func TestSearchAcrossKinds_DoesNotLogQueryText(t *testing.T) {
	buf := providertest.CaptureDefaultLog(t)
	all := discovery.ProviderSearchAllKinds()

	_, _ = discovery.ProviderSearchAcrossKinds(context.Background(), "deezer", providertest.SensitiveQuery, all, all,
		func(_ context.Context, _ discovery.ResultKind) ([]discovery.SearchResult, error) {
			return nil, fmt.Errorf("get \"https://api.example/search?q=%s\": timeout", url.QueryEscape(providertest.SensitiveQuery))
		})

	logged := buf.String()
	if !strings.Contains(logged, "deezer.search_kind_failed") || !strings.Contains(logged, "timeout") {
		t.Fatalf("expected the kind failure (with its error cause) to still be logged, got:\n%s", logged)
	}
	providertest.AssertNoQueryText(t, logged)
}

func TestSearchAcrossKinds_OneFailedKindKeepsTheRestAndSaysWhichFailed(t *testing.T) {
	all := discovery.ProviderSearchAllKinds()

	got, err := discovery.ProviderSearchAcrossKinds(context.Background(), "deezer", "humble", all, all,
		func(_ context.Context, kind discovery.ResultKind) ([]discovery.SearchResult, error) {
			if kind == discovery.ResultKindAlbum {
				return nil, context.DeadlineExceeded
			}
			return []discovery.SearchResult{{Kind: kind, Title: "Humble"}}, nil
		})

	if len(got) != 2 {
		t.Fatalf("results = %d, want the 2 kinds that answered", len(got))
	}
	var partial *discovery.PartialKindsError
	if !errors.As(err, &partial) {
		t.Fatalf("err = %v, want a PartialKindsError", err)
	}
	if len(partial.Failed) != 1 || partial.Failed[0] != discovery.ResultKindAlbum {
		t.Errorf("failed kinds = %v, want [album]", partial.Failed)
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("err = %v, want the album timeout kept as its cause", err)
	}
}

func TestSearchAcrossKinds_AllKindsFailedIsAPlainError(t *testing.T) {
	all := discovery.ProviderSearchAllKinds()

	_, err := discovery.ProviderSearchAcrossKinds(context.Background(), "deezer", "humble", all, all,
		func(context.Context, discovery.ResultKind) ([]discovery.SearchResult, error) {
			return nil, errors.New("down")
		})

	var partial *discovery.PartialKindsError
	if err == nil || errors.As(err, &partial) {
		t.Fatalf("err = %v, want a plain failure when nothing answered", err)
	}
}
