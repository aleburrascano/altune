package download

import (
	domain "altune/go-api/internal/download/internal/core/domain"
	"altune/go-api/internal/download/sourcekey"
)

type SourceName = domain.SourceName

var (
	IsYouTubeVideoID = sourcekey.IsYouTubeVideoID
	SourceKey        = sourcekey.SourceKey
)

var (
	ErrEmptyFingerprint  = domain.ErrEmptyFingerprint
	ErrIdentifyThrottled = domain.ErrIdentifyThrottled
)

type LengthMargin = domain.LengthMargin

var (
	LengthLoose = domain.LengthLoose
	LengthTight = domain.LengthTight
)

var (
	CreditBrackets  = domain.CreditBrackets
	FeaturedCredits = domain.FeaturedCredits
	SplitCredit     = domain.SplitCredit
)
