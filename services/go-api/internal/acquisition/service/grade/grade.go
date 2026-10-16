package grade

import "altune/go-api/internal/shared"

const PipelineGeneration = 1

type Grade string

const (
	A     Grade = "A"
	AUser Grade = "A_user"
	B     Grade = "B"
	C     Grade = "C"
	D     Grade = "D"
	F     Grade = "F"

	BestAvailable Grade = "best_available"
)

type Fingerprint int

const (
	Silent Fingerprint = iota
	Approximates
	Matches
	Contradicts
	Unsupported
)

func Of(fingerprint Fingerprint, provenance shared.AcquisitionProvenance, kind SourceKind) Grade {
	switch fingerprint {
	case Contradicts:
		return F
	case Matches:
		if kind.IsOfficial() {
			return A
		}
		return B
	case Approximates:
		return B
	case Unsupported:
		return D
	}
	if provenance == shared.ProvenanceVerified || provenance == shared.ProvenanceCorroborated {
		return C
	}
	return D
}
