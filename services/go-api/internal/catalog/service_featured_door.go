package catalog

import featured "altune/go-api/internal/catalog/service/featured"

type (
	BackfillFeaturedService = featured.BackfillFeaturedService
	ListFeaturingService    = featured.ListFeaturingService
)

var (
	NewBackfillFeaturedService = featured.NewBackfillFeaturedService
	NewListFeaturingService    = featured.NewListFeaturingService
)
