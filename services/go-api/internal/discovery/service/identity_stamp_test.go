package service

import (
	"altune/go-api/internal/discovery/domain"
	"context"
	"testing"
)

type fakeIdentityBridge struct {
	byMBID map[string]map[string]string
}

func (f *fakeIdentityBridge) ExternalIDs(_ context.Context, _ domain.ResultKind, mbid string) (map[string]string, bool) {
	ids, ok := f.byMBID[mbid]
	return ids, ok
}

func withMBID(r domain.SearchResult, mbid string) domain.SearchResult {
	r.MBID = mbid
	return r
}

func TestStampIdentities_StampsBridgedIDs(t *testing.T) {
	fb := &fakeIdentityBridge{byMBID: map[string]map[string]string{
		"mbid-1": {"deezer": "555"},
	}}
	s := NewService(nil, NewCircuitBreaker(), WithIdentityBridge(fb))

	groups := [][]domain.SearchResult{
		{withMBID(res(domain.ResultKindTrack, "Some Track", "Some Artist", domain.ProviderMusicBrainz, nil), "mbid-1")},
		{res(domain.ResultKindTrack, "No MBID Track", "Other Artist", domain.ProviderDeezer, nil)},
	}

	s.identity.stamp(context.Background(), groups)

	if groups[0][0].Xref["deezer"] != "555" {
		t.Fatalf("expected xref stamped on the MB result, xref=%v", groups[0][0].Xref)
	}
	if groups[1][0].Xref != nil {
		t.Fatalf("did not expect xref on the non-MB result")
	}
}

func TestStampIdentities_NoBridgeIsNoOp(t *testing.T) {
	s := NewService(nil, NewCircuitBreaker())
	groups := [][]domain.SearchResult{
		{withMBID(res(domain.ResultKindTrack, "Some Track", "Some Artist", domain.ProviderMusicBrainz, nil), "mbid-1")},
	}
	s.identity.stamp(context.Background(), groups)
	if groups[0][0].Xref != nil {
		t.Fatalf("nil bridge must be a no-op, but xref was stamped")
	}
}
