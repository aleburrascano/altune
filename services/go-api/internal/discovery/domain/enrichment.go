package domain

type MBEnrichment struct {
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

func EmptyEnrichment() MBEnrichment {
	return MBEnrichment{
		Genres:         []string{},
		SecondaryTypes: []string{},
		ExternalIDs:    map[ProviderKey]string{},
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

type LastFmEnrichment struct {
	MBID      string
	Listeners int64
	Playcount int64
	Tags      []string
	Bio       string
	Similar   []string
	Duration  int
	Album     string
}

func EmptyLastFmEnrichment() LastFmEnrichment {
	return LastFmEnrichment{
		Tags:    []string{},
		Similar: []string{},
	}
}

func (e LastFmEnrichment) IsZero() bool {
	return e.MBID == "" &&
		e.Listeners == 0 &&
		e.Playcount == 0 &&
		len(e.Tags) == 0 &&
		e.Bio == "" &&
		len(e.Similar) == 0 &&
		e.Duration == 0 &&
		e.Album == ""
}

func (e LastFmEnrichment) HasRenderableContent() bool {
	return e.Listeners > 0 ||
		e.Playcount > 0 ||
		len(e.Tags) > 0 ||
		e.Bio != "" ||
		len(e.Similar) > 0
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
