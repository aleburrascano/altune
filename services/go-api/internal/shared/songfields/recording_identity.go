package songfields

type ProviderRef struct {
	Provider   string
	ExternalID string
	URL        string
}

type TrackPosition struct {
	Disc  int
	Track int
}

type Contributor struct {
	ProviderID string
	Name       string
	Role       string
}

type RecordingIdentity struct {
	Title            string
	Artist           string
	Album            string
	Year             int
	ReleaseDate      string
	RecordType       string
	ISRC             string
	RecordingMBID    string
	ReleaseMBID      string
	ReleaseGroupMBID string
	ArtistMBID       string
	UPC              string
	DeezerAlbumID    string
	Position         TrackPosition
	TrackCount       int
	Contributors     []Contributor
	Explicit         *bool
	DurationSeconds  float64
	Sources          []ProviderRef
	Xref             map[string]string
}
