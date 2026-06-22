package service

import (
	"context"
	"testing"

	"altune/go-api/internal/discovery/domain"
)

// identity_bridge_test pins the ADR-0011 cross-provider identity bridge — the
// accepted-but-previously-unverified merge tier. Two surfaces:
//   - stampIdentities (service): reads the IdentityBridge and stamps xref ids.
//   - Merge bridgeMatch (pure): two differently-titled results merge by a shared
//     bridged id, recorded as the high-confidence bridge tier.

// fakeIdentityBridge returns the external ids for a known mbid, mimicking the
// warmed enrichment cache.
type fakeIdentityBridge struct {
	byMBID map[string]map[string]string
}

func (f *fakeIdentityBridge) ExternalIDs(_ context.Context, _ domain.ResultKind, mbid string) (map[string]string, bool) {
	ids, ok := f.byMBID[mbid]
	return ids, ok
}

func TestStampIdentities_StampsBridgedIDs(t *testing.T) {
	fb := &fakeIdentityBridge{byMBID: map[string]map[string]string{
		"mbid-1": {"deezer": "555"},
	}}
	s := NewService(nil, NewCircuitBreaker(), WithIdentityBridge(fb))

	groups := [][]domain.SearchResult{
		{res(domain.ResultKindTrack, "Some Track", "Some Artist", domain.ProviderMusicBrainz, map[string]any{"mbid": "mbid-1"})},
		{res(domain.ResultKindTrack, "No MBID Track", "Other Artist", domain.ProviderDeezer, nil)},
	}

	s.stampIdentities(context.Background(), groups)

	xref, ok := groups[0][0].Extras["xref"].(map[string]string)
	if !ok {
		t.Fatalf("expected xref stamped on the MB result, extras=%v", groups[0][0].Extras)
	}
	if xref["deezer"] != "555" {
		t.Fatalf("xref deezer id = %q, want 555", xref["deezer"])
	}
	// The non-MB result carries no mbid, so nothing is stamped.
	if _, stamped := groups[1][0].Extras["xref"]; stamped {
		t.Fatalf("did not expect xref on the non-MB result")
	}
}

func TestStampIdentities_NoBridgeIsNoOp(t *testing.T) {
	s := NewService(nil, NewCircuitBreaker()) // no IdentityBridge wired
	groups := [][]domain.SearchResult{
		{res(domain.ResultKindTrack, "Some Track", "Some Artist", domain.ProviderMusicBrainz, map[string]any{"mbid": "mbid-1"})},
	}
	s.stampIdentities(context.Background(), groups)
	if _, stamped := groups[0][0].Extras["xref"]; stamped {
		t.Fatalf("nil bridge must be a no-op, but xref was stamped")
	}
}

// The merge tier itself: two results with DIFFERENT titles (so no name match can
// merge them) fold into one entity solely because one carries a bridged id that
// matches the other's native source id — recorded as the high-confidence bridge
// tier.
func TestMerge_BridgeTierMergesCrossProvider(t *testing.T) {
	mb := res(domain.ResultKindTrack, "Bridge Recording One", "Artist X", domain.ProviderMusicBrainz, map[string]any{
		"mbid": "mbid-1",
		"xref": map[string]string{"deezer": "555"},
	})
	dz := domain.SearchResult{
		Kind:     domain.ResultKindTrack,
		Title:    "Totally Different Title",
		Subtitle: "Artist X",
		Sources:  []domain.SourceRef{{Provider: domain.ProviderDeezer, ExternalID: "555", URL: "https://deezer/555"}},
		Extras:   map[string]any{},
	}

	entities := Merge([][]domain.SearchResult{{mb}, {dz}})

	if len(entities) != 1 {
		t.Fatalf("bridge merge failed: got %d entities, want 1 (bridge did not fire)", len(entities))
	}
	if tier := entities[0].Result.Extras["resolution_tier"]; tier != domain.EntityResolutionBridge.String() {
		t.Fatalf("resolution tier = %v, want %q", tier, domain.EntityResolutionBridge.String())
	}
	if entities[0].Result.Confidence != domain.ConfidenceHigh {
		t.Fatalf("bridge merge should be high confidence, got %v", entities[0].Result.Confidence)
	}
}

// A bridge match requires a stamped xref to participate: two native ids alone
// (no xref) are not a cross-provider bridge.
func TestMerge_NoBridgeWithoutXref(t *testing.T) {
	a := res(domain.ResultKindTrack, "Distinct One", "Artist X", domain.ProviderMusicBrainz, map[string]any{"mbid": "mbid-1"})
	b := domain.SearchResult{
		Kind:     domain.ResultKindTrack,
		Title:    "Distinct Two",
		Subtitle: "Artist X",
		Sources:  []domain.SourceRef{{Provider: domain.ProviderDeezer, ExternalID: "555", URL: "https://deezer/555"}},
		Extras:   map[string]any{},
	}
	entities := Merge([][]domain.SearchResult{{a}, {b}})
	if len(entities) != 2 {
		t.Fatalf("without an xref these distinct-title results must not merge: got %d entities, want 2", len(entities))
	}
}
