package download

import grade "altune/go-api/internal/download/internal/core/audioquality"

type (
	Grade      = grade.Grade
	SourceKind = grade.SourceKind
)

var GradeOf = grade.Of

const (
	GradeA                 = grade.A
	GradeAUser             = grade.AUser
	Approximates           = grade.Approximates
	ArtTrack               = grade.ArtTrack
	GradeB                 = grade.B
	GradeBestAvailable     = grade.BestAvailable
	GradeC                 = grade.C
	Contradicts            = grade.Contradicts
	GradeD                 = grade.D
	GradeF                 = grade.F
	FanUpload              = grade.FanUpload
	LabelUpload            = grade.LabelUpload
	Matches                = grade.Matches
	OfficialVideo          = grade.OfficialVideo
	FingerprintSilent      = grade.Silent
	FingerprintUnsupported = grade.Unsupported
	UserUpload             = grade.UserUpload
)

type FingerprintResult = grade.FingerprintResult
