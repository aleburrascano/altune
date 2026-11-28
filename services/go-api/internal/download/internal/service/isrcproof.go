package service

import (
	"altune/go-api/internal/download/internal/ports"
	"slices"
)

type ProofSource string

const (
	ProofCatalog         ProofSource = "catalog"
	ProofPinnedRecording ProofSource = "pinned_recording"
	ProofFingerprint     ProofSource = "fingerprint"
	ProofUnproven        ProofSource = "unproven"
)

type ProofFacts struct {
	CatalogISRC string
	CatalogMBID string
	Identity    ports.RecordingIdentity
	HardVerdict bool
	Surviving   []ports.LinkedRecording
}

func ProveISRC(f ProofFacts) (string, ProofSource) {
	switch {
	case f.CatalogISRC != "":
		return f.CatalogISRC, ProofCatalog
	case f.Identity.ISRC == "":
		return "", ProofUnproven
	case f.identityIsCatalogPin():
		return f.Identity.ISRC, ProofPinnedRecording
	case f.fingerprintProvesIdentity():
		return f.Identity.ISRC, ProofFingerprint
	}
	return "", ProofUnproven
}

func (f ProofFacts) identityIsCatalogPin() bool {
	return f.CatalogMBID != "" && f.Identity.MBID == f.CatalogMBID
}

func (f ProofFacts) fingerprintProvesIdentity() bool {
	if !f.HardVerdict || f.Identity.ReferenceDoubted {
		return false
	}
	return slices.ContainsFunc(f.Surviving, func(recording ports.LinkedRecording) bool {
		return recording.MBID != "" && slices.Contains(f.Identity.MBIDs, recording.MBID)
	})
}
