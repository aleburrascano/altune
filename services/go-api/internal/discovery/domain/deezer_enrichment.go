package domain

type DeezerEnrichment struct {
	BPM        int
	Gain       float64
	Explicit   bool
	Label      string
	Genres     []string
	UPC        string
	RecordType string
	Featured   []FeaturedArtist
}

func EmptyDeezerEnrichment() DeezerEnrichment {
	return DeezerEnrichment{Genres: []string{}}
}

func (e DeezerEnrichment) IsZero() bool {
	return e.BPM == 0 &&
		e.Gain == 0 &&
		!e.Explicit &&
		e.Label == "" &&
		len(e.Genres) == 0 &&
		e.UPC == "" &&
		e.RecordType == ""
}
