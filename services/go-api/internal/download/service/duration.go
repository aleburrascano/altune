package service

import "altune/go-api/internal/download/internal/core/tracklength"

func (ac *AcquisitionContext) lengths() tracklength.Policy {
	return tracklength.New(ac.Track.CatalogDuration, ac.Identity.Duration, ac.Identity.ReferenceDoubted)
}

func (ac *AcquisitionContext) ExpectedLength() float64 {
	return ac.lengths().Expected()
}
