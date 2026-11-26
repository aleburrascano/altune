package audioquality

import "altune/go-api/internal/shared/downloadprovenance"

const PipelineGeneration = 2

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

type FingerprintResult int

const (
	Silent FingerprintResult = iota
	Approximates
	Matches
	Contradicts
	Unsupported
)

func Of(fingerprint FingerprintResult, provenance downloadprovenance.AcquisitionProvenance, kind SourceKind) Grade {
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
	case Silent:
	}
	if provenance == downloadprovenance.ProvenanceVerified || provenance == downloadprovenance.ProvenanceCorroborated {
		return C
	}
	return D
}
