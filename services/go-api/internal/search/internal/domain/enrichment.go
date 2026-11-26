package domain

type MusicBrainzEnrichment struct {
	MBID           string
	Genres         []string
	Year           int
	Rating         float64
	RatingVotes    int
	PrimaryType    string
	SecondaryTypes []string
	ExternalIDs    map[ProviderKey]string
	ArtworkURL     string
}

func EmptyEnrichment() MusicBrainzEnrichment {
	return MusicBrainzEnrichment{
		Genres:         []string{},
		SecondaryTypes: []string{},
		ExternalIDs:    map[ProviderKey]string{},
	}
}

func (e MusicBrainzEnrichment) IsZero() bool {
	return e.MBID == "" &&
		len(e.Genres) == 0 &&
		e.Year == 0 &&
		e.Rating == 0 &&
		e.PrimaryType == "" &&
		len(e.SecondaryTypes) == 0 &&
		len(e.ExternalIDs) == 0 &&
		e.ArtworkURL == ""
}

func (e MusicBrainzEnrichment) HasRenderableContent() bool {
	return len(e.Genres) > 0 ||
		e.Year > 0 ||
		e.Rating > 0 ||
		e.ArtworkURL != "" ||
		len(e.ExternalIDs) > 0
}

type DeezerEnrichment struct {
	BPM        int
	Gain       float64
	Explicit   bool
	Label      string
	Genres     []string
	UPC        string
	RecordType RecordType
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

func (e DeezerEnrichment) HasRenderableContent() bool {
	return e.BPM > 0 ||
		e.Explicit ||
		e.Label != "" ||
		len(e.Genres) > 0 ||
		e.RecordType != "" ||
		len(e.Featured) > 0
}

type DeezerLyrics struct {
	Plain       string
	SyncedLines []SyncedLyricLine
	Writers     []string
	Copyright   string
}

type SyncedLyricLine struct {
	Timecode     string
	Line         string
	Milliseconds int64
	Duration     int64
}

func EmptyDeezerLyrics() DeezerLyrics {
	return DeezerLyrics{SyncedLines: []SyncedLyricLine{}, Writers: []string{}}
}

func (l DeezerLyrics) IsZero() bool {
	return l.Plain == "" && len(l.SyncedLines) == 0
}
