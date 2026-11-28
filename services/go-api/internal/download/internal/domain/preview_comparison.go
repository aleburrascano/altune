package domain

type PreviewTarget struct {
	ISRC          string
	DeezerTrackID string
}

type PreviewComparison struct {
	Matched        bool
	BitErrorRate   float64
	OverlapSeconds float64
	Unavailable    string
}
