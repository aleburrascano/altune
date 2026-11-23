package isrcproof_test

import (
	"altune/go-api/internal/acquisition/internal/core/isrcproof"
	"altune/go-api/internal/acquisition/internal/core/ports"
	"testing"
)

const (
	drinkingISRC  = "CAA509814003"
	drinkingAlbum = "5d6efd30-23c6-4a9b-85af-706ea2df9022"
	drinkingRemix = "d3dc2825-20c0-4a4f-a963-baa589049146"
)

func albumIdentity() ports.RecordingIdentity {
	return ports.RecordingIdentity{ISRC: drinkingISRC, MBID: drinkingAlbum, MBIDs: []string{drinkingAlbum}}
}

func survivingAlbum() []ports.LinkedRecording {
	return []ports.LinkedRecording{{MBID: drinkingAlbum, Duration: 236}}
}

func doubted(identity ports.RecordingIdentity) ports.RecordingIdentity {
	identity.ReferenceDoubted = true
	return identity
}

func TestProve(t *testing.T) {
	tests := []struct {
		name       string
		facts      isrcproof.Facts
		wantISRC   string
		wantSource isrcproof.Source
	}{
		{
			name:       "the catalog's own ISRC is never replaced by a proven one",
			facts:      isrcproof.Facts{CatalogISRC: "USWB19901214", Identity: ports.RecordingIdentity{ISRC: "DEUM71724939", MBID: drinkingAlbum, MBIDs: []string{drinkingAlbum}}, HardVerdict: true, Surviving: survivingAlbum()},
			wantISRC:   "USWB19901214",
			wantSource: isrcproof.Catalog,
		},
		{
			name:       "the catalog's own ISRC stands with nothing else known",
			facts:      isrcproof.Facts{CatalogISRC: "USWB19901214"},
			wantISRC:   "USWB19901214",
			wantSource: isrcproof.Catalog,
		},
		{
			name:       "the pinned recording's ISRC",
			facts:      isrcproof.Facts{CatalogMBID: drinkingAlbum, Identity: albumIdentity()},
			wantISRC:   drinkingISRC,
			wantSource: isrcproof.PinnedRecording,
		},
		{
			name:       "a fingerprint that hard-matches the identity's recording proves its ISRC",
			facts:      isrcproof.Facts{Identity: albumIdentity(), HardVerdict: true, Surviving: survivingAlbum()},
			wantISRC:   drinkingISRC,
			wantSource: isrcproof.Fingerprint,
		},
		{
			name:       "an identity ISRC with no proof is unproven",
			facts:      isrcproof.Facts{Identity: ports.RecordingIdentity{ISRC: drinkingISRC, MBID: drinkingRemix, MBIDs: []string{drinkingRemix}}},
			wantSource: isrcproof.Unproven,
		},
		{
			name:       "a doubted reference does not prove its ISRC",
			facts:      isrcproof.Facts{Identity: doubted(albumIdentity()), HardVerdict: true, Surviving: survivingAlbum()},
			wantSource: isrcproof.Unproven,
		},
		{
			name:       "a hard verdict outside the reference set does not prove the ISRC",
			facts:      isrcproof.Facts{Identity: albumIdentity(), HardVerdict: true, Surviving: []ports.LinkedRecording{{MBID: "some-other-recording"}}},
			wantSource: isrcproof.Unproven,
		},
		{
			name:       "a soft verdict on the identity's recording does not prove the ISRC",
			facts:      isrcproof.Facts{Identity: albumIdentity(), Surviving: survivingAlbum()},
			wantSource: isrcproof.Unproven,
		},
		{
			name:       "a surviving recording with no MBID proves nothing",
			facts:      isrcproof.Facts{Identity: ports.RecordingIdentity{ISRC: drinkingISRC, MBIDs: []string{""}}, HardVerdict: true, Surviving: []ports.LinkedRecording{{}}},
			wantSource: isrcproof.Unproven,
		},
		{
			name:       "an identity with no MBID is not the pin of a track with none",
			facts:      isrcproof.Facts{Identity: ports.RecordingIdentity{ISRC: drinkingISRC}},
			wantSource: isrcproof.Unproven,
		},
		{
			name:       "a pin on another recording proves nothing",
			facts:      isrcproof.Facts{CatalogMBID: drinkingRemix, Identity: albumIdentity()},
			wantSource: isrcproof.Unproven,
		},
		{
			name:       "no ISRC anywhere is unproven even when pinned and fingerprinted",
			facts:      isrcproof.Facts{CatalogMBID: drinkingAlbum, Identity: ports.RecordingIdentity{MBID: drinkingAlbum, MBIDs: []string{drinkingAlbum}}, HardVerdict: true, Surviving: survivingAlbum()},
			wantSource: isrcproof.Unproven,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			isrc, source := isrcproof.Prove(tt.facts)
			if isrc != tt.wantISRC || source != tt.wantSource {
				t.Errorf("Prove = (%q, %q), want (%q, %q)", isrc, source, tt.wantISRC, tt.wantSource)
			}
		})
	}
}

func TestSource_NamesStoredInAcquisitionEvidenceStayStable(t *testing.T) {
	t.Parallel()
	stored := map[isrcproof.Source]string{
		isrcproof.Catalog:         "catalog",
		isrcproof.PinnedRecording: "pinned_recording",
		isrcproof.Fingerprint:     "fingerprint",
		isrcproof.Unproven:        "unproven",
	}
	for source, want := range stored {
		if string(source) != want {
			t.Errorf("isrc_source %q, want %q: acquisition_evidence rows and the eval adapter read this name", source, want)
		}
	}
}
