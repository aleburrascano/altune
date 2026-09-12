package app

import (
	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/shared/config"
	"context"
	"strings"
	"testing"
)

// TestReRun_rejectsInvalidKindInsteadOfSilentDefault pins the reported gap: an
// unparseable kind token must surface a typed "invalid kinds" error (as the
// production /v1/discovery/search entry point does) rather than being silently
// dropped and replaced by the full default kind set, which would let the search
// fan out as if no filter was given.
func TestReRun_rejectsInvalidKindInsteadOfSilentDefault(t *testing.T) {
	ct := &countingTransport{}

	_, err := reRun(context.Background(), &config.Config{}, ct, func() map[string]float64 { return nil }, "kendrick", []string{"bogus"})
	if err == nil {
		t.Fatal("want typed invalid-kinds error for unparseable kind, got nil (filter silently dropped)")
	}
	if !strings.Contains(err.Error(), "invalid kinds") || !strings.Contains(err.Error(), "bogus") {
		t.Fatalf("want error naming the invalid kind, got %v", err)
	}
	if n := ct.calls.Load(); n != 0 {
		t.Errorf("invalid kinds must be rejected before fan-out, got %d provider calls", n)
	}
}

// TestInspectSearch_rejectsInvalidKindInsteadOfSilentDefault mirrors the ReRun
// case for the search-inspection entry point, which routes through the same
// parseSearchKinds helper.
func TestInspectSearch_rejectsInvalidKindInsteadOfSilentDefault(t *testing.T) {
	svc := inspectorForProvider(outageProvider{name: domain.ProviderDeezer, results: []domain.SearchResult{}})

	_, err := inspectSearch(context.Background(), svc, "kendrick", []string{"bogus"})
	if err == nil {
		t.Fatal("want typed invalid-kinds error for unparseable kind, got nil (filter silently dropped)")
	}
	if !strings.Contains(err.Error(), "invalid kinds") || !strings.Contains(err.Error(), "bogus") {
		t.Fatalf("want error naming the invalid kind, got %v", err)
	}
}
