package domain

type MBEnrichment struct {
	MBID           string
	Genres         []string
	Year           int
	Rating         float64
	RatingVotes    int
	PrimaryType    string
	SecondaryTypes []string
	ExternalIDs    map[string]string
	ArtworkURL     string
}

func EmptyEnrichment() MBEnrichment {
	return MBEnrichment{
		Genres:         []string{},
		SecondaryTypes: []string{},
		ExternalIDs:    map[string]string{},
	}
}

func (e MBEnrichment) IsZero() bool {
	return e.MBID == "" &&
		len(e.Genres) == 0 &&
		e.Year == 0 &&
		e.Rating == 0 &&
		e.PrimaryType == "" &&
		len(e.SecondaryTypes) == 0 &&
		len(e.ExternalIDs) == 0 &&
		e.ArtworkURL == ""
}

func (e MBEnrichment) HasRenderableContent() bool {
	return len(e.Genres) > 0 ||
		e.Year > 0 ||
		e.Rating > 0 ||
		e.ArtworkURL != "" ||
		len(e.ExternalIDs) > 0
}
