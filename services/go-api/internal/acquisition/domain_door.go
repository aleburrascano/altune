package acquisition

import (
	domain "altune/go-api/internal/acquisition/internal/core/domain"
	"altune/go-api/internal/acquisition/sourcekey"
)

type SourceName = domain.SourceName

var (
	IsYouTubeVideoID = sourcekey.IsYouTubeVideoID
	SourceKey        = sourcekey.SourceKey
)

var (
	ErrEmptyFingerprint  = domain.ErrEmptyFingerprint
	ErrIdentifyThrottled = domain.ErrIdentifyThrottled
	LengthAuthoritative  = domain.LengthAuthoritative
)

type LengthTolerance = domain.LengthTolerance

var (
	LengthLoose = domain.LengthLoose
	LengthTight = domain.LengthTight
)

var (
	CreditBrackets  = domain.CreditBrackets
	FeaturedCredits = domain.FeaturedCredits
	SplitCredit     = domain.SplitCredit
)
