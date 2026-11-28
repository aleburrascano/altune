package domain

type FindRequest struct {
	Title    string
	Artist   string
	Album    string
	ISRC     string
	Duration float64
	Identity RecordingIdentity

	CatalogSources  []CatalogSource
	CatalogExplicit *bool

	KnownSourceURL string

	Tier SourceTier
}

type SourceTier int

const (
	TierFirstThenFallback SourceTier = iota
	TierFallbackOnly
)

func PrefersExplicit(catalogExplicit *bool) bool {
	return catalogExplicit == nil || *catalogExplicit
}
