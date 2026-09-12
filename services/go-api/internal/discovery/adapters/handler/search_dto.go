package handler

import (
	"altune/go-api/internal/discovery/domain"
)

type SearchResultDTO struct {
	Kind            string         `json:"kind"`
	Title           string         `json:"title"`
	Subtitle        string         `json:"subtitle,omitempty"`
	ImageURL        string         `json:"image_url,omitempty"`
	ArtworkSource   string         `json:"artwork_source,omitempty"`
	Confidence      string         `json:"confidence"`
	ResultSignature string         `json:"result_signature"`
	FavoriteKey     string         `json:"favorite_key"`
	Sources         []SourceRefDTO `json:"sources"`
	Extras          map[string]any `json:"extras"`
}

type SourceRefDTO struct {
	Provider   string `json:"provider"`
	ExternalID string `json:"external_id"`
	URL        string `json:"url"`
}

type ProviderStatusDTO struct {
	Provider    string `json:"provider"`
	Status      string `json:"status"`
	LatencyMs   int64  `json:"latency_ms"`
	ResultCount int    `json:"result_count"`
}

type RelatedGroupDTO struct {
	Relationship string            `json:"relationship"`
	RelatedTo    string            `json:"related_to"`
	Items        []SearchResultDTO `json:"items"`
}

type ResultSectionDTO struct {
	Kind    string            `json:"kind"`
	Items   []SearchResultDTO `json:"items"`
	HasMore bool              `json:"has_more"`
}

type DiscoverySearchResponse struct {
	Query          string              `json:"query"`
	QueryNorm      string              `json:"query_norm"`
	SearchID       string              `json:"search_id"`
	Results        []SearchResultDTO   `json:"results"`
	TopResult      *SearchResultDTO    `json:"top_result,omitempty"`
	Sections       []ResultSectionDTO  `json:"sections"`
	Providers      []ProviderStatusDTO `json:"providers"`
	Partial        bool                `json:"partial"`
	Exploration    bool                `json:"exploration,omitempty"`
	Cache          CacheDTO            `json:"cache"`
	CorrectedQuery string              `json:"corrected_query,omitempty"`
	OriginalQuery  string              `json:"original_query,omitempty"`
	Related        []RelatedGroupDTO   `json:"related,omitempty"`
	Total          int                 `json:"total"`
	Offset         int                 `json:"offset"`
	HasMore        bool                `json:"has_more"`
}

type CacheDTO struct {
	Hit       bool    `json:"hit"`
	FetchedAt *string `json:"fetched_at"`
}

type SearchHistoryItemDTO struct {
	Query      string `json:"query"`
	QueryNorm  string `json:"query_norm"`
	ExecutedAt string `json:"executed_at"`
}

type DiscoverySearchHistoryResponse struct {
	Items []SearchHistoryItemDTO `json:"items"`
	Total int                    `json:"total"`
}

type DiscoveryEventRequest struct {
	Type             string         `json:"type"`
	QueryNorm        string         `json:"query_norm"`
	SearchID         string         `json:"search_id"`
	EventID          string         `json:"event_id"`
	ClientOccurredAt string         `json:"client_occurred_at"`
	Payload          map[string]any `json:"payload"`
}

type SuggestionDTO struct {
	Text       string `json:"text"`
	Kind       string `json:"kind"`
	Popularity int64  `json:"popularity"`
}

type SuggestResponse struct {
	Suggestions []SuggestionDTO `json:"suggestions"`
}

func searchResultToDTO(sr domain.SearchResult) SearchResultDTO {
	sources := make([]SourceRefDTO, len(sr.Sources))
	for i, s := range sr.Sources {
		sources[i] = SourceRefDTO{
			Provider:   s.Provider.String(),
			ExternalID: s.ExternalID,
			URL:        s.URL,
		}
	}
	extras := make(map[string]any, len(sr.Extras)+7)
	for k, v := range sr.Extras {
		extras[k] = v
	}
	if _, set := extras["album"]; !set && sr.Album != "" {
		extras["album"] = sr.Album
	}
	if sr.ISRC != "" {
		extras["isrc"] = sr.ISRC
	}
	if sr.UPC != "" {
		extras["upc"] = sr.UPC
	}
	if sr.MBID != "" {
		extras["mbid"] = sr.MBID
	}
	if sr.Year != 0 {
		extras["year"] = sr.Year
	}
	if sr.ReleaseDate != "" {
		extras["release_date"] = sr.ReleaseDate
	}
	if sr.TrackCount != 0 {
		extras["track_count"] = sr.TrackCount
	}
	if sr.ProviderRank != 0 {
		extras["rank"] = sr.ProviderRank
	}
	if sr.FanCount != 0 {
		extras["nb_fan"] = sr.FanCount
	}
	if _, set := extras["featured_artists"]; !set && sr.Kind == domain.ResultKindTrack {
		if parsed := domain.FeaturedFromText(sr.Title, sr.Subtitle); len(parsed) > 0 {
			extras["featured_artists"] = domain.FeaturedArtistsToExtras(parsed)
		}
	}
	signature := sr.Signature
	if signature == "" {
		signature = domain.ResultSignature(sr)
	}
	return SearchResultDTO{
		Kind:            sr.Kind.String(),
		Title:           sr.Title,
		Subtitle:        sr.Subtitle,
		ImageURL:        sr.ImageURL,
		ArtworkSource:   sr.ArtworkSource,
		Confidence:      sr.Confidence.String(),
		ResultSignature: signature,
		FavoriteKey:     domain.FavoriteKeyOf(sr),
		Sources:         sources,
		Extras:          extras,
	}
}

func searchResultsToDTOs(results []domain.SearchResult) []SearchResultDTO {
	dtos := make([]SearchResultDTO, len(results))
	for i, sr := range results {
		dtos[i] = searchResultToDTO(sr)
	}
	return dtos
}

func relatedGroupsToDTOs(groups []domain.RelatedGroup) []RelatedGroupDTO {
	if len(groups) == 0 {
		return nil
	}
	dtos := make([]RelatedGroupDTO, 0, len(groups))
	for _, g := range groups {
		dtos = append(dtos, RelatedGroupDTO{
			Relationship: string(g.Relationship),
			RelatedTo:    g.RelatedTo,
			Items:        searchResultsToDTOs(g.Items),
		})
	}
	return dtos
}

func providerStatusesToDTOs(statuses []domain.ProviderSearchResponse) []ProviderStatusDTO {
	dtos := make([]ProviderStatusDTO, len(statuses))
	for i, ps := range statuses {
		dtos[i] = ProviderStatusDTO{
			Provider:    ps.Provider.String(),
			Status:      ps.Status.String(),
			LatencyMs:   ps.LatencyMs,
			ResultCount: ps.ResultCount,
		}
	}
	return dtos
}
