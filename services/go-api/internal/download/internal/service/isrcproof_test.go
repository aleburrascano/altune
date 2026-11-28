package service_test

import (
	"altune/go-api/internal/download/internal/ports"
	"altune/go-api/internal/download/internal/service"
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
		facts      service.ProofFacts
		wantISRC   string
		wantSource service.ProofSource
	}{
		{
			name:       "the catalog's own ISRC is never replaced by a proven one",
			facts:      service.ProofFacts{CatalogISRC: "USWB19901214", Identity: ports.RecordingIdentity{ISRC: "DEUM71724939", MBID: drinkingAlbum, MBIDs: []string{drinkingAlbum}}, HardVerdict: true, Surviving: survivingAlbum()},
			wantISRC:   "USWB19901214",
			wantSource: service.ProofCatalog,
		},
		{
			name:       "the catalog's own ISRC stands with nothing else known",
			facts:      service.ProofFacts{CatalogISRC: "USWB19901214"},
			wantISRC:   "USWB19901214",
			wantSource: service.ProofCatalog,
		},
		{
			name:       "the pinned recording's ISRC",
			facts:      service.ProofFacts{CatalogMBID: drinkingAlbum, Identity: albumIdentity()},
			wantISRC:   drinkingISRC,
			wantSource: service.ProofPinnedRecording,
		},
		{
			name:       "a fingerprint that hard-matches the identity's recording proves its ISRC",
			facts:      service.ProofFacts{Identity: albumIdentity(), HardVerdict: true, Surviving: survivingAlbum()},
			wantISRC:   drinkingISRC,
			wantSource: service.ProofFingerprint,
		},
		{
			name:       "an identity ISRC with no proof is unproven",
			facts:      service.ProofFacts{Identity: ports.RecordingIdentity{ISRC: drinkingISRC, MBID: drinkingRemix, MBIDs: []string{drinkingRemix}}},
			wantSource: service.ProofUnproven,
		},
		{
			name:       "a doubted reference does not prove its ISRC",
			facts:      service.ProofFacts{Identity: doubted(albumIdentity()), HardVerdict: true, Surviving: survivingAlbum()},
			wantSource: service.ProofUnproven,
		},
		{
			name:       "a hard verdict outside the reference set does not prove the ISRC",
			facts:      service.ProofFacts{Identity: albumIdentity(), HardVerdict: true, Surviving: []ports.LinkedRecording{{MBID: "some-other-recording"}}},
			wantSource: service.ProofUnproven,
		},
		{
			name:       "a soft verdict on the identity's recording does not prove the ISRC",
			facts:      service.ProofFacts{Identity: albumIdentity(), Surviving: survivingAlbum()},
			wantSource: service.ProofUnproven,
		},
		{
			name:       "a surviving recording with no MBID proves nothing",
			facts:      service.ProofFacts{Identity: ports.RecordingIdentity{ISRC: drinkingISRC, MBIDs: []string{""}}, HardVerdict: true, Surviving: []ports.LinkedRecording{{}}},
			wantSource: service.ProofUnproven,
		},
		{
			name:       "an identity with no MBID is not the pin of a track with none",
			facts:      service.ProofFacts{Identity: ports.RecordingIdentity{ISRC: drinkingISRC}},
			wantSource: service.ProofUnproven,
		},
		{
			name:       "a pin on another recording proves nothing",
			facts:      service.ProofFacts{CatalogMBID: drinkingRemix, Identity: albumIdentity()},
			wantSource: service.ProofUnproven,
		},
		{
			name:       "no ISRC anywhere is unproven even when pinned and fingerprinted",
			facts:      service.ProofFacts{CatalogMBID: drinkingAlbum, Identity: ports.RecordingIdentity{MBID: drinkingAlbum, MBIDs: []string{drinkingAlbum}}, HardVerdict: true, Surviving: survivingAlbum()},
			wantSource: service.ProofUnproven,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			isrc, source := service.ProveISRC(tt.facts)
			if isrc != tt.wantISRC || source != tt.wantSource {
				t.Errorf("Prove = (%q, %q), want (%q, %q)", isrc, source, tt.wantISRC, tt.wantSource)
			}
		})
	}
}

func TestSource_NamesStoredInAcquisitionEvidenceStayStable(t *testing.T) {
	t.Parallel()
	stored := map[service.ProofSource]string{
		service.ProofCatalog:         "catalog",
		service.ProofPinnedRecording: "pinned_recording",
		service.ProofFingerprint:     "fingerprint",
		service.ProofUnproven:        "unproven",
	}
	for source, want := range stored {
		if string(source) != want {
			t.Errorf("isrc_source %q, want %q: acquisition_evidence rows and the eval adapter read this name", source, want)
		}
	}
}
