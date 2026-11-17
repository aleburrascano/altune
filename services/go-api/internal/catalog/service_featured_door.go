package catalog

import featured "altune/go-api/internal/catalog/internal/core/featured"

type (
	BackfillFeaturedService = featured.BackfillFeaturedService
	ListFeaturingService    = featured.ListFeaturingService
)

var (
	NewBackfillFeaturedService = featured.NewBackfillFeaturedService
	NewListFeaturingService    = featured.NewListFeaturingService
)
