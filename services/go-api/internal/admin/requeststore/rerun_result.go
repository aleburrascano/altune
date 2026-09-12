package requeststore

// This file holds the neutral, transport-agnostic results produced by the
// rerun orchestration in internal/app. They carry no json tags: the admin
// handler owns the wire DTOs and maps these into them field-for-field at its
// boundary, so orchestration logic never depends on admin/handler.

// ReRunResult is the orchestration-owned outcome of a search rerun trace.
type ReRunResult struct {
	Query     string
	Kinds     []string
	Providers []ProviderTrace
	Exchanges []Exchange
	Merged    []ResultRow
	RankTrace []ScoredRow
	Final     []ResultRow
	TookMs    int64
}

// ScoredRow pairs a projected result row with the per-signal scores that the
// ranker attached to it.
type ScoredRow struct {
	ResultRow   ResultRow
	Relevance   float64
	Prominence  float64
	Behavioral  float64
	Popularity  float64
	RRF         float64
	MultiSource bool
	Demoted     bool
}

// DetailReRunResult is the orchestration-owned outcome of an artist-detail
// rerun trace.
type DetailReRunResult struct {
	Query      string
	Resolved   *DetailEntity
	AlbumSeeds []DetailSeedGroup
	TrackSeeds []DetailSeedGroup
	Albums     []DetailItemRow
	TopTracks  []DetailItemRow
	TookMs     int64
}

// DetailEntity describes the artist that a detail rerun resolved, with the
// external IDs it was matched to per provider.
type DetailEntity struct {
	Title    string
	Subtitle string
	MBID     string
	Sources  map[string]string
}

// DetailSeedGroup captures one provider's raw contribution to a detail rerun,
// including any fetch error encountered.
type DetailSeedGroup struct {
	Provider   string
	ExternalID string
	Status     string
	Error      string
	Items      []DetailItemRow
}

// DetailItemRow is a single album or track row in a detail rerun trace.
type DetailItemRow struct {
	Title      string
	Subtitle   string
	Year       int
	TrackCount int
	RecordType string
	ImageURL   string
	Sources    []string
}
