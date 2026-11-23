package isrcproof

import (
	"altune/go-api/internal/acquisition/internal/core/ports"
	"slices"
)

type Source string

const (
	Catalog         Source = "catalog"
	PinnedRecording Source = "pinned_recording"
	Fingerprint     Source = "fingerprint"
	Unproven        Source = "unproven"
)

type Facts struct {
	CatalogISRC string
	CatalogMBID string
	Identity    ports.RecordingIdentity
	HardVerdict bool
	Surviving   []ports.LinkedRecording
}

func Prove(f Facts) (string, Source) {
	switch {
	case f.CatalogISRC != "":
		return f.CatalogISRC, Catalog
	case f.Identity.ISRC == "":
		return "", Unproven
	case f.identityIsCatalogPin():
		return f.Identity.ISRC, PinnedRecording
	case f.fingerprintProvesIdentity():
		return f.Identity.ISRC, Fingerprint
	}
	return "", Unproven
}

func (f Facts) identityIsCatalogPin() bool {
	return f.CatalogMBID != "" && f.Identity.MBID == f.CatalogMBID
}

func (f Facts) fingerprintProvesIdentity() bool {
	if !f.HardVerdict || f.Identity.ReferenceDoubted {
		return false
	}
	return slices.ContainsFunc(f.Surviving, func(recording ports.LinkedRecording) bool {
		return recording.MBID != "" && slices.Contains(f.Identity.MBIDs, recording.MBID)
	})
}
