package acquisition

import grade "altune/go-api/internal/acquisition/service/grade"

type (
	Grade      = grade.Grade
	SourceKind = grade.SourceKind
)

var GradeOf = grade.Of

const (
	GradeA                 = grade.A
	Approximates           = grade.Approximates
	ArtTrack               = grade.ArtTrack
	GradeB                 = grade.B
	GradeC                 = grade.C
	Contradicts            = grade.Contradicts
	GradeD                 = grade.D
	GradeF                 = grade.F
	FanUpload              = grade.FanUpload
	LabelUpload            = grade.LabelUpload
	Matches                = grade.Matches
	OfficialVideo          = grade.OfficialVideo
	UserUpload             = grade.UserUpload
	GradeAUser             = grade.AUser
	GradeBestAvailable     = grade.BestAvailable
	FingerprintSilent      = grade.Silent
	FingerprintUnsupported = grade.Unsupported
)

type GradeFingerprint = grade.Fingerprint
