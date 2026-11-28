package domain

type RecordingFacts struct {
	MBID           string
	Title          string
	Disambiguation string
	Artists        []string
	ISRCs          []string
	Duration       float64
}

type ISRCRecording struct {
	MBID     string
	Duration float64
}

type RerecordingQuery struct {
	OriginalMBID string
}

type WorkRecording struct {
	MBID           string
	Title          string
	Disambiguation string
	ArtistIDs      []string
	Live           bool
}

type RecordingRelease struct {
	Title  string
	Date   string
	Status string
}

type RecordingDetail struct {
	MBID             string
	Title            string
	ArtistIDs        []string
	ISRCs            []string
	Duration         float64
	FirstReleaseDate string
	Releases         []RecordingRelease
}
