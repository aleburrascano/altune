package service

import (
	"altune/go-api/internal/download/internal/domain"
	"slices"
)

type ProofFacts struct {
	CatalogISRC string
	CatalogMBID string
	Identity    domain.RecordingIdentity
	HardVerdict bool
	Surviving   []domain.LinkedRecording
}

func ProveISRC(f ProofFacts) (string, domain.ProofSource) {
	switch {
	case f.CatalogISRC != "":
		return f.CatalogISRC, domain.ProofCatalog
	case f.Identity.ISRC == "":
		return "", domain.ProofUnproven
	case f.identityIsCatalogPin():
		return f.Identity.ISRC, domain.ProofPinnedRecording
	case f.fingerprintProvesIdentity():
		return f.Identity.ISRC, domain.ProofFingerprint
	}
	return "", domain.ProofUnproven
}

func (f ProofFacts) identityIsCatalogPin() bool {
	return f.CatalogMBID != "" && f.Identity.MBID == f.CatalogMBID
}

func (f ProofFacts) fingerprintProvesIdentity() bool {
	if !f.HardVerdict || f.Identity.ReferenceDoubted {
		return false
	}
	return slices.ContainsFunc(f.Surviving, func(recording domain.LinkedRecording) bool {
		return recording.MBID != "" && slices.Contains(f.Identity.MBIDs, recording.MBID)
	})
}
