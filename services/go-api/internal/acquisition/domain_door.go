package acquisition

import domain "altune/go-api/internal/acquisition/domain"

type SourceName = domain.SourceName

var (
	IsYouTubeVideoID = domain.IsYouTubeVideoID
	SourceKey        = domain.SourceKey
)

var (
	ErrEmptyFingerprint  = domain.ErrEmptyFingerprint
	ErrIdentifyThrottled = domain.ErrIdentifyThrottled
	LengthAuthoritative  = domain.LengthAuthoritative
)
