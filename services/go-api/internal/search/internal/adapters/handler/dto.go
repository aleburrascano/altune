package handler

import (
	"altune/go-api/internal/search/internal/domain"
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

	isrc     string
	position domain.TrackPosition
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

type DiscoveryEventRequest struct {
	Type             string         `json:"type"`
	SearchID         string         `json:"search_id"`
	EventID          string         `json:"event_id"`
	CorrelationID    string         `json:"correlation_id"`
	ClientOccurredAt string         `json:"client_occurred_at"`
	Payload          map[string]any `json:"payload"`
	IgnoredQueryNorm string         `json:"query_norm"`
}

type SuggestionDTO struct {
	Text       string `json:"text"`
	Kind       string `json:"kind"`
	Popularity int64  `json:"popularity"`
}

type SuggestResponse struct {
	Suggestions []SuggestionDTO `json:"suggestions"`
}

const addedExtraFields = 9

type collapsedArtistDTO struct {
	Title    string                `json:"title"`
	Subtitle string                `json:"subtitle"`
	ImageURL string                `json:"image_url,omitempty"`
	Sources  []domain.ProviderLink `json:"sources"`
	Extras   map[string]any        `json:"extras"`
}

func collapsedArtistsToDTO(collapsed []domain.CollapsedArtistSummary) []collapsedArtistDTO {
	out := make([]collapsedArtistDTO, len(collapsed))
	for i, c := range collapsed {
		out[i] = collapsedArtistDTO{Title: c.Title, Subtitle: c.Subtitle, ImageURL: c.ImageURL, Sources: c.Sources, Extras: c.Extras}
	}
	return out
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
	extras := make(map[string]any, len(sr.Extras)+addedExtraFields)
	for k, v := range sr.Extras {
		extras[k] = v
	}
	if collapsed, ok := extras[domain.ExtraCollapsedArtists].([]domain.CollapsedArtistSummary); ok {
		extras[domain.ExtraCollapsedArtists] = collapsedArtistsToDTO(collapsed)
	}
	domain.PutTypedExtras(extras, sr)
	if _, set := extras[domain.ExtraAlbum]; !set && sr.Album != "" {
		extras[domain.ExtraAlbum] = sr.Album
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
	if _, set := extras[domain.ExtraFeaturedArtists]; !set && sr.Kind == domain.ResultKindTrack {
		if parsed := domain.FeaturedFromText(sr.Title, sr.Subtitle); len(parsed) > 0 {
			extras[domain.ExtraFeaturedArtists] = domain.FeaturedArtistsToExtras(parsed)
		}
	}
	return SearchResultDTO{
		Kind:            sr.Kind.String(),
		Title:           sr.Title,
		Subtitle:        sr.Subtitle,
		ImageURL:        sr.ImageURL,
		ArtworkSource:   sr.ArtworkSource.String(),
		Confidence:      sr.Confidence.String(),
		ResultSignature: domain.SignatureOf(sr),
		FavoriteKey:     domain.FavoriteKeyOf(sr),
		Sources:         sources,
		Extras:          extras,
		isrc:            sr.ISRC,
		position:        sr.TrackPosition,
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

type ContentFetchResponseDTO struct {
	Provider string            `json:"provider_name"`
	Status   string            `json:"status"`
	Items    []SearchResultDTO `json:"items"`
	Partial  bool              `json:"partial"`
	Code     string            `json:"code,omitempty"`
}

type ArtistContentResponseDTO struct {
	TopTracks ContentFetchResponseDTO `json:"top_tracks"`
	Albums    ContentFetchResponseDTO `json:"albums"`
}

type EnrichmentResponseDTO struct {
	MBID           string                        `json:"mbid"`
	Genres         []string                      `json:"genres"`
	Year           int                           `json:"year"`
	Rating         float64                       `json:"rating"`
	RatingVotes    int                           `json:"rating_votes"`
	PrimaryType    string                        `json:"primary_type"`
	SecondaryTypes []string                      `json:"secondary_types"`
	ExternalIDs    map[domain.ProviderKey]string `json:"external_ids"`
	ArtworkURL     string                        `json:"artwork_url"`
	HasContent     bool                          `json:"has_content"`
	Degraded       bool                          `json:"degraded"`
}

type DeezerEnrichmentResponseDTO struct {
	BPM             int              `json:"bpm"`
	Gain            float64          `json:"gain"`
	Explicit        bool             `json:"explicit"`
	Label           string           `json:"label"`
	Genres          []string         `json:"genres"`
	UPC             string           `json:"upc"`
	RecordType      string           `json:"record_type"`
	FeaturedArtists []map[string]any `json:"featured_artists,omitempty"`
	HasContent      bool             `json:"has_content"`
	Degraded        bool             `json:"degraded"`
}

type LyricsResponseDTO struct {
	Plain       string          `json:"plain"`
	SyncedLines []SyncedLineDTO `json:"synced_lines"`
	Writers     []string        `json:"writers"`
	Copyright   string          `json:"copyright"`
	Degraded    bool            `json:"degraded"`
}

type SyncedLineDTO struct {
	Timecode     string `json:"timecode"`
	Line         string `json:"line"`
	Milliseconds int64  `json:"milliseconds"`
	Duration     int64  `json:"duration"`
}

type authFailureRequest struct {
	Reason     *string `json:"reason"`
	AppVersion *string `json:"app_version"`
}

type UnreleasedArtistDTO struct {
	Provider   string `json:"provider"`
	ExternalID string `json:"external_id"`
	Name       string `json:"name"`
}

type UnreleasedSearchItemDTO struct {
	UnreleasedSongDTO
	Artist UnreleasedArtistDTO `json:"artist"`
}

type UnreleasedSearchResponseDTO struct {
	Items []UnreleasedSearchItemDTO `json:"items"`
}

type UnreleasedSongDTO struct {
	GeniusID         *int                   `json:"genius_id"`
	Title            string                 `json:"title"`
	CoverURL         *string                `json:"cover_url"`
	GeniusURL        *string                `json:"genius_url"`
	ReleaseDate      *string                `json:"release_date"`
	HasMedia         *bool                  `json:"has_media"`
	Quality          *string                `json:"quality"`
	QualityRank      int                    `json:"quality_rank"`
	TrackerKey       *string                `json:"tracker_key"`
	OffStreaming     bool                   `json:"off_streaming"`
	OpenURL          *string                `json:"open_url,omitempty"`
	OpenHost         *string                `json:"open_host,omitempty"`
	Versions         []UnreleasedVersionDTO `json:"versions,omitempty"`
	AudioLinks       []AudioLinkDTO         `json:"audio_links"`
	Extras           map[string]any         `json:"extras"`
	BestVersionTitle string                 `json:"best_version_title"`
}

type AudioLinkDTO struct {
	Provider   string `json:"provider"`
	ExternalID string `json:"external_id"`
	URL        string `json:"url"`
}

type UnreleasedVersionDTO struct {
	Title      string         `json:"title"`
	Label      string         `json:"label,omitempty"`
	Quality    string         `json:"quality,omitempty"`
	AudioLinks []AudioLinkDTO `json:"audio_links,omitempty"`
	OpenURL    string         `json:"open_url,omitempty"`
	OpenHost   string         `json:"open_host,omitempty"`
	Extras     map[string]any `json:"extras"`
}

type UnreleasedResponseDTO struct {
	Items   []UnreleasedSongDTO `json:"items"`
	Pending bool                `json:"pending"`
}

type FavoriteDTO struct {
	Kind     string `json:"kind"`
	Key      string `json:"key"`
	Title    string `json:"title"`
	Subtitle string `json:"subtitle,omitempty"`
	ImageURL string `json:"image_url,omitempty"`
}

type FavoriteRequest struct {
	Kind     string `json:"kind"`
	Title    string `json:"title"`
	Subtitle string `json:"subtitle"`
	ImageURL string `json:"image_url"`
}

type RecordSearchHistoryRequest struct {
	Query string `json:"query"`
}
