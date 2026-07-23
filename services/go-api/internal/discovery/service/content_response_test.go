package service

import (
	"testing"

	"altune/go-api/internal/discovery/domain"
)

// A provider returning (nil, nil) must still honor the envelope contract: Items
// is always non-nil so the wire serializes [] rather than null.
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
