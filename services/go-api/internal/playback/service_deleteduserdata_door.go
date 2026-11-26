package playback

import erasure "altune/go-api/internal/playback/internal/core/deleteduserdata"

type DeletedUserDataCleanup = erasure.Sweeper

var (
	NewErasureSweeper       = erasure.NewSweeper
	WithErasedStateReaper   = erasure.WithErasedStateReaper
	WithErasureSweepMetrics = erasure.WithErasureSweepMetrics
	WithSweepRunBudget      = erasure.WithSweepRunBudget
)
