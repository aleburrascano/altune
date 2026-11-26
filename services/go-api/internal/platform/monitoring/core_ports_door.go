package monitoring

import ports "altune/go-api/internal/platform/monitoring/internal/core/ports"

type (
	AcquisitionJob           = ports.AcquisitionJob
	AcquisitionReader        = ports.AcquisitionReader
	DownloadStatusReport     = ports.DownloadStatusReport
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
