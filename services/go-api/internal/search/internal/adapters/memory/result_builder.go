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

func WithSource(provider domain.ProviderName, id, url string) ResultOption {
	return func(r *domain.SearchResult) {
		r.Sources = append(r.Sources, domain.ProviderLink{Provider: provider, ExternalID: id, URL: url})
	}
}

func WithConfidence(c domain.Confidence) ResultOption {
	return func(r *domain.SearchResult) { r.Confidence = c }
}

func WithSignature(signature string) ResultOption {
	return func(r *domain.SearchResult) { r.Signature = signature }
}

func WithExtras(extras map[string]any) ResultOption {
	return func(r *domain.SearchResult) { r.Extras = extras }
}
