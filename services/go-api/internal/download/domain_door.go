package download

import (
	domain "altune/go-api/internal/download/internal/core/domain"
	"altune/go-api/internal/download/sourcekey"
)

type SourceName = domain.SourceName

var IsYouTubeVideoID = sourcekey.IsYouTubeVideoID

var ErrIdentifyThrottled = domain.ErrIdentifyThrottled
