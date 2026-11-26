package isrcwriteback_test

import (
	"altune/go-api/internal/download/internal/core/isrcwriteback"
	"altune/go-api/internal/download/internal/core/ports"
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
		facts      isrcwriteback.Facts
		wantISRC   string
		wantSource isrcwriteback.Source
	}{
		{
			name:       "the catalog's own ISRC is never replaced by a proven one",
			facts:      isrcwriteback.Facts{CatalogISRC: "USWB19901214", Identity: ports.RecordingIdentity{ISRC: "DEUM71724939", MBID: drinkingAlbum, MBIDs: []string{drinkingAlbum}}, HardVerdict: true, Surviving: survivingAlbum()},
			wantISRC:   "USWB19901214",
			wantSource: isrcwriteback.Catalog,
		},
		{
			name:       "the catalog's own ISRC stands with nothing else known",
			facts:      isrcwriteback.Facts{CatalogISRC: "USWB19901214"},
			wantISRC:   "USWB19901214",
			wantSource: isrcwriteback.Catalog,
		},
		{
			name:       "the pinned recording's ISRC",
			facts:      isrcwriteback.Facts{CatalogMBID: drinkingAlbum, Identity: albumIdentity()},
			wantISRC:   drinkingISRC,
			wantSource: isrcwriteback.PinnedRecording,
		},
		{
			name:       "a fingerprint that hard-matches the identity's recording proves its ISRC",
			facts:      isrcwriteback.Facts{Identity: albumIdentity(), HardVerdict: true, Surviving: survivingAlbum()},
			wantISRC:   drinkingISRC,
			wantSource: isrcwriteback.Fingerprint,
		},
		{
			name:       "an identity ISRC with no proof is unproven",
			facts:      isrcwriteback.Facts{Identity: ports.RecordingIdentity{ISRC: drinkingISRC, MBID: drinkingRemix, MBIDs: []string{drinkingRemix}}},
			wantSource: isrcwriteback.Unproven,
		},
		{
			name:       "a doubted reference does not prove its ISRC",
			facts:      isrcwriteback.Facts{Identity: doubted(albumIdentity()), HardVerdict: true, Surviving: survivingAlbum()},
			wantSource: isrcwriteback.Unproven,
		},
		{
			name:       "a hard verdict outside the reference set does not prove the ISRC",
			facts:      isrcwriteback.Facts{Identity: albumIdentity(), HardVerdict: true, Surviving: []ports.LinkedRecording{{MBID: "some-other-recording"}}},
			wantSource: isrcwriteback.Unproven,
		},
		{
			name:       "a soft verdict on the identity's recording does not prove the ISRC",
			facts:      isrcwriteback.Facts{Identity: albumIdentity(), Surviving: survivingAlbum()},
			wantSource: isrcwriteback.Unproven,
		},
		{
			name:       "a surviving recording with no MBID proves nothing",
			facts:      isrcwriteback.Facts{Identity: ports.RecordingIdentity{ISRC: drinkingISRC, MBIDs: []string{""}}, HardVerdict: true, Surviving: []ports.LinkedRecording{{}}},
			wantSource: isrcwriteback.Unproven,
		},
		{
			name:       "an identity with no MBID is not the pin of a track with none",
			facts:      isrcwriteback.Facts{Identity: ports.RecordingIdentity{ISRC: drinkingISRC}},
			wantSource: isrcwriteback.Unproven,
		},
		{
			name:       "a pin on another recording proves nothing",
			facts:      isrcwriteback.Facts{CatalogMBID: drinkingRemix, Identity: albumIdentity()},
			wantSource: isrcwriteback.Unproven,
		},
		{
			name:       "no ISRC anywhere is unproven even when pinned and fingerprinted",
			facts:      isrcwriteback.Facts{CatalogMBID: drinkingAlbum, Identity: ports.RecordingIdentity{MBID: drinkingAlbum, MBIDs: []string{drinkingAlbum}}, HardVerdict: true, Surviving: survivingAlbum()},
			wantSource: isrcwriteback.Unproven,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			isrc, source := isrcwriteback.Prove(tt.facts)
			if isrc != tt.wantISRC || source != tt.wantSource {
				t.Errorf("Prove = (%q, %q), want (%q, %q)", isrc, source, tt.wantISRC, tt.wantSource)
			}
		})
	}
}

func TestSource_NamesStoredInAcquisitionEvidenceStayStable(t *testing.T) {
	t.Parallel()
	stored := map[isrcwriteback.Source]string{
		isrcwriteback.Catalog:         "catalog",
		isrcwriteback.PinnedRecording: "pinned_recording",
		isrcwriteback.Fingerprint:     "fingerprint",
		isrcwriteback.Unproven:        "unproven",
	}
	for source, want := range stored {
		if string(source) != want {
			t.Errorf("isrc_source %q, want %q: acquisition_evidence rows and the eval adapter read this name", source, want)
		}
	}
}
