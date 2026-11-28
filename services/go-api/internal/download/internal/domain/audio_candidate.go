package domain

const (
	UnplayableDRM     = "drm"
	UnplayablePreview = "preview"
)

type CandidateKind string

const (
	KindArtTrack    CandidateKind = "art_track"
	KindPinnedVideo CandidateKind = "pinned_video"
	KindUpload      CandidateKind = "upload"
	KindCatalogPin  CandidateKind = "catalog_pin"
)

type AudioCandidate struct {
	Title       string
	Duration    float64
	URL         string
	Channel     string
	Categories  []string
	ViewCount   int64
	Source      SourceName
	Explicit    bool
	Unplayable  string
	Artists     []string
	Album       string
	Kind        CandidateKind
	ObservedOMV bool
}

func (c AudioCandidate) IsResolved() bool {
	return c.Kind != "" && c.Kind != KindUpload
}

func (c AudioCandidate) HasSourceKey() bool {
	return SourceKey(c.URL) != ""
}
