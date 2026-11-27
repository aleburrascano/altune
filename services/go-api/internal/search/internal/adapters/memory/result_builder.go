package memory

import "altune/go-api/internal/search/internal/domain"

type ResultOption func(*domain.SearchResult)

func NewResult(kind domain.ResultKind, title string, opts ...ResultOption) domain.SearchResult {
	r := domain.SearchResult{Kind: kind, Title: title}
	for _, opt := range opts {
		opt(&r)
	}
	return r
}

func WithSubtitle(subtitle string) ResultOption {
	return func(r *domain.SearchResult) { r.Subtitle = subtitle }
}

func WithImageURL(url string) ResultOption {
	return func(r *domain.SearchResult) { r.ImageURL = url }
}

func WithArtworkSource(source domain.ProviderKey) ResultOption {
	return func(r *domain.SearchResult) { r.ArtworkSource = source }
}

func WithConfidence(c domain.Confidence) ResultOption {
	return func(r *domain.SearchResult) { r.Confidence = c }
}

func WithSources(sources []domain.ProviderLink) ResultOption {
	return func(r *domain.SearchResult) { r.Sources = sources }
}

func WithSource(provider domain.ProviderName, id, url string) ResultOption {
	return func(r *domain.SearchResult) {
		r.Sources = append(r.Sources, domain.ProviderLink{Provider: provider, ExternalID: id, URL: url})
	}
}

func WithPopularity(p float64) ResultOption {
	return func(r *domain.SearchResult) { r.Popularity = p }
}

func WithProviderRank(rank int64) ResultOption {
	return func(r *domain.SearchResult) { r.ProviderRank = rank }
}

func WithISRCCode(isrc string) ResultOption {
	return func(r *domain.SearchResult) { r.ISRC = isrc }
}

func WithRecordingMBID(mbid string) ResultOption {
	return func(r *domain.SearchResult) { r.RecordingMBID = mbid }
}

func WithReleaseMBID(mbid string) ResultOption {
	return func(r *domain.SearchResult) { r.ReleaseMBID = mbid }
}

func WithReleaseGroupMBID(mbid string) ResultOption {
	return func(r *domain.SearchResult) { r.ReleaseGroupMBID = mbid }
}

func WithArtistMBID(mbid string) ResultOption {
	return func(r *domain.SearchResult) { r.ArtistMBID = mbid }
}

func WithUPC(upc string) ResultOption {
	return func(r *domain.SearchResult) { r.UPC = upc }
}

func WithXref(xref map[domain.ProviderKey]string) ResultOption {
	return func(r *domain.SearchResult) { r.Xref = xref }
}

func WithYear(year int) ResultOption {
	return func(r *domain.SearchResult) { r.Year = year }
}

func WithReleaseDate(date string) ResultOption {
	return func(r *domain.SearchResult) { r.ReleaseDate = date }
}

func WithTrackCount(n int) ResultOption {
	return func(r *domain.SearchResult) { r.TrackCount = n }
}

func WithFanCount(n int64) ResultOption {
	return func(r *domain.SearchResult) { r.FanCount = n }
}

func WithAlbum(album string) ResultOption {
	return func(r *domain.SearchResult) { r.Album = album }
}

func WithDuration(seconds int) ResultOption {
	return func(r *domain.SearchResult) { r.Duration = seconds }
}

func WithSignature(signature string) ResultOption {
	return func(r *domain.SearchResult) { r.Signature = signature }
}

func WithRecordType(t domain.RecordType) ResultOption {
	return func(r *domain.SearchResult) { r.RecordType = t }
}

func WithTrackPosition(p domain.TrackPosition) ResultOption {
	return func(r *domain.SearchResult) { r.TrackPosition = p }
}

func WithExplicit(explicit *bool) ResultOption {
	return func(r *domain.SearchResult) { r.Explicit = explicit }
}

func WithArtworkPath(p domain.ArtworkPath) ResultOption {
	return func(r *domain.SearchResult) { r.ArtworkPath = p }
}

func WithResolutionTier(t domain.ResolutionTierStamp) ResultOption {
	return func(r *domain.SearchResult) { r.ResolutionTier = t }
}

func WithExtras(extras map[string]any) ResultOption {
	return func(r *domain.SearchResult) { r.Extras = extras }
}
