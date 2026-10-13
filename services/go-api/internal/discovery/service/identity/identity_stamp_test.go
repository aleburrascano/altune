package identity

import (
	"altune/go-api/internal/discovery/discoverytest"
	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/shared/runloop"
	"context"
	"testing"
)

type fakeIdentityBridge struct {
	byMBID map[string]map[domain.ProviderKey]string
}

func (f *fakeIdentityBridge) ExternalIDs(_ context.Context, _ domain.ResultKind, mbid string) (map[domain.ProviderKey]string, bool) {
	ids, ok := f.byMBID[mbid]
	return ids, ok
}

func TestStampIdentities_StampsBridgedIDs(t *testing.T) {
	fb := &fakeIdentityBridge{byMBID: map[string]map[domain.ProviderKey]string{
		"mbid-1": {"deezer": "555"},
	}}
	s := NewIdentityStamper(fb, nil, nil, &runloop.Tracker{})

	groups := [][]domain.SearchResult{
		{discoverytest.WithMBID(discoverytest.Res(domain.ResultKindTrack, "Some Track", "Some Artist", domain.ProviderMusicBrainz, nil), "mbid-1")},
		{discoverytest.Res(domain.ResultKindTrack, "No MBID Track", "Other Artist", domain.ProviderDeezer, nil)},
	}

	s.Stamp(context.Background(), groups)

	if groups[0][0].Xref["deezer"] != "555" {
		t.Fatalf("expected xref stamped on the MB result, xref=%v", groups[0][0].Xref)
	}
	if groups[1][0].Xref != nil {
		t.Fatalf("did not expect xref on the non-MB result")
	}
}

func TestStampIdentities_NoBridgeIsNoOp(t *testing.T) {
	s := NewIdentityStamper(nil, nil, nil, &runloop.Tracker{})
	groups := [][]domain.SearchResult{
		{discoverytest.WithMBID(discoverytest.Res(domain.ResultKindTrack, "Some Track", "Some Artist", domain.ProviderMusicBrainz, nil), "mbid-1")},
	}
	s.Stamp(context.Background(), groups)
	if groups[0][0].Xref != nil {
		t.Fatalf("nil bridge must be a no-op, but xref was stamped")
	}
}
