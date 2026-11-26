package library

import featured "altune/go-api/internal/library/internal/core/featured"

type (
	FillFeaturedArtistsService = featured.FillFeaturedArtistsService
	ListFeaturingService       = featured.ListFeaturingService
)

var (
	NewBackfillFeaturedService = featured.NewBackfillFeaturedService
	NewFeaturedArtistResolver  = featured.NewFeaturedArtistResolver
	NewListFeaturingService    = featured.NewListFeaturingService
	WithBackfillClock          = featured.WithBackfillClock
	WithBackfillItemTimeout    = featured.WithBackfillItemTimeout
	WithBackfillTimeBudget     = featured.WithBackfillTimeBudget
)

var (
	ErrBackfillCoolingDown = featured.ErrBackfillCoolingDown
	ErrBackfillInProgress  = featured.ErrBackfillInProgress
)
