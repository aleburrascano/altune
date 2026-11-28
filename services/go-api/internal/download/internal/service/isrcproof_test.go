package service_test

import (
	"altune/go-api/internal/download/internal/domain"
	"altune/go-api/internal/download/internal/service"
	"testing"
)

const (
	drinkingISRC  = "CAA509814003"
	drinkingAlbum = "5d6efd30-23c6-4a9b-85af-706ea2df9022"
	drinkingRemix = "d3dc2825-20c0-4a4f-a963-baa589049146"
)

func albumIdentity() domain.RecordingIdentity {
	return domain.RecordingIdentity{ISRC: drinkingISRC, MBID: drinkingAlbum, MBIDs: []string{drinkingAlbum}}
}

func survivingAlbum() []domain.LinkedRecording {
	return []domain.LinkedRecording{{MBID: drinkingAlbum, Duration: 236}}
}

func doubted(identity domain.RecordingIdentity) domain.RecordingIdentity {
	identity.ReferenceDoubted = true
	return identity
}

func TestProve(t *testing.T) {
	tests := []struct {
		name       string
		facts      service.ProofFacts
		wantISRC   string
		wantSource domain.ProofSource
	}{
		{
			name:       "the catalog's own ISRC is never replaced by a proven one",
			facts:      service.ProofFacts{CatalogISRC: "USWB19901214", Identity: domain.RecordingIdentity{ISRC: "DEUM71724939", MBID: drinkingAlbum, MBIDs: []string{drinkingAlbum}}, HardVerdict: true, Surviving: survivingAlbum()},
			wantISRC:   "USWB19901214",
			wantSource: domain.ProofCatalog,
		},
		{
			name:       "the catalog's own ISRC stands with nothing else known",
			facts:      service.ProofFacts{CatalogISRC: "USWB19901214"},
			wantISRC:   "USWB19901214",
			wantSource: domain.ProofCatalog,
		},
		{
			name:       "the pinned recording's ISRC",
			facts:      service.ProofFacts{CatalogMBID: drinkingAlbum, Identity: albumIdentity()},
			wantISRC:   drinkingISRC,
			wantSource: domain.ProofPinnedRecording,
		},
		{
			name:       "a fingerprint that hard-matches the identity's recording proves its ISRC",
			facts:      service.ProofFacts{Identity: albumIdentity(), HardVerdict: true, Surviving: survivingAlbum()},
			wantISRC:   drinkingISRC,
			wantSource: domain.ProofFingerprint,
		},
		{
			name:       "an identity ISRC with no proof is unproven",
			facts:      service.ProofFacts{Identity: domain.RecordingIdentity{ISRC: drinkingISRC, MBID: drinkingRemix, MBIDs: []string{drinkingRemix}}},
			wantSource: domain.ProofUnproven,
		},
		{
			name:       "a doubted reference does not prove its ISRC",
			facts:      service.ProofFacts{Identity: doubted(albumIdentity()), HardVerdict: true, Surviving: survivingAlbum()},
			wantSource: domain.ProofUnproven,
		},
		{
			name:       "a hard verdict outside the reference set does not prove the ISRC",
			facts:      service.ProofFacts{Identity: albumIdentity(), HardVerdict: true, Surviving: []domain.LinkedRecording{{MBID: "some-other-recording"}}},
			wantSource: domain.ProofUnproven,
		},
		{
			name:       "a soft verdict on the identity's recording does not prove the ISRC",
			facts:      service.ProofFacts{Identity: albumIdentity(), Surviving: survivingAlbum()},
			wantSource: domain.ProofUnproven,
		},
		{
			name:       "a surviving recording with no MBID proves nothing",
			facts:      service.ProofFacts{Identity: domain.RecordingIdentity{ISRC: drinkingISRC, MBIDs: []string{""}}, HardVerdict: true, Surviving: []domain.LinkedRecording{{}}},
			wantSource: domain.ProofUnproven,
		},
		{
			name:       "an identity with no MBID is not the pin of a track with none",
			facts:      service.ProofFacts{Identity: domain.RecordingIdentity{ISRC: drinkingISRC}},
			wantSource: domain.ProofUnproven,
		},
		{
			name:       "a pin on another recording proves nothing",
			facts:      service.ProofFacts{CatalogMBID: drinkingRemix, Identity: albumIdentity()},
			wantSource: domain.ProofUnproven,
		},
		{
			name:       "no ISRC anywhere is unproven even when pinned and fingerprinted",
			facts:      service.ProofFacts{CatalogMBID: drinkingAlbum, Identity: domain.RecordingIdentity{MBID: drinkingAlbum, MBIDs: []string{drinkingAlbum}}, HardVerdict: true, Surviving: survivingAlbum()},
			wantSource: domain.ProofUnproven,
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
	stored := map[domain.ProofSource]string{
		domain.ProofCatalog:         "catalog",
		domain.ProofPinnedRecording: "pinned_recording",
		domain.ProofFingerprint:     "fingerprint",
		domain.ProofUnproven:        "unproven",
	}
	for source, want := range stored {
		if string(source) != want {
			t.Errorf("isrc_source %q, want %q: acquisition_evidence rows and the eval adapter read this name", source, want)
		}
	}
}
