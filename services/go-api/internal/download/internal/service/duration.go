package service

func (ac *AcquisitionContext) lengths() LengthPolicy {
	if ac.Identity.ReferenceDoubted {
		return DoubtedLengthPolicy(ac.Track.CatalogDuration)
	}
	return NewLengthPolicy(ac.Track.CatalogDuration, ac.Identity.Duration)
}

func (ac *AcquisitionContext) ExpectedLength() float64 {
	return ac.lengths().Expected()
}
