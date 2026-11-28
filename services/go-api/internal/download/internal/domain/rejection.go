package domain

type RejectionBasis struct {
	ExpectedSeconds float64
	ReferenceMBID   string
}

type CandidateRejectionRecord struct {
	TrackID   string
	SourceKey string
	Reason    string
	Detail    string
	Basis     RejectionBasis
}

type PriorRejection struct {
	SourceKey string
	Basis     *RejectionBasis
}
