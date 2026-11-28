package domain

import "altune/go-api/internal/shared/music"

const PipelineGeneration = 2

type Grade string

const (
	GradeA     Grade = "A"
	GradeAUser Grade = "A_user"
	GradeB     Grade = "B"
	GradeC     Grade = "C"
	GradeD     Grade = "D"
	GradeF     Grade = "F"

	BestAvailable Grade = "best_available"
)

type FingerprintResult int

const (
	FingerprintSilent FingerprintResult = iota
	FingerprintApproximates
	FingerprintMatches
	FingerprintContradicts
	FingerprintUnsupported
)

func GradeOf(fingerprint FingerprintResult, provenance music.AcquisitionProvenance, kind SourceKind) Grade {
	switch fingerprint {
	case FingerprintContradicts:
		return GradeF
	case FingerprintMatches:
		if kind.IsOfficial() {
			return GradeA
		}
		return GradeB
	case FingerprintApproximates:
		return GradeB
	case FingerprintUnsupported:
		return GradeD
	case FingerprintSilent:
	}
	if provenance == music.ProvenanceVerified || provenance == music.ProvenanceCorroborated {
		return GradeC
	}
	return GradeD
}
