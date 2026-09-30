package domain

import (
	"altune/go-api/internal/shared/textnorm"
	"net/url"
)

type SourceRef struct {
	Provider   ProviderName
	ExternalID string
	URL        string
}

type SearchResult struct {
	Kind           ResultKind
	Title          string
	Subtitle       string
	ImageURL       string
	ArtworkSource  string
	Confidence     Confidence
	Sources        []SourceRef
	Popularity     float64
	ISRC           string
	MBID           string
	UPC            string
	Xref           map[string]string
	Year           int
	ReleaseDate    string
	TrackCount     int
	ProviderRank   int64
	FanCount       int64
	Album          string
	Duration       int
	DeezerAlbumID  string
	Signature      string
	RecordType     RecordType
	ResolutionTier ResolutionTierStamp
	Extras         map[string]any
}

func (r *SearchResult) PutExtra(key string, value any) {
	if r.Extras == nil {
		r.Extras = map[string]any{}
	}
	r.Extras[key] = value
}

func (r SearchResult) WithExtra(key string, value any) SearchResult {
	r.Extras = copyExtras(r.Extras)
	r.Extras[key] = value
	return r
}

func PutTypedExtras(extras map[string]any, r SearchResult) {
	if r.RecordType != "" {
		extras[ExtraRecordType] = string(r.RecordType)
	}
	if r.ResolutionTier.Stamped {
		extras[ExtraResolutionTier] = r.ResolutionTier.Tier.String()
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
		ImageURL:   SafeImageURL(imageURL),
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

func SignatureOf(r SearchResult) string {
	if r.Signature != "" {
		return r.Signature
	}
	return ResultSignature(r)
}

func SafeImageURL(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" {
		return ""
	}
	return raw
}
