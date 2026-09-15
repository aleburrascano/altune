package service

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"testing"

	"altune/go-api/internal/discovery/domain"
)

// A single-provider content fetch reports its failure in the typed status
// model search uses, not one generic error.
func TestFetchProviderResults_ClassifiesFailureStatus(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want domain.ProviderStatus
	}{
		{name: "client deadline", err: &url.Error{Op: "Get", URL: "https://p.test", Err: context.DeadlineExceeded}, want: domain.ProviderStatusTimeout},
		{name: "bare deadline", err: fmt.Errorf("fetch: %w", context.DeadlineExceeded), want: domain.ProviderStatusTimeout},
		{name: "upstream 429", err: fmt.Errorf("fetch: %w", statusErr(429)), want: domain.ProviderStatusRateLimited},
		{name: "upstream 503", err: statusErr(503), want: domain.ProviderStatusError},
		{name: "connection refused", err: upstreamDown, want: domain.ProviderStatusError},
		{name: "plain error", err: errors.New("bad payload"), want: domain.ProviderStatusError},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			results, degraded := fetchProviderResults(context.Background(), nil, domain.ProviderITunes, "id-1", "test.provider_failed",
				func(context.Context, domain.ProviderName, string) ([]domain.SearchResult, error) {
					return nil, tc.err
				})
			if results != nil || degraded == nil {
				t.Fatalf("results = %v, degraded = %v, want a degraded response", results, degraded)
			}
			if degraded.Status != tc.want || degraded.Unserved {
				t.Errorf("status = %v (unserved %v), want %v served", degraded.Status, degraded.Unserved, tc.want)
			}
			if degraded.Items == nil || len(degraded.Items) != 0 {
				t.Errorf("items = %v, want an empty slice", degraded.Items)
			}
		})
	}
}

func TestOkContentResponse_nilResultsCoercedToEmptySlice(t *testing.T) {
	resp := okContentResponse(domain.ProviderDeezer, nil, 10)
	if resp.Items == nil {
		t.Fatal("Items = nil, want a non-nil empty slice")
	}
	if len(resp.Items) != 0 {
		t.Fatalf("Items = %d, want 0", len(resp.Items))
	}
	if resp.Status != domain.ProviderStatusOK {
		t.Errorf("Status = %v, want OK", resp.Status)
	}
}
