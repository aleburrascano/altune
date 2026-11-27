package service

import "altune/go-api/internal/download/internal/core/tracklength"

func (ac *AcquisitionContext) lengths() tracklength.Policy {
	if ac.Identity.ReferenceDoubted {
		return tracklength.Doubted(ac.Track.CatalogDuration)
	}
	return tracklength.New(ac.Track.CatalogDuration, ac.Identity.Duration)
}

func (ac *AcquisitionContext) ExpectedLength() float64 {
	return ac.lengths().Expected()
}
