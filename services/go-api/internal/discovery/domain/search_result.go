package domain

import "altune/go-api/internal/shared/textnorm"

type SourceRef struct {
	Provider   ProviderName
	ExternalID string
	URL        string
}

type SearchResult struct {
	Kind          ResultKind
	Title         string
	Subtitle      string
	ImageURL      string
	ArtworkSource string
	Confidence    Confidence
	Sources       []SourceRef
	Popularity    float64
	ISRC          string
	MBID          string
	UPC           string
	Xref          map[string]string
	Year          int
	ReleaseDate   string
	TrackCount    int
	ProviderRank  int64
	FanCount      int64
	Album         string
	Duration      int
	DeezerAlbumID string
	Signature     string
	// RecordType is the provider's raw release type (e.g. "album", "single",
	// "ep", "compile"); discography bucketing and release merge branch on it.
	RecordType RecordType
	// ResolutionTier is set by entity merge; unmerged results leave it zero.
	ResolutionTier ResolutionTierStamp
	Extras         map[string]any
}

// PutExtra sets key to value on the result's own Extras map, lazily
// initialising it when nil. It mutates in place, so use it only on a result
// this code owns and is free to change.
func (r *SearchResult) PutExtra(key string, value any) {
	if r.Extras == nil {
		r.Extras = map[string]any{}
	}
	r.Extras[key] = value
}

// WithExtra returns a copy of the result with key set to value on a freshly
// copied Extras map, never mutating the receiver's map. It is the single home
// of the "never mutate the cached list" invariant: use it whenever the result
// may be shared (e.g. a cached or ranked list) rather than owned outright.
func (r SearchResult) WithExtra(key string, value any) SearchResult {
	r.Extras = copyExtras(r.Extras)
	r.Extras[key] = value
	return r
}

// PutTypedExtras writes the typed fields that the wire contract still carries
// under extras (record_type, resolution_tier) into extras, which the caller
// must own. It keeps response JSON identical to when these lived in Extras.
func PutTypedExtras(extras map[string]any, r SearchResult) {
	if r.RecordType != "" {
		extras["record_type"] = string(r.RecordType)
	}
	if r.ResolutionTier.Stamped {
		extras["resolution_tier"] = r.ResolutionTier.Tier.String()
	}
}

func copyExtras(src map[string]any) map[string]any {
	dst := make(map[string]any, len(src))
	for k, v := range src {
		dst[k] = v
	}
	return dst
}

type CollapsedArtistSummary struct {
	Title    string         `json:"title"`
	Subtitle string         `json:"subtitle"`
	ImageURL string         `json:"image_url,omitempty"`
	Sources  []SourceRef    `json:"sources"`
	Extras   map[string]any `json:"extras"`
}

func NewProviderResult(kind ResultKind, title, subtitle, imageURL string, source SourceRef, extras map[string]any) SearchResult {
	if extras == nil {
		extras = map[string]any{}
	}
	return SearchResult{
		Kind:       kind,
		Title:      title,
		Subtitle:   subtitle,
		ImageURL:   imageURL,
		Confidence: ConfidenceLow,
		Sources:    []SourceRef{source},
		Extras:     extras,
	}
}

func ResultSignature(r SearchResult) string {
	return r.Kind.String() + "|" +
		textnorm.NormalizeForMatch(r.Title) + "|" +
		textnorm.NormalizeForMatch(r.Subtitle)
}
