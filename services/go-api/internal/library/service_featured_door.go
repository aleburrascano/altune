package library

import featured "altune/go-api/internal/library/internal/service/featured"

type (
	FillFeaturedArtistsService = featured.FillFeaturedArtistsService
	ListFeaturingService       = featured.ListFeaturingService
)

var (
	ErrBackfillClockRequired       = featured.ErrClockRequired
	MustNewBackfillFeaturedService = featured.MustNewBackfillFeaturedService
	NewBackfillFeaturedService     = featured.NewBackfillFeaturedService
	NewFeaturedArtistResolver      = featured.NewFeaturedArtistResolver
	NewListFeaturingService        = featured.NewListFeaturingService
	WithBackfillItemTimeout        = featured.WithBackfillItemTimeout
	WithBackfillTimeBudget         = featured.WithBackfillTimeBudget
)

var (
	ErrBackfillCoolingDown = featured.ErrBackfillCoolingDown
	ErrBackfillInProgress  = featured.ErrBackfillInProgress
)
