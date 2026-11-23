package observe

import ports "altune/go-api/internal/observe/internal/core/ports"

type (
	AcquisitionJob           = ports.AcquisitionJob
	AcquisitionReader        = ports.AcquisitionReader
	AcquisitionStatus        = ports.AcquisitionStatus
	AcquisitionTools         = ports.AcquisitionTools
	AlertNotifier            = ports.AlertNotifier
	DiscographyCase          = ports.DiscographyCase
	DiscographyGroupBy       = ports.DiscographyGroupBy
	DiscographyQualityReader = ports.DiscographyQualityReader
	DiscographySuspectRate   = ports.DiscographySuspectRate
)

const (
	GroupByArtist            = ports.GroupByArtist
	GroupByContaminationBand = ports.GroupByContaminationBand
	GroupByProvider          = ports.GroupByProvider
)
