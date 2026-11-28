package domain

type ProofSource string

const (
	ProofCatalog         ProofSource = "catalog"
	ProofPinnedRecording ProofSource = "pinned_recording"
	ProofFingerprint     ProofSource = "fingerprint"
	ProofUnproven        ProofSource = "unproven"
)

type AcquisitionEvidence struct {
	Verdict              string
	AcoustIDScore        float64
	DurationDeltaSeconds *float64
	Channel              string
	Qualifiers           []string
	Resolved             bool
	SourceTitle          string
	ISRCSource           ProofSource
	SourceKind           SourceKind
	PipelineGeneration   int
	Grade                Grade
	SourceFormatID       string
	SourceKbps           int
}

type InPlaceEvidence struct {
	Grade             Grade
	SourceKind        SourceKind
	InPlaceGeneration int
	SourceFormatID    string
	SourceKbps        int
}

func (e InPlaceEvidence) Format() SourceFormat {
	return SourceFormat{ID: e.SourceFormatID, Kbps: e.SourceKbps}
}
