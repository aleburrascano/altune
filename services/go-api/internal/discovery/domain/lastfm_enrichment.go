package domain

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
